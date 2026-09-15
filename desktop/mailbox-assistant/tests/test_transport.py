import json
import os
import shutil
import ssl
import subprocess
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path

import pytest
from PySide6.QtCore import QByteArray
from PySide6.QtNetwork import QSslCertificate, QSslConfiguration

from mailbox_assistant.api import Api


@pytest.fixture
def tls_server(tmp_path, qapp):
    openssl = shutil.which("openssl")
    if not openssl:
        git = shutil.which("git")
        candidate = Path(git).parent.parent / "usr/bin/openssl.exe" if git else Path("")
        if candidate.is_file():
            openssl = str(candidate)
    if not openssl:
        pytest.skip("OpenSSL CLI required for ephemeral TLS test certificates")
    cert, key = tmp_path / "cert.pem", tmp_path / "key.pem"
    subprocess.run(
        [
            openssl,
            "req",
            "-x509",
            "-newkey",
            "rsa:2048",
            "-nodes",
            "-days",
            "1",
            "-subj",
            "/CN=localhost",
            "-addext",
            "subjectAltName=DNS:localhost",
            "-keyout",
            str(key),
            "-out",
            str(cert),
        ],
        check=True,
        capture_output=True,
        creationflags=subprocess.CREATE_NO_WINDOW if os.name == "nt" else 0,
    )
    requests = []

    class Handler(BaseHTTPRequestHandler):
        def log_message(self, *_):
            pass

        def do_GET(self):
            requests.append(
                (self.path, {key.lower(): value for key, value in self.headers.items()})
            )
            if self.path.endswith("/redirect"):
                self.send_response(302)
                self.send_header("Location", "https://unrelated.invalid/stolen")
                self.end_headers()
                return
            body = json.dumps({"success": True, "data": {"value": "safe"}}).encode()
            if self.path.endswith("/large"):
                body = b"x" * (3 * 1024 * 1024)
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            try:
                self.wfile.write(body)
            except OSError:
                pass

        def do_POST(self):
            body = self.rfile.read(int(self.headers.get("Content-Length", 0)))
            requests.append(
                (self.path, {key.lower(): value for key, value in self.headers.items()})
            )
            result = json.dumps({"success": True, "data": {"length": len(body)}}).encode()
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(result)))
            self.end_headers()
            self.wfile.write(result)

    server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
    context = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
    context.load_cert_chain(cert, key)
    server.socket = context.wrap_socket(server.socket, server_side=True)
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    yield server.server_port, cert, requests
    server.shutdown()
    server.server_close()
    thread.join(timeout=3)


def call(qtbot, api, path, **kwargs):
    result = []
    api.call(path, lambda data, error: result.append((data, error)), **kwargs)
    qtbot.waitUntil(lambda: bool(result), timeout=8000)
    return result[0]


def test_tls_redirect_limits_and_native_csrf(tls_server, qtbot):
    port, cert, requests = tls_server
    api = Api()
    api.configure(f"https://localhost:{port}")
    _, error = call(qtbot, api, "/auth/session")
    assert error is not None
    assert not requests  # No request bytes sent to an untrusted TLS endpoint.
    original = QSslConfiguration.defaultConfiguration()
    trusted = QSslConfiguration(original)
    trusted.setCaCertificates([QSslCertificate(QByteArray(cert.read_bytes()))])
    QSslConfiguration.setDefaultConfiguration(trusted)
    try:
        api.manager.clearConnectionCache()
        data, error = call(qtbot, api, "/auth/session")
        assert error is None
        assert data == {"value": "safe"}
        assert requests[-1][1]["x-mailbox-request"] == "1"
        _, error = call(qtbot, api, "/redirect")
        assert error is not None
        assert requests[-1][0].endswith("/redirect")
        _, error = call(qtbot, api, "/large")
        assert error.code == "mailbox_response_too_large"
        _, error = call(
            qtbot, api, "/accounts/1/credentials", method="POST", body={"kind": "password"}
        )
        assert error.code == "mailbox_csrf_required"
        api.csrf = "a" * 64
        data, error = call(
            qtbot, api, "/accounts/1/credentials", method="POST", body={"kind": "password"}
        )
        assert error is None
        assert requests[-1][1]["x-mailbox-csrf"] == "a" * 64
        data, error = call(
            qtbot, api, "/assignments/1/attachments", method="POST", image=b"synthetic-image"
        )
        assert error is None
        assert data["length"] > len(b"synthetic-image")
    finally:
        api.clear()
        QSslConfiguration.setDefaultConfiguration(original)
