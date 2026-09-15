"""Isolated packaged-runtime check; no network or real user configuration."""

import tempfile
from pathlib import Path

from PySide6.QtCore import QTimer
from PySide6.QtGui import QImageReader
from PySide6.QtNetwork import QSslSocket
from PySide6.QtWidgets import QApplication

from .storage import SessionStore, dpapi
from .window import Window


def run(app: QApplication) -> int:
    formats = {bytes(value.data()).decode() for value in QImageReader.supportedImageFormats()}
    if not {"png", "jpeg", "webp"}.issubset(formats) or not QSslSocket.supportsSsl():
        return 2
    secret, salt = b"synthetic-runtime-test", b"isolated-smoke-test"
    if dpapi(dpapi(secret, salt), salt, decrypt=True) != secret:
        return 3
    with tempfile.TemporaryDirectory(prefix="mailbox-runtime-") as directory:
        window = Window(store=SessionStore(Path(directory)))
        window.show()

        def finish() -> None:
            ok = window.width() >= 320 and not window.grab().isNull()
            window.quitting = True
            window.api.clear()
            window.tray.hide()
            window.close()
            app.exit(0 if ok else 4)

        QTimer.singleShot(250, finish)
        return app.exec()
