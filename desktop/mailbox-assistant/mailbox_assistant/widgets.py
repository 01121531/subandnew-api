"""Native, keyboard-accessible controls; secret values never become tooltips."""

from PySide6.QtCore import QEvent, QPointF, Qt, QTimer, Signal
from PySide6.QtGui import QImage, QPainter, QPaintEvent, QPalette, QPixmap, QTextLayout, QTextOption
from PySide6.QtWidgets import (
    QDialog,
    QDialogButtonBox,
    QHBoxLayout,
    QLabel,
    QPushButton,
    QScrollArea,
    QVBoxLayout,
    QWidget,
)


class WholeValueLabel(QLabel):
    """Wrap even unbroken passwords/addresses without changing the copied value."""

    def text_layout(self, width: int) -> tuple[QTextLayout, float]:
        layout = QTextLayout(self.text(), self.font())
        option = QTextOption()
        option.setWrapMode(QTextOption.WrapMode.WrapAtWordBoundaryOrAnywhere)
        layout.setTextOption(option)
        layout.beginLayout()
        height = 0.0
        while True:
            line = layout.createLine()
            if not line.isValid():
                break
            line.setLineWidth(max(1, width))
            line.setPosition(QPointF(0, height))
            height += line.height()
        layout.endLayout()
        return layout, height

    def heightForWidth(self, width: int) -> int:
        return int(self.text_layout(width)[1] + 1)

    def paintEvent(self, event: QPaintEvent) -> None:
        painter = QPainter(self)
        painter.setPen(self.palette().color(QPalette.ColorRole.WindowText))
        layout, height = self.text_layout(self.width())
        layout.draw(painter, QPointF(0, max(0, (self.height() - height) / 2)))


class CopyField(QWidget):
    requested = Signal(str)

    def __init__(self, key: str, title: str, parent: QWidget | None = None):
        super().__init__(parent)
        self.key = key
        layout = QVBoxLayout(self)
        layout.setContentsMargins(0, 2, 0, 2)
        layout.setSpacing(3)
        self.caption = QLabel(title)
        self.caption.setObjectName("muted")
        layout.addWidget(self.caption)
        self.button = QPushButton("未提供")
        self.button.setObjectName("copyField")
        self.button.setMinimumHeight(40)
        self.button.setAccessibleName(f"复制{title}")
        self.button.clicked.connect(lambda: self.requested.emit(key))
        layout.addWidget(self.button)
        self.set_value(None)

    def set_value(self, value: str | None, hint: str = "未提供") -> None:
        # QPushButton text does not wrap, so a child plain-text label carries long values.
        if not hasattr(self, "text_label"):
            self.button.setText("")
            row = QHBoxLayout(self.button)
            row.setContentsMargins(10, 7, 10, 7)
            self.text_label = WholeValueLabel()
            self.text_label.setTextFormat(Qt.TextFormat.PlainText)
            self.text_label.setWordWrap(True)
            self.text_label.setAttribute(Qt.WidgetAttribute.WA_TransparentForMouseEvents)
            self.text_label.installEventFilter(self)
            row.addWidget(self.text_label, 1)
            symbol = QLabel("⧉")
            symbol.setAccessibleName("复制")
            symbol.setAttribute(Qt.WidgetAttribute.WA_TransparentForMouseEvents)
            row.addWidget(symbol)
        self.text_label.setText(value if value is not None else hint)
        self.button.setEnabled(value is not None)
        QTimer.singleShot(0, self.fit_text)

    def fit_text(self) -> None:
        required = self.text_label.heightForWidth(max(30, self.text_label.width()))
        self.button.setMinimumHeight(max(40, required + 20))

    def eventFilter(self, watched: object, event: QEvent) -> bool:
        if watched is self.text_label and event.type() == QEvent.Type.Resize:
            QTimer.singleShot(0, self.fit_text)
        return False


class ImagePreview(QDialog):
    def __init__(self, image: QImage, parent: QWidget):
        super().__init__(parent)
        self.setWindowTitle("截图预览")
        self.resize(720, 620)
        layout = QVBoxLayout(self)
        area = QScrollArea()
        label = QLabel()
        label.setPixmap(QPixmap.fromImage(image))
        area.setWidget(label)
        layout.addWidget(area)
        buttons = QDialogButtonBox(QDialogButtonBox.StandardButton.Close)
        buttons.rejected.connect(self.reject)
        layout.addWidget(buttons)


def stylesheet(dark: bool) -> str:
    bg, surface, text, muted, border, selected = (
        ("#202124", "#292b2f", "#f2f3f5", "#afb3bb", "#494c53", "#18364b")
        if dark
        else ("#f5f6f8", "#ffffff", "#21252c", "#666e7b", "#dce1e8", "#eaf4ff")
    )
    return f"""
    QWidget {{ color: {text}; font-family: 'Microsoft YaHei UI'; font-size: 12px; }}
    QMainWindow, QDialog, QWidget#body {{ background: {bg}; }}
    QLabel#muted {{ color: {muted}; }}
    QLabel#heading {{ font-size: 18px; font-weight: 600; }}
    QLineEdit, QComboBox, QListWidget {{ background: {surface}; border: 1px solid {border};
        border-radius: 8px; padding: 8px; selection-background-color: #1674bd; }}
    QPushButton, QToolButton {{ background: {surface}; border: 1px solid {border};
        border-radius: 8px; padding: 7px 9px; min-height: 20px; }}
    QPushButton:hover, QToolButton:hover {{ background: {selected}; }}
    QPushButton:focus, QToolButton:focus, QLineEdit:focus, QComboBox:focus {{
        border: 2px solid #2688d5; }}
    QPushButton:disabled, QToolButton:disabled {{ color: {muted}; }}
    QPushButton#primary {{ background: #086eb9; color: white; border-color: #086eb9; }}
    QPushButton#primary:disabled {{ background: {border}; color: {muted}; }}
    QPushButton#copyField {{ text-align: left; background: {surface}; }}
    QScrollArea {{ border: 0; background: transparent; }}
    QScrollArea > QWidget > QWidget {{ background: transparent; }}
    QToolTip {{ background: {surface}; color: {text}; border: 1px solid {border}; }}
    QToolButton:checked {{ background: {selected}; border-color: #2688d5; }}
    QScrollBar:vertical {{ background: {bg}; width: 9px; margin: 0; }}
    QScrollBar::handle:vertical {{ background: {border}; border-radius: 4px; min-height: 28px; }}
    QScrollBar::add-line:vertical, QScrollBar::sub-line:vertical {{ height: 0; }}
    QScrollBar::add-page:vertical, QScrollBar::sub-page:vertical {{ background: transparent; }}
    """
