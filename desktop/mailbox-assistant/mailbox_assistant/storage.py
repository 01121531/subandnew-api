"""Only the operator session is persisted; Windows DPAPI protects the cookie."""

import base64
import ctypes
import hashlib
import json
import os
import sys
import time
from ctypes import wintypes
from pathlib import Path
from typing import Any

from PySide6.QtCore import QIODevice, QSaveFile

from .api import normalize_origin


class Blob(ctypes.Structure):
    _fields_ = [("size", wintypes.DWORD), ("data", ctypes.POINTER(ctypes.c_ubyte))]


def dpapi(value: bytes, entropy: bytes, *, decrypt: bool = False) -> bytes:
    if sys.platform != "win32":
        raise ValueError("mailbox_secure_storage_unavailable")
    crypt = ctypes.WinDLL("crypt32", use_last_error=True)
    kernel = ctypes.WinDLL("kernel32", use_last_error=True)
    buffers = [ctypes.create_string_buffer(x) for x in (value, entropy)]
    source, salt = [
        Blob(len(raw), ctypes.cast(buf, ctypes.POINTER(ctypes.c_ubyte)))
        for raw, buf in zip((value, entropy), buffers, strict=True)
    ]
    output = Blob()
    function = crypt.CryptUnprotectData if decrypt else crypt.CryptProtectData
    function.restype = wintypes.BOOL
    function.argtypes = [
        ctypes.POINTER(Blob),
        ctypes.c_void_p,
        ctypes.POINTER(Blob),
        ctypes.c_void_p,
        ctypes.c_void_p,
        wintypes.DWORD,
        ctypes.POINTER(Blob),
    ]
    kernel.LocalFree.argtypes = [ctypes.c_void_p]
    kernel.LocalFree.restype = ctypes.c_void_p
    try:
        if not function(
            ctypes.byref(source), None, ctypes.byref(salt), None, None, 1, ctypes.byref(output)
        ):
            raise ValueError("mailbox_secure_storage_unavailable")
        return ctypes.string_at(output.data, output.size)
    finally:
        if output.data:
            ctypes.memset(output.data, 0, output.size)
            kernel.LocalFree(output.data)
        for buf in buffers:
            ctypes.memset(buf, 0, ctypes.sizeof(buf))


class SessionStore:
    def __init__(self, directory: Path | None = None):
        root = (
            directory or Path(os.environ.get("LOCALAPPDATA", str(Path.home()))) / "MailboxAssistant"
        )
        self.path = root / "session.json"

    def read(self) -> dict[str, Any]:
        try:
            if self.path.stat().st_size > 16384:
                return {}
            value = json.loads(self.path.read_text(encoding="utf-8"))
            return value if isinstance(value, dict) else {}
        except (OSError, ValueError):
            return {}

    def _write(self, value: dict[str, Any]) -> None:
        self.path.parent.mkdir(parents=True, exist_ok=True)
        file = QSaveFile(str(self.path))
        if not file.open(QIODevice.OpenModeFlag.WriteOnly):
            raise ValueError("mailbox_secure_storage_unavailable")
        payload = json.dumps(value, ensure_ascii=True).encode("utf-8")
        if file.write(payload) != len(payload) or not file.commit():
            raise ValueError("mailbox_secure_storage_unavailable")

    @staticmethod
    def entropy(origin: str, username: str, operator: int) -> bytes:
        return hashlib.sha256(
            f"mailbox-desktop:v1\0{origin}\0{username}\0{operator}".encode()
        ).digest()

    def save(self, origin: str, username: str, operator: int, cookie: str, expires: int) -> None:
        origin = normalize_origin(origin)
        value = {"origin": origin, "username": username, "operator": operator, "expires": expires}
        protected = dpapi(cookie.encode("ascii"), self.entropy(origin, username, operator))
        self._write({**value, "protected_session": base64.b64encode(protected).decode("ascii")})

    def load(self, origin: str, username: str) -> tuple[str, int] | None:
        value = self.read()
        try:
            origin = normalize_origin(origin)
            if value.get("origin") != origin or value.get("username") != username:
                return None
            operator = int(value["operator"])
            if operator <= 0 or int(value["expires"]) <= time.time():
                self.clear()
                return None
            cookie = dpapi(
                base64.b64decode(value["protected_session"], validate=True),
                self.entropy(origin, username, operator),
                decrypt=True,
            ).decode("ascii")
            return cookie, operator
        except (KeyError, ValueError, TypeError, OSError):
            self.clear()
            return None

    def clear(self) -> None:
        value = self.read()
        self._write({k: value[k] for k in ("origin", "username") if isinstance(value.get(k), str)})
