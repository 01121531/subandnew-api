"""Scope and clipboard lifetimes shared by the desktop views."""

import ctypes
import sys
from typing import Any

from PySide6.QtCore import QObject, QTimer
from PySide6.QtGui import QClipboard


def assignment_scope(account: dict[str, Any]) -> tuple[Any, ...]:
    return tuple(
        account.get(key)
        for key in (
            "id",
            "account_type",
            "assignment_id",
            "assignment_version",
            "operator_id",
            "status",
        )
    )


def actionable(account: dict[str, Any], account_type: str) -> bool:
    return (
        account.get("account_type") == account_type
        and account.get("status") in ("pending", "rejected")
        and account.get("credentials_available") is True
        and isinstance(account.get("assignment_id"), int)
        and account["assignment_id"] > 0
    )


class ClipboardLease(QObject):
    def __init__(self, clipboard: QClipboard, parent: QObject | None = None):
        super().__init__(parent)
        self.clipboard = clipboard
        self.value = ""
        self.sequence = 0
        self.timer = QTimer(self)
        self.timer.setSingleShot(True)
        self.timer.timeout.connect(self.clear)

    @staticmethod
    def current_sequence() -> int:
        if sys.platform == "win32":
            return int(ctypes.windll.user32.GetClipboardSequenceNumber())
        return 0

    def copy(self, value: str, seconds: int = 30) -> bool:
        self.timer.stop()
        self.value = value
        self.clipboard.setText(value)
        self.sequence = self.current_sequence()
        self.timer.start(seconds * 1000)
        return self.clipboard.text() == value

    def clear(self) -> None:
        self.timer.stop()
        if (
            self.value
            and self.current_sequence() == self.sequence
            and self.clipboard.text() == self.value
        ):
            self.clipboard.clear()
        self.value = ""
