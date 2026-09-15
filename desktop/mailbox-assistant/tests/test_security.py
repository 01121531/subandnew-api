import time

import pytest

from mailbox_assistant.api import Api, normalize_origin
from mailbox_assistant.security import ClipboardLease, actionable, assignment_scope
from mailbox_assistant.storage import SessionStore, dpapi


@pytest.mark.parametrize(
    "value",
    [
        "http://example.test",
        "https://user:pass@example.test",
        "https://example.test/path",
        "https://example.test?a=b",
        "https://example.test#secret",
        "https://example.test:99999",
        "https://exa mple.test",
        "file:///secret",
    ],
)
def test_origins_reject_unsafe_input(value):
    with pytest.raises(ValueError):
        normalize_origin(value)


def test_origin_and_cookie_isolation(qapp):
    api = Api()
    api.configure("https://EXAMPLE.test:443/")
    assert api.origin == "https://example.test"
    api.restore_cookie("a" * 43)
    assert api.cookie() == "a" * 43
    api.configure("https://other.test")
    assert api.cookie() == ""
    assert api.csrf == ""


class MemoryClipboard:
    value = ""

    def setText(self, value):
        self.value = value

    def text(self):
        return self.value

    def clear(self):
        self.value = ""


def test_clipboard_preserves_exact_values_and_does_not_erase_other_app(qapp):
    clipboard = MemoryClipboard()
    lease = ClipboardLease(clipboard)
    for value in ("  exact password  ", "000012", "001", "4242424242424242", "09/29"):
        lease.copy(value)
        assert clipboard.text() == value
        lease.clear()
        assert clipboard.text() == ""
    lease.copy("secret")
    clipboard.setText("copied elsewhere")
    lease.clear()
    assert clipboard.text() == "copied elsewhere"


def test_dpapi_round_trip_scope_and_no_plaintext(tmp_path, qapp):
    value = b"synthetic-session-" * 3
    encrypted = dpapi(value, b"scope-one")
    assert value not in encrypted
    assert dpapi(encrypted, b"scope-one", decrypt=True) == value
    with pytest.raises(ValueError):
        dpapi(encrypted, b"scope-two", decrypt=True)
    store = SessionStore(tmp_path)
    store.save("https://example.test", "operator", 1, value.decode(), int(time.time()) + 60)
    assert value not in store.path.read_bytes()
    assert store.load("https://other.test", "operator") is None
    assert store.load("https://example.test", "another") is None
    assert store.load("https://example.test", "operator") == (value.decode(), 1)
    store.clear()
    assert "protected_session" not in store.read()


def test_assignment_scope_ignores_upload_account_version():
    account = dict(
        id=2,
        account_type="refund",
        assignment_id=8,
        assignment_version=1,
        operator_id=3,
        status="pending",
        credentials_available=True,
        version=1,
    )
    changed = {**account, "version": 2}
    assert assignment_scope(account) == assignment_scope(changed)
    assert actionable(account, "refund")
    assert not actionable(account, "opening")
    assert not actionable({**account, "status": "submitted"}, "refund")
    assert assignment_scope(account) != assignment_scope({**account, "assignment_version": 2})
