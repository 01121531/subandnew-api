import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

from PySide6.QtCore import QMimeData, QObject, Qt, Signal
from PySide6.QtGui import QCloseEvent, QImage, QKeySequence
from PySide6.QtTest import QTest
from PySide6.QtWidgets import QApplication, QMessageBox, QPlainTextEdit, QTextEdit

from mailbox_assistant.api import ApiError
from mailbox_assistant.storage import SessionStore
from mailbox_assistant.window import Shot, Window


class FakeApi(QObject):
    auth_failed = Signal()
    origin = "https://example.test"

    def __init__(self):
        super().__init__()
        self.calls = []

    def call(self, path, callback, **options):
        self.calls.append((path, callback, options))

    def cancel(self):
        pass

    def clear(self):
        pass

    def take(self, route):
        for index, call in enumerate(self.calls):
            if route in call[0]:
                return self.calls.pop(index)
        raise AssertionError(f"Missing route: {route}")


class SubmissionRemarksTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.app = QApplication.instance() or QApplication([])

    def setUp(self):
        self.directory = tempfile.TemporaryDirectory()
        self.api = FakeApi()
        self.window = Window(api=self.api, store=SessionStore(Path(self.directory.name)))
        self.app.processEvents()
        self.window.ticker.stop()
        self.window.verify_timer.stop()
        self.reset_task()
        self.window.show()
        self.app.processEvents()

    def tearDown(self):
        self.window.clear_task(clear_draft=True)
        self.window.tray.hide()
        self.window.quitting = True
        self.window.close()
        self.window.deleteLater()
        self.app.clipboard().clear()
        self.app.processEvents()
        self.directory.cleanup()

    def reset_task(self, kind="refund"):
        window = self.window
        window.clear_task(clear_draft=True)
        window.operator_id = 1
        window.hidden_private = False
        window.kind = kind
        window.pool.blockSignals(True)
        window.pool.setCurrentIndex(0 if kind == "refund" else 1)
        window.pool.blockSignals(False)
        self.account = dict(
            id=5,
            assignment_id=105,
            assignment_version=1,
            account_type=kind,
            operator_id=1,
            status="pending",
            credentials_available=True,
            version=1,
            email="test@example.test",
        )
        window.current = dict(self.account)
        window.items = [window.current, {**self.account, "id": 6, "assignment_id": 106}]
        window.index = 0
        window.page = 1
        window.stack.setCurrentIndex(1)
        window.set_busy(False)
        self.api.calls.clear()

    def add_shot(self, uploaded=True):
        image = QImage(30, 30, QImage.Format.Format_RGB32)
        image.fill(Qt.GlobalColor.white)
        self.window.shots.append(Shot(image, b"image", "a" * 64 if uploaded else ""))
        self.window.render_shots()

    def verify(self):
        self.api.take("/accounts/5?")[1](dict(self.account), None)

    def test_screenshot_only_remark_only_and_both_for_each_task_type(self):
        for kind in ("refund", "opening"):
            for has_shot, remark in ((True, " \n\t"), (False, "Done"), (True, "Done")):
                with self.subTest(kind=kind, has_shot=has_shot, remark=remark):
                    self.reset_task(kind)
                    if has_shot:
                        self.add_shot()
                    self.window.remark.setPlainText("  " + remark + " \n")
                    self.window.redacted.setChecked(has_shot)
                    self.window.submit()
                    self.assertTrue(self.window.busy)
                    self.assertFalse(self.window.remark.isEnabled())
                    self.verify()
                    self.verify()
                    path, callback, options = self.api.take("/submit?")
                    self.assertIn(f"account_type={kind}", path)
                    expected = {"version": 1, "attachment_ids": ["a" * 64] if has_shot else []}
                    if remark.strip():
                        expected["remark"] = "Done"
                    self.assertEqual(options["body"], expected)
                    self.assertEqual(options["method"], "POST")
                    self.assertFalse(self.api.calls)
                    callback({"assignment_id": 105, "remark": remark.strip()}, None)
                    self.assertEqual(self.window.remark.toPlainText(), "")
                    self.assertFalse(self.window.shots)
                    self.api.take("/accounts?")
                    self.assertEqual(list(Path(self.directory.name).rglob("*")), [])

    def test_empty_and_unicode_whitespace_do_not_submit(self):
        for text in ("", " \t\n", "\u3000\u2003"):
            with self.subTest(text=text):
                self.window.remark.setPlainText(text)
                self.window.submit()
                self.assertFalse(self.api.calls)
                self.assertFalse(self.window.busy)

    def test_unicode_limit_counts_code_points_not_utf16_units(self):
        text = "\U0001f600\u6210" * 1000
        self.assertIsInstance(self.window.remark, QTextEdit)
        self.window.remark.setPlainText(text)
        self.assertEqual(self.window.remark.toPlainText(), text)
        self.window.remark.moveCursor(self.window.remark.textCursor().MoveOperation.End)
        self.window.remark.insertPlainText("\U0001f600extra")
        self.assertEqual(self.window.remark.toPlainText(), text)
        self.assertEqual(self.window.remark.textCursor().position(), 3000)
        self.window.submit()
        self.verify()
        self.verify()
        self.assertEqual(self.api.take("/submit?")[2]["body"]["remark"], text)

    def test_defensive_validation_rejects_overlong_remark(self):
        self.window.remark.blockSignals(True)
        self.window.remark.setPlainText("x" * 2001)
        self.window.remark.blockSignals(False)
        self.window.submit()
        self.assertFalse(self.api.calls)

    def test_paste_is_plain_text_and_obeys_limit(self):
        mime = QMimeData()
        mime.setHtml("<b>Not rich text</b>")
        mime.setText("\U0001f600" * 2001)
        self.app.clipboard().setMimeData(mime)
        self.window.remark.paste()
        self.assertFalse(self.window.remark.acceptRichText())
        self.assertEqual(self.window.remark.toPlainText(), "\U0001f600" * 2000)
        self.assertFalse(self.window.shots)

    def test_keyboard_paste_enters_remark_not_screenshot(self):
        self.app.clipboard().setText("Pasted remark")
        self.window.activateWindow()
        self.window.remark.setFocus()
        self.app.processEvents()
        QTest.keySequence(self.window.remark, QKeySequence.StandardKey.Paste)
        self.assertEqual(self.window.remark.toPlainText(), "Pasted remark")
        self.assertFalse(self.window.shots)

    def test_opening_screenshots_still_require_redaction_confirmation(self):
        self.reset_task("opening")
        self.add_shot()
        self.window.remark.setPlainText("Done")
        self.window.submit()
        self.assertFalse(self.api.calls)
        self.assertFalse(self.window.busy)

    def test_upload_and_submit_lock_remark_and_navigation(self):
        self.add_shot(uploaded=False)
        self.window.remark.setPlainText("Done")
        self.window.submit()
        self.verify()
        self.assertFalse(self.window.remark.isEnabled())
        self.assertFalse(self.window.next.isEnabled())
        self.assertFalse(self.window.pool.isEnabled())
        self.assertFalse(self.window.report_button.isEnabled())
        self.assertFalse(self.window.discard_allowed())
        self.api.take("/attachments?")[1]({"id": "a" * 64}, None)
        self.verify()
        self.assertFalse(self.window.remark.isEnabled())
        self.assertEqual(self.api.take("/submit?")[2]["body"]["remark"], "Done")

    def test_definite_failure_preserves_editable_draft(self):
        self.window.remark.setPlainText("  Done  ")
        self.window.finish_submit()
        self.api.take("/submit?")[1](None, ApiError("mailbox_request_failed", 400))
        self.assertFalse(self.window.uncertain_submission)
        self.assertTrue(self.window.remark.isEnabled())
        self.assertEqual(self.window.remark.toPlainText(), "  Done  ")

    def test_revalidation_failure_preserves_remark_without_posting(self):
        self.window.remark.setPlainText("Draft")
        self.window.submit()
        self.api.take("/accounts/5?")[1](None, ApiError("mailbox_network_error"))
        self.assertFalse(self.window.busy)
        self.assertFalse(self.api.calls)
        self.assertTrue(self.window.remark.isEnabled())
        self.assertEqual(self.window.remark.toPlainText(), "Draft")

    def test_late_submit_callback_cannot_clear_new_task_remark(self):
        self.window.remark.setPlainText("Old draft")
        self.window.submit()
        self.verify()
        self.verify()
        _, callback, _ = self.api.take("/submit?")
        self.reset_task("opening")
        self.window.remark.setPlainText("New draft")
        callback({"assignment_id": 105, "remark": "Old draft"}, None)
        self.assertEqual(self.window.remark.toPlainText(), "New draft")
        self.assertFalse(self.api.calls)

    def test_interrupted_remark_only_post_keeps_uncertain_lock_and_never_replays(self):
        self.window.remark.setPlainText("Done")
        self.window.submit()
        self.verify()
        self.verify()
        _, callback, _ = self.api.take("/submit?")
        self.window.suspend_private()
        callback({"assignment_id": 105, "remark": "Done"}, None)
        self.assertEqual(self.window.remark.toPlainText(), "Done")
        self.assertTrue(self.window.uncertain_submission)
        self.window.resume_private()
        self.verify()
        self.assertFalse(self.window.remark.isEnabled())
        self.api.calls.clear()
        self.window.submit()
        self.assertEqual(len(self.api.calls), 1)
        _, callback, options = self.api.take("/accounts/5?")
        self.assertEqual(options.get("method", "GET"), "GET")
        callback(dict(self.account), None)
        self.assertFalse(self.window.remark.isEnabled())
        self.assertEqual(self.window.remark.toPlainText(), "Done")

    def test_identical_remark_after_rejection_cannot_match_old_submission(self):
        original_call = self.api.call
        for kind in ("refund", "opening"):
            with self.subTest(kind=kind):
                self.reset_task(kind)
                self.account.update(status="rejected", assignment_version=3)
                self.window.current.update(self.account)
                self.window.remark.setPlainText("  Done\nexact  ")
                self.window.submit()
                self.verify()
                self.verify()
                _, submitted, _ = self.api.take("/submit?")

                def respond_to_old_history(path, callback, **options):
                    if path.startswith("/submissions?"):
                        callback(
                            {
                                "items": [
                                    {
                                        "assignment_id": 105,
                                        "remark": "Done\nexact",
                                        "status": "rejected",
                                        "attachments": [],
                                    }
                                ]
                            },
                            None,
                        )
                    else:
                        original_call(path, callback, **options)

                with patch.object(self.api, "call", side_effect=respond_to_old_history):
                    submitted(None, ApiError("mailbox_network_error", uncertain=True))
                _, callback, options = self.api.take("/accounts/5?")
                self.assertEqual(options.get("method", "GET"), "GET")
                callback(dict(self.account), None)
                self.assertTrue(self.window.uncertain_submission)
                self.assertEqual(self.window.remark.toPlainText(), "  Done\nexact  ")
                self.assertFalse(self.window.remark.isEnabled())
                self.assertFalse(self.api.calls)
                self.window.submit()
                self.assertEqual(len(self.api.calls), 1)
                self.api.take("/accounts/5?")[1](
                    {**self.account, "assignment_version": 4, "status": "submitted"}, None
                )
                self.assertFalse(self.window.uncertain_submission)
                self.assertEqual(self.window.remark.toPlainText(), "")
                self.api.take("/accounts?")

    def test_normal_reconcile_requires_same_identity_newer_version_and_success_status(self):
        valid = {**self.account, "assignment_version": 2, "status": "submitted"}
        invalid = [
            {**valid, "id": 6},
            {**valid, "assignment_id": 106},
            {**valid, "operator_id": 2},
            {**valid, "account_type": "opening"},
            {**valid, "assignment_version": 1},
            {**valid, "assignment_version": 0},
            {**valid, "assignment_version": None},
            {**valid, "assignment_version": "2"},
            {**valid, "assignment_version": True},
            {**valid, "status": "pending"},
            {**valid, "status": "rejected"},
            {**valid, "status": "issue_pending"},
            {**valid, "status": "unassigned"},
            {},
            None,
        ]
        for response in invalid:
            with self.subTest(response=response):
                self.reset_task()
                self.window.remark.setPlainText("Draft")
                self.window.finish_submit()
                self.api.take("/submit?")[1](None, None)
                self.api.take("/accounts/5?")[1](response, None)
                self.assertTrue(self.window.uncertain_submission)
                self.assertFalse(self.window.remark.isEnabled())
                self.assertEqual(self.window.remark.toPlainText(), "Draft")
                self.assertFalse(self.api.calls)

    def test_normal_reconcile_failure_and_stale_callback_cannot_clear_draft(self):
        self.window.remark.setPlainText("Old draft")
        self.window.finish_submit()
        self.api.take("/submit?")[1](None, None)
        self.api.take("/accounts/5?")[1](None, ApiError("mailbox_network_error"))
        self.assertFalse(self.window.busy)
        self.assertFalse(self.window.remark.isEnabled())
        self.assertEqual(self.window.remark.toPlainText(), "Old draft")
        self.window.submit()
        _, callback, _ = self.api.take("/accounts/5?")
        self.reset_task("opening")
        self.window.remark.setPlainText("New draft")
        callback({**self.account, "assignment_version": 2, "status": "submitted"}, None)
        self.assertEqual(self.window.remark.toPlainText(), "New draft")
        self.assertFalse(self.api.calls)

    def test_already_approved_newer_assignment_confirms_remark_only_submission(self):
        self.window.remark.setPlainText("Done")
        self.window.finish_submit()
        self.api.take("/submit?")[1](None, None)
        self.api.take("/accounts/5?")[1](
            {
                **self.account,
                "assignment_version": 3,
                "status": "approved",
                "credentials_available": False,
            },
            None,
        )
        self.assertFalse(self.window.uncertain_submission)
        self.assertEqual(self.window.remark.toPlainText(), "")
        self.api.take("/accounts?")

    def test_screenshot_only_submission_uses_authoritative_account_to_reconcile(self):
        self.add_shot()
        self.window.finish_submit()
        self.api.take("/submit?")[1](None, None)
        self.api.take("/accounts/5?")[1](
            {**self.account, "assignment_version": 2, "status": "submitted"}, None
        )
        self.assertFalse(self.window.uncertain_submission)
        self.assertFalse(self.window.shots)

    def test_remark_draft_requires_confirmation_and_clears_on_discard(self):
        actions = {
            "next": lambda: self.window.navigate(1),
            "type": lambda: self.window.pool.setCurrentIndex(1),
            "feedback": self.window.toggle_issue,
            "history": self.window.open_history,
            "refresh": self.window.refresh_tasks,
            "close": lambda: self.window.closeEvent(QCloseEvent()),
        }
        for name, action in actions.items():
            with self.subTest(action=name):
                self.reset_task()
                self.window.remark.setPlainText("Draft")
                with patch.object(QMessageBox, "question", return_value=QMessageBox.Cancel) as ask:
                    action()
                ask.assert_called_once()
                self.assertEqual(self.window.remark.toPlainText(), "Draft")
                self.assertFalse(self.api.calls)
                self.assertEqual(self.window.kind, "refund")
                self.assertFalse(self.window.issue_mode)
                with (
                    patch.object(QMessageBox, "question", return_value=QMessageBox.Discard) as ask,
                    patch(
                        "mailbox_assistant.window.QSystemTrayIcon.isSystemTrayAvailable",
                        return_value=True,
                    ),
                ):
                    action()
                ask.assert_called_once()
                self.assertEqual(self.window.remark.toPlainText(), "")

    def test_feedback_does_not_send_remark_and_return_clears_feedback(self):
        self.window.toggle_issue()
        self.assertTrue(self.window.remark_fields.isHidden())
        self.window.issue_description.setPlainText("Issue")
        self.window.finish_submit()
        _, callback, options = self.api.take("/issues?")
        self.assertNotIn("remark", options["body"])
        callback(None, ApiError("mailbox_request_failed", 400))
        with patch.object(QMessageBox, "question", return_value=QMessageBox.Discard):
            self.window.toggle_issue()
        self.assertFalse(self.window.issue_mode)
        self.assertFalse(self.window.remark_fields.isHidden())
        self.assertEqual(self.window.issue_description.toPlainText(), "")
        self.assertEqual(self.window.remark.toPlainText(), "")

    def test_suspend_resume_keeps_remark_draft_and_revalidates(self):
        self.window.remark.setPlainText("Draft")
        self.window.suspend_private()
        self.assertEqual(self.window.remark.toPlainText(), "Draft")
        self.assertFalse(self.window.remark.isEnabled())
        self.window.resume_private()
        self.assertTrue(self.window.busy)
        self.verify()
        self.assertEqual(self.window.remark.toPlainText(), "Draft")
        self.assertTrue(self.window.remark.isEnabled())
        self.assertTrue(all("/accounts?" not in call[0] for call in self.api.calls))

    def test_session_expiry_clears_remark(self):
        self.window.remark.setPlainText("Draft")
        self.window.session_expired()
        self.assertEqual(self.window.remark.toPlainText(), "")

    def test_history_is_readonly_plain_text_and_fits_narrow_window(self):
        text = "<img src='file:///private'>" + "\U0001f600" * 1900
        self.window.history_items = [{"remark": text, "review_reason": "Reviewed"}, {}]
        self.window.stack.setCurrentIndex(2)
        self.window.resize(320, 720)
        self.window.show_history_item(0)
        self.app.processEvents()
        self.assertIsInstance(self.window.history_details, QPlainTextEdit)
        self.assertIn(text, self.window.history_details.toPlainText())
        self.assertIn("Reviewed", self.window.history_details.toPlainText())
        self.assertTrue(self.window.history_details.isReadOnly())
        self.assertLessEqual(self.window.history_details.height(), 180)
        self.assertEqual(self.window.width(), 320)
        self.window.show_history_item(1)
        self.assertNotIn(text, self.window.history_details.toPlainText())
        self.assertEqual(self.window.history_images.count(), 0)

    def test_remark_editor_fits_narrow_task_window(self):
        self.window.resize(320, 720)
        self.window.remark.setPlainText("x" * 2000)
        self.app.processEvents()
        self.assertEqual(self.window.width(), 320)
        self.assertEqual(self.window.remark.height(), 90)
        self.assertEqual(self.window.detail_area.horizontalScrollBar().maximum(), 0)


if __name__ == "__main__":
    unittest.main()
