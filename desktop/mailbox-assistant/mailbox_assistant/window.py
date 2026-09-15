"""Floating operator workspace. All task data and screenshot drafts stay in memory."""

import ctypes
import sys
import time
from dataclasses import dataclass
from typing import Any
from urllib.parse import urlencode

from PySide6.QtCore import QBuffer, QEvent, QIODevice, Qt, QTimer
from PySide6.QtGui import QCloseEvent, QImage, QImageWriter, QKeySequence, QShortcut
from PySide6.QtWidgets import (
    QApplication,
    QCheckBox,
    QComboBox,
    QDialog,
    QDialogButtonBox,
    QFormLayout,
    QHBoxLayout,
    QLabel,
    QLineEdit,
    QListWidget,
    QMainWindow,
    QMenu,
    QMessageBox,
    QPlainTextEdit,
    QPushButton,
    QScrollArea,
    QStackedWidget,
    QStyle,
    QSystemTrayIcon,
    QToolButton,
    QVBoxLayout,
    QWidget,
)

from .api import Api, ApiError
from .security import ClipboardLease, actionable, assignment_scope
from .storage import SessionStore
from .widgets import CopyField, ImagePreview, stylesheet

ERRORS = {
    "mailbox_invalid_server": "请输入有效的 HTTPS 站点地址，不包含路径或登录信息。",
    "mailbox_network_error": "无法安全连接服务端，请检查网络和证书后重试。",
    "mailbox_credentials_revoked": "此邮箱的资料访问权限已收回。",
    "mailbox_not_found": "任务已被收回或改派，请刷新列表。",
    "mailbox_version_conflict": "任务已发生变化，请刷新后核对。",
    "mailbox_credentials_changed": "资料已发生变化，请重新打开任务。",
    "mailbox_permission_denied": "当前账号无权执行此操作。",
    "mailbox_cvv_disabled": "服务端未启用单节点临时 CVV 交付。",
    "mailbox_cvv_unavailable": "临时 CVV 未提供、已过期或已领取，请联系管理员。",
    "mailbox_invalid_credentials": "用户名或密码不正确。",
    "mailbox_login_limited": "登录失败次数过多，请稍后重试。",
}


@dataclass
class Shot:
    image: QImage
    data: bytes
    attachment_id: str = ""


class Window(QMainWindow):
    def __init__(self, api: Api | None = None, store: SessionStore | None = None):
        super().__init__()
        self.api = api or Api(self)
        self.store = store or SessionStore()
        self.clipboard = ClipboardLease(QApplication.clipboard(), self)
        self.operator_id = 0
        self.epoch = 0
        self.busy = False
        self.hidden_private = False
        self.collapsed = False
        self.quitting = False
        self.kind = "refund"
        self.page = 1
        self.index = 0
        self.items: list[dict[str, Any]] = []
        self.has_more = False
        self.current: dict[str, Any] | None = None
        self.values: dict[str, str] = {}
        self.deadlines: dict[str, float] = {}
        self.shots: list[Shot] = []
        self.cvv_attempts: set[tuple[Any, ...]] = set()
        self.otp_pending = False
        self.otp_needs_refresh = False
        self.check_pending = False
        self.restore_operator = 0
        self.uncertain_submission: tuple[Any, ...] | None = None
        self.history_page = 1
        self.issue_mode = False
        self.history_items: list[dict[str, Any]] = []
        self.setWindowTitle("邮箱助手")
        self.resize(400, 720)
        self.setMinimumSize(320, 250)
        self.setWindowFlag(Qt.WindowType.WindowStaysOnTopHint, True)
        self.setWindowIcon(
            self.style().standardIcon(QStyle.StandardPixmap.SP_FileDialogDetailedView)
        )
        self._build()
        self._theme()
        self.api.auth_failed.connect(self.session_expired)
        self.ticker = QTimer(self)
        self.ticker.timeout.connect(self.tick)
        self.ticker.start(1000)
        self.verify_timer = QTimer(self)
        self.verify_timer.timeout.connect(self.periodic_check)
        self.verify_timer.start(30000)
        QShortcut(QKeySequence.StandardKey.Paste, self, self.paste)
        self.tray = QSystemTrayIcon(self.windowIcon(), self)
        self.tray.setToolTip("邮箱助手")
        menu = QMenu(self)
        menu.addAction("显示", self.restore_window)
        menu.addAction("退出", self.quit_app)
        self.tray.setContextMenu(menu)
        self.tray.activated.connect(
            lambda reason: (
                self.restore_window()
                if reason == QSystemTrayIcon.ActivationReason.DoubleClick
                else None
            )
        )
        self.tray.show()
        self.wts_registered = False
        self.wts_handle = 0
        self.register_session_notifications()
        QTimer.singleShot(0, self.restore_session)

    def _build(self) -> None:
        root = QWidget()
        root.setObjectName("body")
        self.setCentralWidget(root)
        layout = QVBoxLayout(root)
        layout.setContentsMargins(12, 10, 12, 10)
        layout.setSpacing(9)
        bar = QHBoxLayout()
        title = QLabel("邮箱助手")
        title.setObjectName("heading")
        bar.addWidget(title, 1)
        self.pin = QToolButton()
        self.pin.setText("置顶")
        self.pin.setCheckable(True)
        self.pin.setChecked(True)
        self.pin.setToolTip("保持窗口置顶")
        self.pin.toggled.connect(self.pin_changed)
        bar.addWidget(self.pin)
        self.fold = QToolButton()
        self.fold.setText("⌃")
        self.fold.setToolTip("折叠或展开")
        self.fold.clicked.connect(self.toggle_collapse)
        bar.addWidget(self.fold)
        self.account_menu = QToolButton()
        self.account_menu.setText("账户")
        self.account_menu.setPopupMode(QToolButton.ToolButtonPopupMode.InstantPopup)
        menu = QMenu(self)
        menu.addAction("修改密码", self.change_password)
        menu.addAction("退出登录", self.logout)
        menu.addAction("退出应用", self.quit_app)
        self.account_menu.setMenu(menu)
        self.account_menu.setEnabled(False)
        bar.addWidget(self.account_menu)
        layout.addLayout(bar)
        self.stack = QStackedWidget()
        layout.addWidget(self.stack, 1)
        self.message = QLabel()
        self.message.setTextFormat(Qt.TextFormat.PlainText)
        self.message.setWordWrap(True)
        self.message.setObjectName("muted")
        self.message.setAccessibleName("操作状态")
        layout.addWidget(self.message)
        self._login_page()
        self._work_page()
        self._history_page()

    def _login_page(self) -> None:
        page = QWidget()
        layout = QVBoxLayout(page)
        layout.setContentsMargins(4, 20, 4, 4)
        heading = QLabel("操作员登录")
        heading.setObjectName("heading")
        layout.addWidget(heading)
        form = QFormLayout()
        form.setRowWrapPolicy(QFormLayout.RowWrapPolicy.WrapAllRows)
        self.server = QLineEdit()
        self.server.setPlaceholderText("https://example.com")
        self.username = QLineEdit()
        self.password = QLineEdit()
        self.password.setEchoMode(QLineEdit.EchoMode.Password)
        self.password.returnPressed.connect(self.login)
        for caption, control in (
            ("站点", self.server),
            ("用户名", self.username),
            ("密码", self.password),
        ):
            form.addRow(caption, control)
        layout.addLayout(form)
        self.remember = QCheckBox("记住有效会话")
        self.remember.setChecked(True)
        layout.addWidget(self.remember)
        self.login_button = QPushButton("登录")
        self.login_button.setObjectName("primary")
        self.login_button.clicked.connect(self.login)
        layout.addWidget(self.login_button)
        layout.addStretch()
        self.stack.addWidget(page)

    def _work_page(self) -> None:
        page = QWidget()
        layout = QVBoxLayout(page)
        layout.setContentsMargins(0, 0, 0, 0)
        row = QHBoxLayout()
        self.pool = QComboBox()
        self.pool.addItem("退款邮箱", "refund")
        self.pool.addItem("开号邮箱", "opening")
        self.pool.currentIndexChanged.connect(self.pool_changed)
        row.addWidget(self.pool, 1)
        self.refresh = QToolButton()
        self.refresh.setIcon(self.style().standardIcon(QStyle.StandardPixmap.SP_BrowserReload))
        self.refresh.setToolTip("刷新任务")
        self.refresh.clicked.connect(self.refresh_tasks)
        row.addWidget(self.refresh)
        self.history = QPushButton("提交记录")
        self.history.clicked.connect(self.open_history)
        row.addWidget(self.history)
        layout.addLayout(row)
        self.position = QLabel("暂无任务")
        self.position.setObjectName("muted")
        layout.addWidget(self.position)
        self.detail_area = QScrollArea()
        self.detail_area.setWidgetResizable(True)
        contents = QWidget()
        detail = QVBoxLayout(contents)
        detail.setContentsMargins(0, 0, 0, 0)
        detail.setSpacing(5)
        self.fields: dict[str, CopyField] = {}
        for key, title in (
            ("email", "邮箱"),
            ("password", "密码"),
            ("code", "验证码"),
            ("card_number", "信用卡号"),
            ("card_expiry", "有效期"),
            ("cvv", "临时 CVV"),
        ):
            field = CopyField(key, title)
            field.requested.connect(self.copy_field)
            detail.addWidget(field)
            self.fields[key] = field
        self.otp_caption = QLabel()
        self.otp_caption.setObjectName("muted")
        detail.addWidget(self.otp_caption)
        self.cvv_caption = QLabel()
        self.cvv_caption.setWordWrap(True)
        self.cvv_caption.setObjectName("muted")
        detail.addWidget(self.cvv_caption)
        self.report_button = QPushButton("反馈问题")
        self.report_button.clicked.connect(self.toggle_issue)
        detail.addWidget(self.report_button)
        self.issue_fields = QWidget()
        issue_layout = QVBoxLayout(self.issue_fields)
        issue_layout.setContentsMargins(0, 0, 0, 0)
        self.issue_account = QLabel()
        self.issue_account.setTextFormat(Qt.TextFormat.PlainText)
        self.issue_account.setWordWrap(True)
        issue_layout.addWidget(self.issue_account)
        warning = QLabel(
            "请勿填写密码、2FA 密钥、完整卡号或 CVV。反馈后暂停任务，管理员处理后才能继续。"
        )
        warning.setWordWrap(True)
        issue_layout.addWidget(warning)
        self.issue_kind = QComboBox()
        self.issue_kind.setAccessibleName("问题类型")
        issue_layout.addWidget(self.issue_kind)
        self.issue_description = QPlainTextEdit()
        self.issue_description.setAccessibleName("问题说明")
        self.issue_description.setPlaceholderText("填写问题说明，最多 2,000 字")
        self.issue_description.setMaximumHeight(110)
        issue_layout.addWidget(self.issue_description)
        self.issue_fields.hide()
        detail.addWidget(self.issue_fields)
        self.paste_button = QPushButton("粘贴截图")
        self.paste_button.clicked.connect(self.paste)
        detail.addWidget(self.paste_button)
        self.shot_list = QListWidget()
        self.shot_list.setFixedHeight(92)
        self.shot_list.itemDoubleClicked.connect(lambda _: self.preview_shot())
        detail.addWidget(self.shot_list)
        buttons = QHBoxLayout()
        self.preview_button = QPushButton("预览")
        self.preview_button.clicked.connect(self.preview_shot)
        self.remove_button = QPushButton("移除")
        self.remove_button.clicked.connect(self.remove_shot)
        buttons.addWidget(self.preview_button)
        buttons.addWidget(self.remove_button)
        detail.addLayout(buttons)
        self.redacted = QCheckBox("截图已遮挡完整卡号及 CVV")
        detail.addWidget(self.redacted)
        detail.addStretch()
        self.detail_area.setWidget(contents)
        layout.addWidget(self.detail_area, 1)
        self.task_notice = QWidget()
        notice = QVBoxLayout(self.task_notice)
        notice.addStretch()
        self.task_notice_text = QLabel()
        self.task_notice_text.setWordWrap(True)
        self.task_notice_text.setAlignment(Qt.AlignmentFlag.AlignCenter)
        self.task_notice_text.setTextFormat(Qt.TextFormat.PlainText)
        notice.addWidget(self.task_notice_text)
        self.task_retry = QPushButton("刷新任务")
        self.task_retry.clicked.connect(self.refresh_tasks)
        notice.addWidget(self.task_retry)
        notice.addStretch()
        layout.addWidget(self.task_notice, 1)
        self.task_actions = QWidget()
        bottom = QHBoxLayout(self.task_actions)
        bottom.setContentsMargins(0, 0, 0, 0)
        self.previous = QPushButton("上一条")
        self.previous.clicked.connect(lambda: self.navigate(-1))
        self.next = QPushButton("下一条")
        self.next.clicked.connect(lambda: self.navigate(1))
        self.submit_button = QPushButton("提交并下一条")
        self.submit_button.setObjectName("primary")
        self.submit_button.clicked.connect(self.submit)
        for button in (self.previous, self.next, self.submit_button):
            bottom.addWidget(button)
        layout.addWidget(self.task_actions)
        self.stack.addWidget(page)

    def _history_page(self) -> None:
        page = QWidget()
        layout = QVBoxLayout(page)
        self.history_type = QComboBox()
        self.history_type.addItem("截图提交记录", "submissions")
        self.history_type.addItem("异常反馈记录", "issues")
        self.history_type.currentIndexChanged.connect(lambda _: self.load_history(1))
        layout.addWidget(self.history_type)
        self.history_list = QListWidget()
        self.history_list.setWordWrap(True)
        self.history_list.currentRowChanged.connect(self.show_history_item)
        layout.addWidget(self.history_list, 1)
        self.history_details = QPlainTextEdit()
        self.history_details.setReadOnly(True)
        self.history_details.setMaximumHeight(180)
        self.history_details.setAccessibleName("处理详情")
        layout.addWidget(self.history_details)
        self.history_images = QComboBox()
        layout.addWidget(self.history_images)
        show = QPushButton("查看截图")
        show.clicked.connect(self.read_history_image)
        layout.addWidget(show)
        row = QHBoxLayout()
        self.history_prev = QPushButton("上一页")
        self.history_prev.clicked.connect(lambda: self.load_history(self.history_page - 1))
        self.history_next = QPushButton("下一页")
        self.history_next.clicked.connect(lambda: self.load_history(self.history_page + 1))
        back = QPushButton("返回任务")
        back.clicked.connect(self.return_to_tasks)
        for button in (self.history_prev, self.history_next, back):
            row.addWidget(button)
        layout.addLayout(row)
        self.stack.addWidget(page)

    def _theme(self) -> None:
        dark = QApplication.palette().window().color().lightness() < 128
        if getattr(self, "theme_dark", None) == dark:
            return
        self.theme_dark = dark
        self.setStyleSheet(stylesheet(dark))

    def say(self, text: str) -> None:
        self.message.setText(text)

    def error(self, error: ApiError) -> None:
        self.say(
            ERRORS.get(error.code, "操作未完成，请重试或联系管理员。")
            + (" 结果不确定，请先核对记录。" if error.uncertain else "")
        )

    def set_busy(self, busy: bool) -> None:
        self.busy = busy
        for control in (
            self.pool,
            self.refresh,
            self.history,
            self.previous,
            self.next,
            self.submit_button,
            self.paste_button,
            self.remove_button,
            self.preview_button,
            self.fold,
            self.account_menu,
            self.pin,
            self.report_button,
            self.issue_kind,
            self.issue_description,
            self.history_type,
            self.task_retry,
        ):
            control.setEnabled(not busy and self.operator_id > 0)
        active = self.current is not None
        self.detail_area.setVisible(active)
        self.task_actions.setVisible(active)
        self.task_notice.setVisible(not active)
        ready = active and not busy and self.operator_id > 0 and not self.hidden_private
        for task_control in (
            self.submit_button,
            self.paste_button,
            self.report_button,
            self.redacted,
        ):
            task_control.setEnabled(ready)
        self.previous.setEnabled(ready and (self.index > 0 or self.page > 1))
        self.next.setEnabled(ready and (self.index + 1 < len(self.items) or self.has_more))
        self.preview_button.setEnabled(ready and bool(self.shots))
        self.remove_button.setEnabled(ready and bool(self.shots) and not self.uncertain_submission)
        self.login_button.setEnabled(not busy)
        self.password.setEnabled(not busy)
        self.server.setEnabled(not busy and self.operator_id == 0)
        self.username.setEnabled(not busy and self.operator_id == 0)
        for key, field in self.fields.items():
            field.button.setEnabled(not busy and key in self.values)
        if self.uncertain_submission:
            self.issue_kind.setEnabled(False)
            self.issue_description.setEnabled(False)
            self.report_button.setEnabled(False)

    def toggle_issue(self) -> None:
        if not self.current or self.uncertain_submission or not self.discard_allowed():
            return
        self.epoch += 1
        self.check_pending = False
        self.otp_pending = False
        self.clear_secrets()
        self.shots.clear()
        self.render_shots()
        self.redacted.setChecked(False)
        self.issue_description.clear()
        self.issue_mode = not self.issue_mode
        for field in self.fields.values():
            field.setVisible(not self.issue_mode)
        self.otp_caption.setVisible(not self.issue_mode)
        self.cvv_caption.setVisible(not self.issue_mode and self.kind == "opening")
        self.issue_account.setText(str(self.current.get("email", "")) if self.issue_mode else "")
        self.issue_fields.setVisible(self.issue_mode)
        self.report_button.setText("返回截图提交" if self.issue_mode else "反馈问题")
        self.submit_button.setText("反馈并下一条" if self.issue_mode else "提交并下一条")
        self.issue_kind.clear()
        for label, value in (
            ("邮箱无法登录", "email_login"),
            ("验证码异常", "otp"),
            ("信用卡不可用", "card"),
            ("其他", "other"),
        ):
            if value != "card" or self.kind == "opening":
                self.issue_kind.addItem(label, value)
        if not self.issue_mode:
            self.open_current()

    def forget_local_session(self) -> None:
        try:
            self.store.clear()
        except (OSError, ValueError):
            self.say("无法清理会话文件；旧会话仍须由服务端注销或到期失效。")

    def restore_session(self) -> None:
        metadata = self.store.read()
        self.server.setText(str(metadata.get("origin", "")))
        self.username.setText(str(metadata.get("username", "")))
        if not self.server.text():
            return
        try:
            self.api.configure(self.server.text())
            stored = self.store.load(self.api.origin, self.username.text())
            if not stored:
                return
            cookie, self.restore_operator = stored
            self.api.restore_cookie(cookie)
        except (ValueError, OSError):
            self.forget_local_session()
            return
        self.set_busy(True)
        self.say("正在验证会话…")
        self.api.call("/auth/session", self.session_result)

    def login(self) -> None:
        if self.busy:
            return
        try:
            self.api.configure(self.server.text())
        except ValueError:
            self.error(ApiError("mailbox_invalid_server"))
            self.server.setFocus()
            return
        if not self.username.text().strip() or not self.password.text():
            self.say("请填写用户名和密码。")
            return
        self.restore_operator = 0
        self.api.clear()
        self.set_busy(True)
        body = {"username": self.username.text().strip(), "password": self.password.text()}
        self.password.clear()
        self.api.call("/auth/login", self.session_result, method="POST", body=body)

    def session_result(self, data: Any, error: ApiError | None) -> None:
        self.set_busy(False)
        if error:
            self.error(error)
            return
        if not isinstance(data, dict) or data.get("authenticated") is not True:
            self.session_expired()
            return
        if QApplication.platformName() == "windows" and not self.wts_registered:
            self.register_session_notifications()
            if not self.wts_registered:
                return
        operator = data.get("operator", {})
        oid = operator.get("id")
        if (
            not isinstance(oid, int)
            or oid <= 0
            or (self.restore_operator and oid != self.restore_operator)
        ):
            self.session_expired()
            return
        csrf = data.get("csrf_token")
        if not isinstance(csrf, str) or len(csrf) != 64:
            self.session_expired()
            return
        self.operator_id = oid
        self.api.csrf = csrf
        self.restore_operator = 0
        self.hidden_private = False
        self.set_busy(False)
        if self.remember.isChecked():
            try:
                self.store.save(
                    self.api.origin,
                    str(operator.get("username", self.username.text())),
                    oid,
                    self.api.cookie(),
                    int(data["expires_at"]),
                )
            except (ValueError, OSError, KeyError):
                self.say("已登录，但无法安全保存会话；关闭后需要重新登录。")
        else:
            self.forget_local_session()
        self.stack.setCurrentIndex(1)
        self.load_page(1)

    def session_expired(self) -> None:
        self.clear_task(clear_draft=True)
        self.api.clear()
        self.forget_local_session()
        self.operator_id = 0
        self.items.clear()
        self.history_items.clear()
        self.history_list.clear()
        self.history_details.clear()
        self.history_images.clear()
        self.cvv_attempts.clear()
        self.password.clear()
        self.stack.setCurrentIndex(0)
        self.stack.show()
        self.collapsed = False
        self.setMinimumHeight(250)
        self.resize(max(320, self.width()), 720)
        self.set_busy(False)
        self.say("请登录操作员账号。")

    def context(self) -> tuple[Any, ...]:
        return (
            self.api.origin,
            self.operator_id,
            self.epoch,
            self.kind,
            assignment_scope(self.current) if self.current else None,
        )

    def scoped_call(self, path: str, callback: Any, **kwargs: Any) -> None:
        scope = self.context()

        def done(data: Any, error: ApiError | None) -> None:
            if scope == self.context() and not self.hidden_private:
                callback(data, error)

        self.api.call(path, done, **kwargs)

    def clear_secrets(self) -> None:
        self.values.clear()
        self.otp_needs_refresh = False
        self.deadlines.clear()
        self.clipboard.clear()
        for field in self.fields.values():
            field.set_value(None)
        self.otp_caption.clear()
        self.cvv_caption.clear()

    def clear_task(self, clear_draft: bool = False) -> None:
        self.epoch += 1
        self.clear_secrets()
        self.current = None
        self.check_pending = False
        self.otp_pending = False
        if clear_draft:
            self.shots.clear()
            self.render_shots()
            self.uncertain_submission = None
            self.redacted.setChecked(False)
            self.issue_mode = False
            self.issue_fields.hide()
            self.issue_account.clear()
            self.issue_description.clear()
            self.report_button.setText("反馈问题")
            self.submit_button.setText("提交并下一条")
        self.position.setText("尚未读取任务")
        self.task_notice_text.setText("点击刷新任务，读取当前类型下分配给你的邮箱。")
        self.set_busy(self.busy)

    def discard_allowed(self) -> bool:
        if self.busy:
            return False
        if not self.shots and not self.issue_description.toPlainText():
            return True
        return (
            QMessageBox.question(
                self,
                "离开当前任务",
                "丢弃尚未提交的截图和反馈草稿？",
                QMessageBox.StandardButton.Discard | QMessageBox.StandardButton.Cancel,
                QMessageBox.StandardButton.Cancel,
            )
            == QMessageBox.StandardButton.Discard
        )

    def query(self, **values: Any) -> str:
        return "?" + urlencode({"account_type": self.kind, **values})

    def pool_changed(self, _: int) -> None:
        kind = self.pool.currentData()
        if kind == self.kind:
            return
        if not self.discard_allowed():
            self.pool.blockSignals(True)
            self.pool.setCurrentIndex(0 if self.kind == "refund" else 1)
            self.pool.blockSignals(False)
            return
        self.kind = kind
        self.load_page(1)

    def refresh_tasks(self) -> None:
        if self.discard_allowed():
            self.load_page(self.page)

    def load_page(self, page: int, index: int = 0) -> None:
        previous_page = self.page if index < 0 and self.current and self.page > page else 0
        self.clear_task(clear_draft=True)
        self.items = []
        self.has_more = False
        self.index = 0
        self.set_busy(True)
        self.page = max(1, page)
        self.read_queue_page(self.page, index, self.page - 1 if index >= 0 else previous_page)

    def read_queue_page(self, page: int, index: int, fallback_page: int) -> None:
        self.page = page
        self.position.setText("正在读取任务…")
        self.task_notice_text.setText("正在读取任务，请稍候。")
        self.say("正在读取任务…")

        def loaded(data: Any, error: ApiError | None) -> None:
            if error:
                self.set_busy(False)
                self.position.setText("任务读取失败")
                self.task_notice_text.setText(
                    "没有加载到任务数据。请刷新重试；这不表示没有分配任务。"
                )
                self.error(error)
                return
            if not isinstance(data, dict) or not isinstance(data.get("items"), list):
                self.set_busy(False)
                self.position.setText("任务数据格式异常")
                self.task_notice_text.setText("请确认服务端和邮箱助手均已更新，然后刷新任务。")
                self.error(ApiError("mailbox_request_failed"))
                return
            if any(
                not isinstance(item, dict)
                or item.get("account_type") != self.kind
                or item.get("operator_id") != self.operator_id
                or item.get("status")
                not in ("pending", "rejected", "submitted", "approved", "issue_pending")
                or (
                    item.get("status") in ("pending", "rejected")
                    and not actionable(item, self.kind)
                )
                for item in data["items"]
            ):
                self.items = []
                self.has_more = False
                self.set_busy(False)
                self.position.setText("任务状态需要确认")
                self.task_notice_text.setText(
                    "服务端返回了当前不可处理的任务。请刷新任务；若仍出现，请确认服务端已更新并联系管理员核对分配状态。"
                )
                self.say("")
                return
            self.items = [item for item in data["items"] if actionable(item, self.kind)]
            self.has_more = data.get("has_more") is True
            if not self.items:
                if index < 0 and page > 1:
                    self.read_queue_page(page - 1, -1, fallback_page)
                    return
                if index < 0 and fallback_page > 0:
                    self.read_queue_page(fallback_page, 0, 0)
                    return
                if index >= 0 and self.has_more:
                    self.read_queue_page(page + 1, 0, fallback_page)
                    return
                if index >= 0 and fallback_page > 0:
                    self.read_queue_page(fallback_page, -1, 0)
                    return
                self.has_more = False
                self.set_busy(False)
                self.position.setText("没有待处理或已退回任务")
                pool_name = "开号邮箱" if self.kind == "opening" else "退款邮箱"
                self.task_notice_text.setText(
                    f"当前没有可处理的{pool_name}。\n\n"
                    "请确认管理员已将此类型邮箱分配给当前操作员，或切换另一种邮箱类型。\n\n"
                    "待审核、已通过和异常待处理任务不在此列表，可在提交记录中查看。分配或恢复任务后，点击刷新任务。"
                )
                self.say("")
                return
            self.index = min(index, len(self.items) - 1) if index >= 0 else len(self.items) - 1
            self.open_current()

        # Older servers treat "actionable" as a literal status and return no rows.
        # Read the same assigned-account list as the web UI; never prefetch credentials.
        self.scoped_call("/accounts" + self.query(page=page, page_size=20), loaded)

    def open_current(self) -> None:
        hint = self.items[self.index]
        self.clear_task(clear_draft=True)
        self.current = hint
        self.set_busy(True)

        def loaded(data: Any, error: ApiError | None) -> None:
            self.set_busy(False)
            if error or not isinstance(data, dict) or not actionable(data, self.kind):
                self.clear_task(clear_draft=True)
                self.error(error or ApiError("mailbox_credentials_revoked"))
                return
            if data.get("id") != hint.get("id") or data.get("operator_id") != self.operator_id:
                self.clear_task(clear_draft=True)
                self.error(ApiError("mailbox_permission_denied"))
                return
            self.current = data
            status = "已退回" if data["status"] == "rejected" else "待处理"
            self.position.setText(
                f"第 {self.page} 页 · {self.index + 1}/{len(self.items)} · {status}"
            )
            self.values["email"] = str(data["email"])
            self.fields["email"].set_value(self.values["email"])
            for key in ("email", "password", "code"):
                self.fields[key].show()
            self.otp_caption.show()
            for key in ("card_number", "card_expiry", "cvv"):
                self.fields[key].setVisible(self.kind == "opening")
            self.redacted.setVisible(self.kind == "opening")
            self.cvv_caption.setVisible(self.kind == "opening")
            self.fetch_credential("password")
            self.fetch_credential("otp")
            if self.kind == "opening":
                self.fetch_credential("card")
                delivery_id = data.get("temporary_cvv_id")
                claim_key = (
                    self.api.origin,
                    self.operator_id,
                    data["id"],
                    data["assignment_id"],
                    delivery_id,
                )
                if delivery_id and claim_key not in self.cvv_attempts:
                    self.cvv_attempts.add(claim_key)
                    self.fetch_credential("cvv")
                elif data.get("temporary_cvv_status") == "disabled":
                    self.cvv_caption.setText(ERRORS["mailbox_cvv_disabled"])
                elif not delivery_id:
                    self.cvv_caption.setText(ERRORS["mailbox_cvv_unavailable"])
                else:
                    self.cvv_caption.setText("本次会话已尝试领取，旧 CVV 不会再次领取。")
            self.say("")

        self.scoped_call(f"/accounts/{hint['id']}" + self.query(), loaded)

    def fetch_credential(self, kind: str, after: Any = None) -> None:
        if not self.current or self.hidden_private:
            return
        started = time.monotonic()
        if kind == "otp":
            if self.otp_pending:
                return
            self.otp_pending = True

        def received(data: Any, error: ApiError | None) -> None:
            if kind == "otp":
                self.otp_pending = False
            if error:
                if kind == "cvv":
                    text = (
                        "领取结果不确定，请联系管理员重新提供。"
                        if error.uncertain
                        else ERRORS.get(error.code, "临时 CVV 不可用，请联系管理员。")
                    )
                    self.fields["cvv"].set_value(None, "不可用")
                    self.cvv_caption.setText(text)
                else:
                    self.epoch += 1
                    self.clear_secrets()
                    self.error(error)
                return
            if not isinstance(data, dict):
                if kind == "cvv":
                    self.cvv_caption.setText("领取结果不确定，请联系管理员重新提供。")
                    return
                self.error(ApiError("mailbox_request_failed"))
                return
            keys = {
                "password": ["password"],
                "otp": ["code"],
                "card": ["card_number", "card_expiry"],
                "cvv": ["cvv"],
            }[kind]
            for key in keys:
                value = data.get(key)
                if not isinstance(value, str) or not value:
                    self.values.pop(key, None)
                    self.fields[key].set_value(None)
                    if key == "cvv":
                        self.cvv_caption.setText("领取结果不确定，请联系管理员重新提供。")
                    continue
                if key in ("code", "cvv"):
                    expires, server_time = data.get("expires_at"), data.get("server_time")
                    if not isinstance(expires, int) or not isinstance(server_time, int):
                        continue
                    remaining = expires - server_time - (time.monotonic() - started)
                    if remaining <= 0:
                        continue
                    self.deadlines[key] = time.monotonic() + min(
                        remaining, 60 if key == "cvv" else 120
                    )
                self.values[key] = value
                self.fields[key].set_value(value)
            if after:
                after()

        self.scoped_call(
            f"/accounts/{self.current['id']}/credentials" + self.query(),
            received,
            method="POST",
            body={"kind": kind},
        )

    def verify(self, after: Any, *, quiet: bool = False) -> None:
        if not self.current or self.hidden_private:
            return
        expected = assignment_scope(self.current)

        def checked(data: Any, error: ApiError | None) -> None:
            self.check_pending = False
            if (
                error
                or not isinstance(data, dict)
                or assignment_scope(data) != expected
                or not actionable(data, self.kind)
            ):
                self.epoch += 1
                self.clear_secrets()
                self.set_busy(False)
                self.error(error or ApiError("mailbox_credentials_revoked"))
                return
            if not quiet:
                self.say("")
            after()

        self.scoped_call(f"/accounts/{self.current['id']}" + self.query(), checked)

    def periodic_check(self) -> None:
        if (
            self.operator_id
            and self.current
            and not self.busy
            and not self.hidden_private
            and not self.check_pending
        ):
            self.check_pending = True
            self.verify(lambda: None, quiet=True)

    def tick(self) -> None:
        if self.hidden_private or not self.current:
            return
        for key in ("code", "cvv"):
            remaining = max(0, int(self.deadlines.get(key, 0) - time.monotonic()))
            if key in self.values and remaining <= 0:
                self.values.pop(key, None)
                self.fields[key].set_value(None, "已过期")
                self.deadlines.pop(key, None)
                if key == "code":
                    self.otp_needs_refresh = True
            if key == "code":
                self.otp_caption.setText(f"验证码 {remaining} 秒后失效" if remaining else "")
            elif remaining:
                self.cvv_caption.setText(f"临时 CVV {remaining} 秒后清除，复制后立即隐藏")
        if self.otp_needs_refresh and not self.busy and not self.otp_pending:
            self.otp_needs_refresh = False
            self.fetch_credential("otp")

    def copy_field(self, key: str) -> None:
        if self.busy or key not in self.values or not self.current:
            return
        self.set_busy(True)

        def copy() -> None:
            self.set_busy(False)
            if key not in self.values:
                return
            if key in ("code", "cvv") and self.deadlines.get(key, 0) <= time.monotonic():
                self.values.pop(key, None)
                self.fields[key].set_value(None, "已过期")
                if key == "code":
                    self.fetch_credential("otp", lambda: self.copy_field("code"))
                return
            copied = self.clipboard.copy(self.values[key], 15 if key == "cvv" else 30)
            if key == "cvv":
                self.values.pop(key, None)
                self.deadlines.pop(key, None)
                self.fields[key].set_value(None, "已复制并清除" if copied else "复制失败，已清除")
                self.cvv_caption.setText("已领取的 CVV 不能再次获取。")
            self.say("已复制" if copied else "剪贴板正被占用，复制未成功。")

        self.verify(copy)

    def navigate(self, direction: int) -> None:
        if not self.discard_allowed() or not self.items:
            return
        target = self.index + direction
        if 0 <= target < len(self.items):
            self.index = target
            self.open_current()
        elif target < 0 and self.page > 1:
            self.load_page(self.page - 1, -1)
        elif target >= len(self.items) and self.has_more:
            self.load_page(self.page + 1)
        else:
            self.say("已经是第一条。" if direction < 0 else "已经是最后一条。")

    def paste(self) -> None:
        if self.busy or not self.current or self.hidden_private or self.stack.currentIndex() != 1:
            return
        if self.uncertain_submission:
            self.say("请先核对提交结果，不能修改截图。")
            return
        if len(self.shots) >= 5:
            self.say("每次最多提交 5 张截图。")
            return
        image = QApplication.clipboard().image()
        if image.isNull():
            self.say("剪贴板中没有图片。")
            return
        if (
            image.width() > 12000
            or image.height() > 12000
            or image.width() * image.height() > 40000000
        ):
            self.say("图片尺寸过大，请裁剪后粘贴。")
            return
        # Copy pixels into a fresh image to discard clipboard text metadata.
        clean = QImage(image.size(), QImage.Format.Format_RGB32)
        from PySide6.QtGui import QPainter

        painter = QPainter(clean)
        painter.drawImage(0, 0, image)
        painter.end()
        buffer = QBuffer()
        buffer.open(QIODevice.OpenModeFlag.WriteOnly)
        writer = QImageWriter(buffer, b"png")
        if not writer.write(clean):
            self.say("无法读取截图。")
            return
        data = bytes(buffer.data().data())
        if len(data) > 10 * 1024 * 1024:
            self.say("单张截图不能超过 10MB。")
            return
        self.shots.append(Shot(clean, data))
        self.render_shots()
        self.say("已添加截图")

    def render_shots(self) -> None:
        self.shot_list.clear()
        for index, shot in enumerate(self.shots):
            state = "已上传" if shot.attachment_id else "待上传"
            self.shot_list.addItem(
                f"截图 {index + 1} · {shot.image.width()} × {shot.image.height()} · {state}"
            )
        if self.shots:
            self.shot_list.setCurrentRow(len(self.shots) - 1)
        self.set_busy(self.busy)

    def preview_shot(self) -> None:
        index = self.shot_list.currentRow()
        if not self.busy and 0 <= index < len(self.shots):
            dialog = ImagePreview(self.shots[index].image, self)
            dialog.exec()

    def remove_shot(self) -> None:
        if self.uncertain_submission:
            self.say("请先核对提交结果，不能修改截图。")
            return
        index = self.shot_list.currentRow()
        if not self.busy and 0 <= index < len(self.shots):
            self.shots.pop(index)
            self.render_shots()

    def submit(self) -> None:
        if self.busy or not self.current:
            return
        if self.uncertain_submission:
            self.reconcile_submission()
            return
        if not self.issue_mode and not self.shots:
            self.say("请先粘贴至少一张截图，再提交任务。")
            return
        if self.issue_mode and not 1 <= len(self.issue_description.toPlainText().strip()) <= 2000:
            self.say("请填写 1～2,000 字的问题说明。")
            self.issue_description.setFocus()
            return
        if self.shots and self.kind == "opening" and not self.redacted.isChecked():
            self.say("请先确认截图已遮挡完整卡号及 CVV。")
            self.redacted.setFocus()
            return
        if self.uncertain_submission:
            self.reconcile_submission()
            return
        self.set_busy(True)
        self.verify(lambda: self.upload_next(0))

    def upload_next(self, index: int) -> None:
        if not self.current:
            return
        if index == len(self.shots):
            self.verify(self.finish_submit)
            return
        shot = self.shots[index]
        if shot.attachment_id:
            self.upload_next(index + 1)
            return
        self.say(f"正在上传截图 {index + 1}/{len(self.shots)}…")

        def uploaded(data: Any, error: ApiError | None) -> None:
            if error or not isinstance(data, dict) or not isinstance(data.get("id"), str):
                self.set_busy(False)
                self.error(error or ApiError("mailbox_request_failed"))
                return
            shot.attachment_id = data["id"]
            self.render_shots()
            self.upload_next(index + 1)

        self.scoped_call(
            f"/assignments/{self.current['assignment_id']}/attachments" + self.query(),
            uploaded,
            method="POST",
            image=shot.data,
        )

    def finish_submit(self) -> None:
        if not self.current:
            return
        scope = assignment_scope(self.current)
        assignment_id = self.current["assignment_id"]
        self.uncertain_submission = scope
        reporting = self.issue_mode
        self.say("正在提交…")

        def submitted(data: Any, error: ApiError | None) -> None:
            self.set_busy(False)
            if error:
                if error.uncertain:
                    self.uncertain_submission = scope
                    self.reconcile_submission()
                else:
                    self.uncertain_submission = None
                    self.set_busy(False)
                    self.error(error)
                return
            if not isinstance(data, dict) or data.get("assignment_id") != assignment_id:
                self.uncertain_submission = scope
                self.reconcile_submission()
                return
            self.after_submit()

        self.scoped_call(
            f"/assignments/{self.current['assignment_id']}/{'issues' if reporting else 'submit'}"
            + self.query(),
            submitted,
            method="POST",
            body={
                "version": self.current["assignment_version"],
                "attachment_ids": [shot.attachment_id for shot in self.shots],
                **(
                    {
                        "kind": self.issue_kind.currentData(),
                        "description": self.issue_description.toPlainText().strip(),
                    }
                    if reporting
                    else {}
                ),
            },
        )

    def reconcile_submission(self) -> None:
        if not self.current:
            return
        self.set_busy(True)
        assignment = self.current["assignment_id"]
        submitted_version = self.current["assignment_version"]
        reporting = self.issue_mode
        ids = {shot.attachment_id for shot in self.shots}

        def loaded(data: Any, error: ApiError | None) -> None:
            self.set_busy(False)
            if not error and isinstance(data, dict):
                for item in data.get("items", []):
                    if reporting and (
                        item.get("submitted_version") != submitted_version
                        or item.get("kind") != self.issue_kind.currentData()
                        or item.get("description") != self.issue_description.toPlainText().strip()
                    ):
                        continue
                    if item.get("assignment_id") == assignment and ids == {
                        attachment.get("id") for attachment in item.get("attachments", [])
                    }:
                        self.after_submit()
                        return
            self.say("提交结果尚未确认，未重复提交。请查看提交记录或稍后再次核对。")

        self.scoped_call(
            ("/issues" if reporting else "/submissions")
            + self.query(
                page=1, page_size=100, **({"assignment_id": assignment} if reporting else {})
            ),
            loaded,
        )

    def after_submit(self) -> None:
        self.say("提交成功")
        # Queue shrank by one; the same offset now points at the next task.
        self.load_page(self.page, self.index)

    def open_history(self) -> None:
        if not self.discard_allowed():
            return
        self.clear_task(clear_draft=True)
        self.stack.setCurrentIndex(2)
        self.load_history(1)

    def load_history(self, page: int) -> None:
        if self.busy or page < 1:
            return
        self.epoch += 1
        self.history_page = page
        self.history_items.clear()
        self.history_list.clear()
        self.history_images.clear()
        self.history_details.clear()
        self.set_busy(True)

        def loaded(data: Any, error: ApiError | None) -> None:
            self.set_busy(False)
            if error or not isinstance(data, dict):
                self.error(error or ApiError("mailbox_request_failed"))
                return
            self.history_items = data.get("items", [])
            statuses = {
                "pending": "待处理" if self.history_type.currentData() == "issues" else "待审核",
                "approved": "已通过",
                "rejected": "已退回",
                "resolved": "已解决",
                "invalidated": "分配已失效",
            }
            for item in self.history_items:
                self.history_list.addItem(
                    f"{item['email']}\n{statuses.get(item['status'], '未知状态')}"
                )
            self.history_prev.setEnabled(page > 1)
            self.history_next.setEnabled(data.get("has_more") is True)
            self.say("没有提交记录" if not self.history_items else "")

        self.scoped_call(
            f"/{self.history_type.currentData()}" + self.query(page=page, page_size=20), loaded
        )

    def show_history_item(self, index: int) -> None:
        self.history_images.clear()
        if 0 <= index < len(self.history_items):
            item = self.history_items[index]
            if self.history_type.currentData() == "issues":
                labels = {
                    "email_login": "邮箱无法登录",
                    "otp": "验证码异常",
                    "card": "信用卡不可用",
                    "other": "其他",
                }
                created = time.strftime("%Y-%m-%d %H:%M", time.localtime(item.get("created_at", 0)))
                self.history_details.setPlainText(
                    f"{labels.get(str(item.get('kind')), '其他')} · {created}\n"
                    f"{item.get('description', '')}\n管理员回复：{item.get('reply') or '暂无'}"
                )
            else:
                self.history_details.setPlainText(item.get("review_reason") or "")
            for number, attachment in enumerate(item.get("attachments", [])):
                if attachment.get("deleted_at") or attachment.get("expires_at", 0) <= time.time():
                    self.history_images.addItem(f"截图 {number + 1} 已过期", None)
                else:
                    self.history_images.addItem(f"截图 {number + 1}", attachment["id"])

    def read_history_image(self) -> None:
        attachment = self.history_images.currentData()
        if self.busy or not attachment:
            return
        self.set_busy(True)

        def loaded(data: Any, error: ApiError | None) -> None:
            self.set_busy(False)
            if error:
                self.error(error)
                return
            image = QImage.fromData(data)
            if not image.isNull():
                ImagePreview(image, self).exec()

        self.scoped_call(f"/attachments/{attachment}" + self.query(), loaded, blob=True)

    def return_to_tasks(self) -> None:
        if self.busy:
            return
        self.history_items.clear()
        self.history_list.clear()
        self.history_details.clear()
        self.history_images.clear()
        self.stack.setCurrentIndex(1)
        self.load_page(self.page)

    def pin_changed(self, enabled: bool) -> None:
        self.setWindowFlag(Qt.WindowType.WindowStaysOnTopHint, enabled)
        self.show()
        self.register_session_notifications()

    def register_session_notifications(self) -> None:
        if sys.platform != "win32":
            return
        handle = int(self.winId())
        if self.wts_registered and self.wts_handle == handle:
            return
        wts = ctypes.WinDLL("wtsapi32", use_last_error=True)
        wts.WTSUnRegisterSessionNotification.argtypes = [ctypes.c_void_p]
        wts.WTSRegisterSessionNotification.argtypes = [ctypes.c_void_p, ctypes.c_uint]
        if self.wts_registered:
            wts.WTSUnRegisterSessionNotification(self.wts_handle)
        self.wts_handle = handle
        self.wts_registered = bool(wts.WTSRegisterSessionNotification(handle, 0))
        if not self.wts_registered and QApplication.platformName() == "windows":
            self.suspend_private()
            self.say("无法监听 Windows 锁屏状态，请重新启动应用后再处理敏感资料。")

    def suspend_private(self) -> None:
        self.hidden_private = True
        self.epoch += 1
        self.clear_secrets()
        self.password.clear()
        self.api.cancel()
        self.set_busy(False)
        for dialog in self.findChildren(QDialog):
            dialog.reject()
        self.history_items.clear()
        self.history_list.clear()
        self.history_details.clear()
        self.history_images.clear()

    def toggle_collapse(self) -> None:
        if self.busy:
            return
        if self.collapsed:
            self.collapsed = False
            self.stack.show()
            self.setMinimumHeight(250)
            self.resize(self.width(), 720)
            self.resume_private()
        else:
            self.suspend_private()
            self.collapsed = True
            self.stack.hide()
            self.setMinimumHeight(100)
            self.resize(self.width(), 100)

    def resume_private(self) -> None:
        if not self.hidden_private or self.collapsed:
            return
        if QApplication.platformName() == "windows" and not self.wts_registered:
            self.register_session_notifications()
            if not self.wts_registered:
                return
        self.hidden_private = False
        if self.operator_id:
            if self.shots:
                self.say("已隐藏敏感资料，截图草稿仍在内存；刷新资料后可继续。")
                self.verify(lambda: self.reload_visible_credentials())
            else:
                self.load_page(self.page, self.index)

    def reload_visible_credentials(self) -> None:
        if not self.current:
            return
        self.values["email"] = str(self.current["email"])
        self.fields["email"].set_value(self.values["email"])
        self.fetch_credential("password")
        self.fetch_credential("otp")
        if self.kind == "opening":
            self.fetch_credential("card")
            self.cvv_caption.setText("隐藏时已清除临时 CVV，不会再次领取。")

    def restore_window(self) -> None:
        self.showNormal()
        self.raise_()
        self.activateWindow()
        self.resume_private()

    def changeEvent(self, event: QEvent) -> None:
        if event.type() == QEvent.Type.WindowStateChange and hasattr(self, "fields"):
            if self.isMinimized():
                self.suspend_private()
                if QSystemTrayIcon.isSystemTrayAvailable():
                    QTimer.singleShot(0, self.hide)
            else:
                self.resume_private()
        elif event.type() == QEvent.Type.PaletteChange:
            self._theme()
        super().changeEvent(event)

    def nativeEvent(self, event_type: Any, message: int) -> tuple[bool, int]:
        if sys.platform == "win32":
            from ctypes import wintypes

            msg = wintypes.MSG.from_address(int(message))
            if msg.message == 0x02B1 and msg.wParam == 0x7:  # WTS_SESSION_LOCK
                self.suspend_private()
            elif msg.message == 0x02B1 and msg.wParam == 0x8:
                self.resume_private()
        return False, 0

    def change_password(self) -> None:
        if not self.discard_allowed() or not self.operator_id:
            return
        dialog = QDialog(self)
        dialog.setWindowTitle("修改密码")
        layout = QVBoxLayout(dialog)
        old, new = QLineEdit(), QLineEdit()
        old.setEchoMode(QLineEdit.EchoMode.Password)
        new.setEchoMode(QLineEdit.EchoMode.Password)
        form = QFormLayout()
        form.addRow("当前密码", old)
        form.addRow("新密码", new)
        layout.addLayout(form)
        buttons = QDialogButtonBox(
            QDialogButtonBox.StandardButton.Save | QDialogButtonBox.StandardButton.Cancel
        )
        layout.addWidget(buttons)
        buttons.accepted.connect(dialog.accept)
        buttons.rejected.connect(dialog.reject)
        if dialog.exec() == QDialog.DialogCode.Accepted:
            self.set_busy(True)
            body = {"current_password": old.text(), "password": new.text()}
            old.clear()
            new.clear()

            def changed(_: Any, error: ApiError | None) -> None:
                self.set_busy(False)
                if error:
                    self.error(error)
                else:
                    self.session_expired()

            self.api.call("/auth/password", changed, method="POST", body=body)
        old.clear()
        new.clear()
        dialog.deleteLater()

    def logout(self) -> None:
        if not self.discard_allowed():
            return
        self.clear_task(clear_draft=True)
        self.set_busy(True)

        def done(_: Any, error: ApiError | None) -> None:
            self.session_expired()
            if error:
                self.say("本地已退出；服务端注销未确认，请管理员撤销会话或等待到期。")

        self.api.call("/auth/logout", done, method="POST")

    def quit_app(self) -> None:
        if not self.discard_allowed():
            return
        self.quitting = True
        self.clear_task(clear_draft=True)
        self.api.clear()
        self.tray.hide()
        if self.wts_registered:
            wts = ctypes.WinDLL("wtsapi32", use_last_error=True)
            wts.WTSUnRegisterSessionNotification.argtypes = [ctypes.c_void_p]
            wts.WTSUnRegisterSessionNotification(self.wts_handle)
        QApplication.quit()

    def closeEvent(self, event: QCloseEvent) -> None:
        if self.quitting:
            event.accept()
            return
        event.ignore()
        if self.discard_allowed():
            self.clear_task(clear_draft=True)
            self.suspend_private()
            if QSystemTrayIcon.isSystemTrayAvailable():
                self.hide()
            else:
                self.quit_app()
