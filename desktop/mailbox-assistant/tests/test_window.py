import time
from urllib.parse import parse_qs, urlsplit

import pytest
from PySide6.QtCore import QObject, Qt, Signal
from PySide6.QtGui import QImage

from mailbox_assistant.api import ApiError
from mailbox_assistant.storage import SessionStore
from mailbox_assistant.window import Window


class FakeApi(QObject):
    auth_failed = Signal()

    def __init__(self):
        super().__init__()
        self.origin = "https://example.test"
        self.csrf = "test"
        self.calls = []

    def call(self, path, callback, **kwargs):
        self.calls.append((path, callback, kwargs))

    def cancel(self):
        pass

    def clear(self):
        pass

    def take(self, part):
        for index, item in enumerate(self.calls):
            if part in item[0]:
                return self.calls.pop(index)
        raise AssertionError(f"missing route {part}")


def account(id=5, kind="refund"):
    return dict(
        id=id,
        account_type=kind,
        assignment_id=id + 100,
        assignment_version=1,
        operator_id=1,
        status="pending",
        credentials_available=True,
        version=1,
        email=f"test{id}@example.test",
    )


@pytest.fixture
def window(qtbot, tmp_path):
    api = FakeApi()
    window = Window(api=api, store=SessionStore(tmp_path))
    qtbot.addWidget(window, before_close_func=lambda w: setattr(w, "quitting", True))
    qtbot.wait(5)
    window.operator_id = 1
    window.stack.setCurrentIndex(1)
    window.current = account()
    window.items = [window.current]
    window.set_busy(False)
    window.show()
    yield window, api
    window.shots.clear()
    window.busy = False
    window.quitting = True
    window.tray.hide()
    window.close()


def test_value_region_click_revalidates_then_copies(window, qtbot, qapp):
    window, api = window
    window.values["password"] = "  exact password  "
    field = window.fields["password"]
    field.set_value(window.values["password"])
    qtbot.mouseClick(field.button, Qt.MouseButton.LeftButton)
    assert window.busy
    _, callback, _ = api.take("/accounts/5?")
    callback(account(), None)
    assert qapp.clipboard().text() == "  exact password  "
    assert not window.busy


def test_copy_cvv_clears_value_and_uses_short_clipboard_lease(window, qapp):
    window, api = window
    window.kind = "opening"
    window.current = account(kind="opening")
    window.values["cvv"] = "007"
    window.deadlines["cvv"] = time.monotonic() + 40
    window.copy_field("cvv")
    _, callback, _ = api.take("/accounts/5?")
    callback(account(kind="opening"), None)
    assert qapp.clipboard().text() == "007"
    assert "cvv" not in window.values
    assert window.clipboard.timer.remainingTime() <= 15000


def test_stale_response_after_navigation_never_populates_new_account(window):
    window, api = window
    window.fetch_credential("password")
    _, callback, _ = api.take("credentials")
    window.clear_task(clear_draft=True)
    window.current = account(4)
    callback({"password": "old-secret"}, None)
    assert not window.values


def test_issue_mode_clears_secrets_and_reports_without_screenshots(window):
    window, api = window
    window.values["password"] = "secret"
    window.toggle_issue()
    assert window.issue_mode
    assert not window.values
    assert window.issue_kind.findData("card") == -1
    window.issue_description.setPlainText("Cannot sign in")
    window.submit()
    api.take("/accounts/5?")[1](account(), None)
    api.take("/accounts/5?")[1](account(), None)
    path, callback, options = api.take("/issues?")
    assert "/assignments/105/issues" in path
    assert options["body"] == {
        "version": 1,
        "kind": "email_login",
        "description": "Cannot sign in",
        "attachment_ids": [],
    }
    callback({"id": 8, "assignment_id": 105}, None)
    assert not window.issue_mode
    assert window.issue_description.toPlainText() == ""
    assert any("status=actionable" in call[0] for call in api.calls)


def test_issue_uncertain_result_is_reconciled_not_replayed(window):
    window, api = window
    window.toggle_issue()
    window.issue_description.setPlainText("Cannot sign in")
    window.finish_submit()
    _, callback, _ = api.take("/issues?")
    callback(None, ApiError("mailbox_network_error", uncertain=True))
    _, callback, options = api.take("/issues?")
    assert options.get("method", "GET") == "GET"
    callback(
        {
            "items": [
                {
                    "assignment_id": 105,
                    "submitted_version": 0,
                    "kind": "email_login",
                    "description": "Cannot sign in",
                    "attachments": [],
                }
            ]
        },
        None,
    )
    assert window.uncertain_submission
    window.submit()
    assert all(call[2].get("method", "GET") == "GET" for call in api.calls)


def test_issue_mode_discards_stale_credential_response(window):
    window, api = window
    window.fetch_credential("password")
    _, callback, _ = api.take("credentials")
    window.toggle_issue()
    callback({"password": "old"}, None)
    assert not window.values


def test_issue_history_bounds_long_untrusted_text(window, qtbot):
    window, _ = window
    window.history_type.blockSignals(True)
    window.history_type.setCurrentIndex(1)
    window.history_type.blockSignals(False)
    description = "<img src='file:///private'>" + "x" * 1800
    window.history_items = [
        {"kind": "other", "description": description, "reply": "Resolved", "attachments": []}
    ]
    window.stack.setCurrentIndex(2)
    window.resize(320, 720)
    window.show_history_item(0)
    qtbot.wait(20)
    assert description in window.history_details.toPlainText()
    assert window.history_details.height() <= 180
    assert window.width() == 320


def test_revocation_and_network_failure_clear_all_sensitive_data(window, qapp):
    window, api = window
    window.values.update(password="secret", email="test@example.test")
    window.clipboard.copy("secret")
    window.copy_field("password")
    _, callback, _ = api.take("/accounts/5?")
    callback(None, ApiError("mailbox_network_error"))
    assert not window.values
    assert qapp.clipboard().text() == ""
    assert not window.busy


def test_expired_otp_is_not_copied(window, qapp):
    window, api = window
    qapp.clipboard().setText("unchanged")
    window.values["code"] = "000012"
    window.deadlines["code"] = time.monotonic() - 1
    window.copy_field("code")
    _, callback, _ = api.take("/accounts/5?")
    callback(account(), None)
    assert qapp.clipboard().text() == "unchanged"
    assert any(call[2].get("body", {}).get("kind") == "otp" for call in api.calls)


def test_paste_is_explicit_bounded_and_keeps_uploaded_ids(window, qapp):
    window, _ = window
    image = QImage(200, 100, QImage.Format.Format_RGB32)
    image.fill(Qt.GlobalColor.white)
    qapp.clipboard().setImage(image)
    assert not window.shots
    for _ in range(6):
        window.paste()
    assert len(window.shots) == 5
    window.shots[0].attachment_id = "a" * 64
    window.render_shots()
    assert window.shots[0].attachment_id == "a" * 64


def test_cross_page_navigation_only_fetches_list_then_current_details(window):
    window, api = window
    window.has_more = True
    window.navigate(1)
    path, callback, _ = api.take("/accounts?")
    assert parse_qs(urlsplit(path).query)["status"] == ["actionable"]
    assert parse_qs(urlsplit(path).query)["page"] == ["2"]
    callback({"items": [account(4), account(3)], "has_more": False}, None)
    path, _, _ = api.take("/accounts/4?")
    assert not api.calls


def test_uncertain_submit_never_replays(window, qapp):
    window, api = window
    image = QImage(50, 50, QImage.Format.Format_RGB32)
    image.fill(Qt.GlobalColor.white)
    qapp.clipboard().setImage(image)
    window.paste()
    window.shots[0].attachment_id = "b" * 64
    window.finish_submit()
    _, callback, _ = api.take("/submit?")
    callback(None, ApiError("mailbox_network_error", uncertain=True))
    assert window.uncertain_submission
    _, callback, _ = api.take("/submissions?")
    callback({"items": []}, None)
    window.submit()
    assert all("/submit?" not in call[0] for call in api.calls)


def test_collapse_erases_secrets_and_old_result(window):
    window, api = window
    window.values["password"] = "secret"
    window.fetch_credential("password")
    _, callback, _ = api.take("credentials")
    window.toggle_collapse()
    callback({"password": "late-secret"}, None)
    assert not window.values
    assert window.collapsed
    assert window.hidden_private


def test_new_cvv_delivery_can_be_claimed_but_same_delivery_is_not_replayed(window):
    window, api = window
    window.kind = "opening"
    item = account(kind="opening")
    item.update(temporary_cvv_id="delivery-one", temporary_cvv_status="available")
    window.items = [item]
    for delivery, expected in [("delivery-one", 1), ("delivery-one", 0), ("delivery-two", 1)]:
        item["temporary_cvv_id"] = delivery
        window.open_current()
        _, done, _ = api.take("/accounts/5?")
        done(dict(item), None)
        assert sum(call[2].get("body", {}).get("kind") == "cvv" for call in api.calls) == expected
        api.calls.clear()


def test_otp_expiring_during_busy_operation_refreshes_when_unlocked(window):
    window, api = window
    window.values["code"] = "000012"
    window.deadlines["code"] = time.monotonic() - 1
    window.set_busy(True)
    window.tick()
    assert "code" not in window.values
    assert not api.calls
    window.set_busy(False)
    window.tick()
    assert api.calls[0][2]["body"] == {"kind": "otp"}


def test_server_assignment_version_change_blocks_copy(window, qapp):
    window, api = window
    qapp.clipboard().clear()
    window.values["password"] = "do-not-copy"
    window.copy_field("password")
    _, done, _ = api.take("/accounts/5?")
    item = account()
    item["assignment_version"] += 1
    done(item, None)
    assert not window.values
    assert qapp.clipboard().text() == ""


def test_confirmed_submit_removes_task_and_reads_next_current_page(window, qapp):
    window, api = window
    image = QImage(30, 30, QImage.Format.Format_RGB32)
    image.fill(Qt.GlobalColor.white)
    qapp.clipboard().setImage(image)
    window.paste()
    window.shots[0].attachment_id = "c" * 64
    window.finish_submit()
    _, done, _ = api.take("/submit?")
    done({"id": 10, "assignment_id": 105}, None)
    assert not window.shots
    assert not window.values
    path, _, _ = api.take("/accounts?")
    assert parse_qs(urlsplit(path).query)["page"] == ["1"]


def test_long_values_expand_and_native_lock_clears_values(window, qtbot, qapp):
    import ctypes
    from ctypes import wintypes

    window, _ = window
    window.resize(320, 720)
    field = window.fields["email"]
    field.set_value("long-" + "x" * 180 + "@example.test")
    qtbot.wait(150)
    assert field.text_label.height() >= field.text_label.heightForWidth(field.text_label.width())
    assert window.detail_area.horizontalScrollBar().maximum() == 0
    window.pin.setChecked(False)
    if qapp.platformName() == "windows":
        assert window.wts_registered
        assert window.wts_handle == int(window.winId())
    window.values["password"] = "private"
    window.password.setText("login-private")
    message = wintypes.MSG()
    message.message, message.wParam = 0x02B1, 0x7
    window.nativeEvent(b"windows_generic_MSG", ctypes.addressof(message))
    assert not window.values
    assert window.password.text() == ""
    assert window.hidden_private
