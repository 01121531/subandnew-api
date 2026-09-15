"""One-origin asynchronous transport. No retries, redirects or disk cache."""

import json
import re
from collections.abc import Callable
from dataclasses import dataclass
from typing import Any
from urllib.parse import urlsplit

from PySide6.QtCore import QByteArray, QObject, QTimer, QUrl, Signal
from PySide6.QtNetwork import (
    QHttpMultiPart,
    QHttpPart,
    QNetworkAccessManager,
    QNetworkCookie,
    QNetworkCookieJar,
    QNetworkReply,
    QNetworkRequest,
)

MAX_RESPONSE = 2 * 1024 * 1024


@dataclass(frozen=True)
class ApiError:
    code: str
    status: int = 0
    uncertain: bool = False


Callback = Callable[[Any, ApiError | None], None]


def normalize_origin(raw: str) -> str:
    value = raw.strip()
    parsed = urlsplit(value)
    if (
        parsed.scheme != "https"
        or not parsed.hostname
        or parsed.username is not None
        or parsed.password is not None
        or parsed.path not in ("", "/")
        or parsed.query
        or parsed.fragment
        or any(ord(ch) < 33 for ch in value)
    ):
        raise ValueError("mailbox_invalid_server")
    try:
        port = parsed.port
    except ValueError as exc:
        raise ValueError("mailbox_invalid_server") from exc
    host = parsed.hostname.encode("idna").decode("ascii").lower()
    if ":" in host:
        host = f"[{host}]"
    suffix = f":{port}" if port and port != 443 else ""
    return f"https://{host}{suffix}"


class Api(QObject):
    auth_failed = Signal()

    def __init__(self, parent: QObject | None = None):
        super().__init__(parent)
        self.manager = QNetworkAccessManager(self)
        self.jar = QNetworkCookieJar(self.manager)
        self.manager.setCookieJar(self.jar)
        self.origin = ""
        self.csrf = ""
        self.generation = 0
        self.pending: set[QNetworkReply] = set()

    def configure(self, origin: str) -> None:
        normalized = normalize_origin(origin)
        if normalized != self.origin:
            self.clear()
            self.origin = normalized

    def cancel(self) -> None:
        self.generation += 1
        for reply in list(self.pending):
            reply.abort()

    def clear(self) -> None:
        self.cancel()
        self.csrf = ""
        self.jar.setAllCookies([])
        self.manager.clearAccessCache()

    def cookie(self) -> str:
        for cookie in self.jar.cookiesForUrl(QUrl(self.origin + "/mailbox-api/v1/auth/session")):
            if cookie.name().data() == b"mailbox_session":
                return bytes(cookie.value().data()).decode("ascii")
        return ""

    def restore_cookie(self, value: str) -> None:
        if not re.fullmatch(r"[A-Za-z0-9_-]{20,256}", value):
            raise ValueError("mailbox_session_invalid")
        cookie = QNetworkCookie(QByteArray(b"mailbox_session"), QByteArray(value.encode("ascii")))
        cookie.setPath("/mailbox-api/v1")
        cookie.setSecure(True)
        cookie.setHttpOnly(True)
        self.jar.setCookiesFromUrl([cookie], QUrl(self.origin + "/mailbox-api/v1/auth/session"))

    def call(
        self,
        path: str,
        callback: Callback,
        *,
        method: str = "GET",
        body: dict[str, Any] | None = None,
        image: bytes | None = None,
        blob: bool = False,
    ) -> QNetworkReply | None:
        if not self.origin or not path.startswith("/") or path.startswith("//"):
            callback(None, ApiError("mailbox_invalid_server"))
            return None
        if method != "GET" and path != "/auth/login" and not self.csrf:
            callback(None, ApiError("mailbox_csrf_required", 403))
            return None
        request = QNetworkRequest(QUrl(self.origin + "/mailbox-api/v1" + path))
        request.setAttribute(
            QNetworkRequest.Attribute.RedirectPolicyAttribute,
            QNetworkRequest.RedirectPolicy.ManualRedirectPolicy,
        )
        request.setAttribute(
            QNetworkRequest.Attribute.CacheLoadControlAttribute,
            QNetworkRequest.CacheLoadControl.AlwaysNetwork,
        )
        request.setRawHeader(b"X-Mailbox-Request", b"1")
        request.setRawHeader(
            b"Accept", b"image/png,image/jpeg,image/webp" if blob else b"application/json"
        )
        if self.csrf and method != "GET":
            request.setRawHeader(b"X-Mailbox-CSRF", self.csrf.encode("ascii"))
        request.setTransferTimeout(30000)
        multipart = None
        if image is not None:
            multipart = QHttpMultiPart(QHttpMultiPart.ContentType.FormDataType)
            part = QHttpPart()
            part.setHeader(QNetworkRequest.KnownHeaders.ContentTypeHeader, "image/png")
            part.setHeader(
                QNetworkRequest.KnownHeaders.ContentDispositionHeader,
                'form-data; name="file"; filename="screenshot.png"',
            )
            part.setBody(QByteArray(image))
            multipart.append(part)
            reply = self.manager.post(request, multipart)
            multipart.setParent(reply)
        elif method == "GET":
            reply = self.manager.get(request)
        else:
            request.setHeader(QNetworkRequest.KnownHeaders.ContentTypeHeader, "application/json")
            reply = self.manager.sendCustomRequest(
                request, method.encode("ascii"), QByteArray(json.dumps(body or {}).encode("utf-8"))
            )
        epoch = self.generation
        self.pending.add(reply)
        buffer = bytearray()
        limit = 10 * 1024 * 1024 if blob else MAX_RESPONSE
        oversized = False

        def read() -> None:
            nonlocal oversized
            if not reply.isOpen():
                return
            chunk = reply.readAll().data()
            if len(buffer) + len(chunk) > limit:
                oversized = True
                reply.abort()
            else:
                buffer.extend(chunk)

        timer = QTimer(reply)
        timer.setSingleShot(True)
        timer.timeout.connect(reply.abort)
        timer.start(60000 if image is not None else 35000)

        def finished() -> None:
            timer.stop()
            read()
            self.pending.discard(reply)
            status = reply.attribute(QNetworkRequest.Attribute.HttpStatusCodeAttribute) or 0
            network_error = reply.error() != QNetworkReply.NetworkError.NoError
            content_type = bytes(reply.rawHeader("Content-Type").data()).decode(
                "ascii", errors="ignore"
            )
            reply.deleteLater()
            if epoch != self.generation:
                buffer.clear()
                return
            error: ApiError | None = None
            result: Any = None
            if oversized:
                error = ApiError("mailbox_response_too_large", int(status), method != "GET")
            elif (
                200 <= status < 300
                and blob
                and content_type.split(";")[0] in ("image/png", "image/jpeg", "image/webp")
                and not network_error
            ):
                result = bytes(buffer)
            else:
                try:
                    payload = json.loads(buffer)
                except (ValueError, UnicodeDecodeError):
                    payload = {}
                if not isinstance(payload, dict):
                    payload = {}
                if 200 <= status < 300 and payload.get("success") is True and not network_error:
                    result = payload.get("data")
                else:
                    code = payload.get("message", "")
                    if not isinstance(code, str) or not re.fullmatch(
                        r"mailbox_[a-z0-9_]{1,80}", code
                    ):
                        code = (
                            "mailbox_network_error" if network_error else "mailbox_request_failed"
                        )
                    error = ApiError(
                        code, int(status), method != "GET" and (network_error or status >= 500)
                    )
            buffer.clear()
            if (
                error
                and path != "/auth/login"
                and (
                    error.status == 401
                    or error.code
                    in (
                        "mailbox_csrf_invalid",
                        "mailbox_session_expired",
                        "mailbox_session_revoked",
                    )
                )
            ):
                self.clear()
                self.auth_failed.emit()
                return
            callback(result, error)

        reply.readyRead.connect(read)
        reply.finished.connect(finished)
        return reply
