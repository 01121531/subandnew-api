"""Windows entry point. Never print exception details containing request data."""

import sys

from PySide6.QtCore import qInstallMessageHandler
from PySide6.QtWidgets import QApplication, QMessageBox

from .window import Window


def main() -> int:
    app = QApplication(sys.argv)
    app.setApplicationName("MailboxAssistant")
    app.setOrganizationName("HUICHUAN-AI")
    app.setQuitOnLastWindowClosed(False)
    qInstallMessageHandler(lambda *_: None)
    if "--self-test" in sys.argv:
        from .smoke import run

        return run(app)
    window = Window()

    def unexpected(*_: object) -> None:
        window.suspend_private()
        QMessageBox.warning(window, "邮箱助手", "操作意外中断，已隐藏敏感资料。请刷新或重新登录。")

    sys.excepthook = unexpected
    window.show()
    return app.exec()


if __name__ == "__main__":
    raise SystemExit(main())
