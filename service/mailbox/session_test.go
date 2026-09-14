package mailbox

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/01121531/subandnew-api/model"
	"github.com/01121531/subandnew-api/service/authz"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func TestMailboxSessionPersistenceExpiryAndLogout(t *testing.T) {
	s, admin := operatorTestService(t)
	ctx := context.Background()
	item := operatorTestCreate(t, s, admin, "portal.user")
	p, raw, err := s.Login(ctx, " PORTAL.USER ", "password-123", "192.0.2.2")
	require.NoError(t, err)
	require.Len(t, raw, 43)
	require.Empty(t, p.Operator.PasswordHash)
	require.Equal(t, digest(raw), p.Session.TokenHash)
	require.NotEqual(t, raw, p.Session.TokenHash)
	require.Equal(t, int64(SessionTTL/time.Second), p.Session.ExpiresAt-p.Session.CreatedAt)
	restarted := New(s.DB)
	restarted.Now = s.Now
	fresh, err := restarted.Authenticate(raw)
	require.NoError(t, err)
	require.Empty(t, fresh.Operator.PasswordHash)
	require.Equal(t, item.ID, fresh.Operator.ID)
	_, other, err := s.Login(ctx, item.Username, "password-123", "192.0.2.3")
	require.NoError(t, err)
	require.NotEqual(t, raw, other)
	operatorTestError(t, s.Logout(ctx, admin), 403, "mailbox_permission_denied")
	require.NoError(t, s.Logout(ctx, PortalActor(p, "192.0.2.2")))
	_, err = s.Authenticate(raw)
	operatorTestError(t, err, 401, "mailbox_session_expired")
	_, err = s.Authenticate(other)
	require.NoError(t, err)
	s.Now = func() time.Time { return time.Unix(p.Session.ExpiresAt, 0) }
	_, err = s.Authenticate(other)
	operatorTestError(t, err, 401, "mailbox_session_expired")
	for _, invalid := range []string{"", strings.Repeat("x", 43), p.Session.TokenHash} {
		_, err = s.Authenticate(invalid)
		operatorTestError(t, err, 401, "mailbox_session_expired")
	}
	require.Empty(t, PortalActor(nil, "test").SessionHash)
}

func TestMailboxSessionFreshChecks(t *testing.T) {
	for _, mutation := range []string{"disabled", "version", "session-version", "deleted-session", "deleted-operator"} {
		t.Run(mutation, func(t *testing.T) {
			s, admin := operatorTestService(t)
			ctx := context.Background()
			item := operatorTestCreate(t, s, admin, "portal.user")
			p, raw, err := s.Login(ctx, item.Username, "password-123", "192.0.2.2")
			require.NoError(t, err)
			switch mutation {
			case "disabled":
				err = s.DB.Model(&model.MailboxOperator{}).Where("id = ?", item.ID).UpdateColumn("enabled", false).Error
			case "version":
				err = s.DB.Model(&model.MailboxOperator{}).Where("id = ?", item.ID).UpdateColumn("auth_version", 2).Error
			case "session-version":
				err = s.DB.Model(&model.MailboxSession{}).Where("id = ?", p.Session.ID).UpdateColumn("auth_version", 2).Error
			case "deleted-session":
				err = s.DB.Delete(p.Session).Error
			case "deleted-operator":
				err = s.DB.Delete(&model.MailboxOperator{}, item.ID).Error
			}
			require.NoError(t, err)
			_, err = s.Authenticate(raw)
			operatorTestError(t, err, 401, "mailbox_session_expired")
			actor := PortalActor(p, "")
			operatorTestError(t, s.CheckActor(actor, authz.MailboxView), 401, "mailbox_session_expired")
			operatorTestError(t, s.ChangePassword(ctx, actor, "password-123", "changed-password"), 401, "mailbox_session_expired")
			operatorTestError(t, s.Logout(ctx, actor), 401, "mailbox_session_expired")
		})
	}
}

func TestMailboxSessionChangePassword(t *testing.T) {
	s, admin := operatorTestService(t)
	ctx := context.Background()
	item := operatorTestCreate(t, s, admin, "portal.user")
	p, raw, err := s.Login(ctx, item.Username, "password-123", "192.0.2.2")
	require.NoError(t, err)
	_, other, err := s.Login(ctx, item.Username, "password-123", "192.0.2.3")
	require.NoError(t, err)
	actor := PortalActor(p, "")
	operatorTestError(t, s.ChangePassword(ctx, admin, "password-123", "changed-password"), 403, "mailbox_permission_denied")
	operatorTestError(t, s.ChangePassword(ctx, actor, "incorrect", "changed-password"), 400, "mailbox_current_password_invalid")
	operatorTestError(t, s.ChangePassword(ctx, actor, "password-123", "short"), 400, "mailbox_invalid_password")
	_, err = s.Authenticate(raw)
	require.NoError(t, err)
	password := " " + strings.Repeat("x", 70) + " "
	require.NoError(t, s.ChangePassword(ctx, actor, "password-123", password))
	for _, token := range []string{raw, other} {
		_, err = s.Authenticate(token)
		operatorTestError(t, err, 401, "mailbox_session_expired")
	}
	_, _, err = s.Login(ctx, item.Username, "password-123", "192.0.2.4")
	operatorTestError(t, err, 401, "mailbox_invalid_credentials")
	_, _, err = s.Login(ctx, item.Username, password, "192.0.2.4")
	require.NoError(t, err)
	_, _, err = s.Login(ctx, item.Username, strings.TrimSpace(password), "192.0.2.4")
	operatorTestError(t, err, 401, "mailbox_invalid_credentials")
}

func TestMailboxSessionPersistentHashedRateLimit(t *testing.T) {
	s, admin := operatorTestService(t)
	ctx := context.Background()
	operatorTestCreate(t, s, admin, "portal.user")
	for i := 0; i < 5; i++ {
		restarted := New(s.DB)
		restarted.Now = s.Now
		_, _, err := restarted.Login(ctx, " PORTAL.USER ", "incorrect", "192.0.2.2")
		operatorTestError(t, err, 401, "mailbox_invalid_credentials")
	}
	_, _, err := s.Login(ctx, "portal.user", "password-123", "192.0.2.2")
	operatorTestError(t, err, 429, "mailbox_login_rate_limited")
	var rows []model.MailboxLoginAttempt
	require.NoError(t, s.DB.Find(&rows).Error)
	require.Len(t, rows, 1)
	require.Equal(t, digest("mailbox-login:v1:portal.user\x00192.0.2.2"), rows[0].Key)
	require.Equal(t, 5, rows[0].Failures)
	require.NotContains(t, rows[0].Key, "portal.user")
	require.NotContains(t, rows[0].Key, "192.0.2.2")
	_, _, err = s.Login(ctx, "portal.user", "password-123", "192.0.2.3")
	require.NoError(t, err)
	start := s.Now()
	s.Now = func() time.Time { return start.Add(operatorLoginWindow - time.Second) }
	_, _, err = s.Login(ctx, "portal.user", "password-123", "192.0.2.2")
	operatorTestError(t, err, 429, "mailbox_login_rate_limited")
	s.Now = func() time.Time { return start.Add(operatorLoginWindow) }
	_, _, err = s.Login(ctx, "portal.user", "password-123", "192.0.2.2")
	require.NoError(t, err)
	var row model.MailboxLoginAttempt
	require.NoError(t, s.DB.Where(clause.Eq{Column: "key", Value: rows[0].Key}).First(&row).Error)
	require.Zero(t, row.Failures)
	require.Equal(t, s.Now().Unix(), row.WindowStart)
}

func TestMailboxSessionUnknownAndDisabledAreIndistinguishable(t *testing.T) {
	s, admin := operatorTestService(t)
	ctx := context.Background()
	item := operatorTestCreate(t, s, admin, "disabled.user")
	_, err := s.SaveOperator(ctx, admin, item.ID, OperatorInput{Username: item.Username, DisplayName: item.DisplayName, Version: item.Version})
	require.NoError(t, err)
	for _, username := range []string{"missing.user", "disabled.user", "!invalid"} {
		for i := 0; i < 5; i++ {
			_, raw, err := s.Login(ctx, username, "password-123", "192.0.2.2")
			require.Empty(t, raw)
			operatorTestError(t, err, 401, "mailbox_invalid_credentials")
		}
		_, _, err := s.Login(ctx, username, "password-123", "192.0.2.2")
		operatorTestError(t, err, 429, "mailbox_login_rate_limited")
	}
}

func TestMailboxSessionConcurrentLoginLimit(t *testing.T) {
	s, _ := operatorTestService(t)
	var wg sync.WaitGroup
	results := make(chan error, 16)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			other := New(s.DB)
			other.Now = s.Now
			_, _, err := other.Login(context.Background(), "unknown.user", "incorrect", "192.0.2.2")
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	invalid, limited := 0, 0
	for err := range results {
		status, code := HTTPError(err)
		switch status {
		case 401:
			require.Equal(t, "mailbox_invalid_credentials", code)
			invalid++
		case 429:
			require.Equal(t, "mailbox_login_rate_limited", code)
			limited++
		default:
			t.Fatalf("unexpected login error: %v", err)
		}
	}
	require.Equal(t, 5, invalid)
	require.Equal(t, 11, limited)
}

func TestMailboxSessionSuccessPreservesOtherFailures(t *testing.T) {
	s, admin := operatorTestService(t)
	ctx := context.Background()
	operatorTestCreate(t, s, admin, "portal.user")
	for i := 0; i < 3; i++ {
		_, _, err := s.Login(ctx, "portal.user", "incorrect", "192.0.2.2")
		require.Error(t, err)
	}
	for i := 0; i < 3; i++ {
		_, _, err := s.Login(ctx, "portal.user", "password-123", "192.0.2.2")
		require.NoError(t, err)
	}
	var row model.MailboxLoginAttempt
	require.NoError(t, s.DB.First(&row).Error)
	require.Equal(t, 3, row.Failures)
}

func TestMailboxSessionLoginRechecksAfterPasswordRead(t *testing.T) {
	for _, mutation := range []string{"disable", "reset", "revoke"} {
		t.Run(mutation, func(t *testing.T) {
			s, admin := operatorTestService(t)
			item := operatorTestCreate(t, s, admin, "portal.user")
			changed := false
			require.NoError(t, s.DB.Callback().Query().After("gorm:query").Register("mailbox_test_login_revoke", func(tx *gorm.DB) {
				if changed || tx.Statement.Table != "mailbox_operators" {
					return
				}
				changed = true
				updates := map[string]any{"auth_version": 2, "version": 2}
				if mutation == "disable" {
					updates["enabled"] = false
				}
				if mutation == "reset" {
					updates["password_hash"] = operatorDummyHash
				}
				require.NoError(t, s.DB.Model(&model.MailboxOperator{}).Where("id = ?", item.ID).Updates(updates).Error)
			}))
			p, raw, err := s.Login(context.Background(), item.Username, "password-123", "192.0.2.2")
			operatorTestError(t, err, 401, "mailbox_invalid_credentials")
			require.Nil(t, p)
			require.Empty(t, raw)
			require.True(t, changed)
			var count int64
			require.NoError(t, s.DB.Model(&model.MailboxSession{}).Count(&count).Error)
			require.Zero(t, count)
		})
	}
}

func TestMailboxSessionAuthenticateRechecksSessionAfterOperatorRead(t *testing.T) {
	s, admin := operatorTestService(t)
	item := operatorTestCreate(t, s, admin, "portal.user")
	p, raw, err := s.Login(context.Background(), item.Username, "password-123", "192.0.2.2")
	require.NoError(t, err)
	changed := false
	require.NoError(t, s.DB.Callback().Query().After("gorm:query").Register("mailbox_test_auth_revoke", func(tx *gorm.DB) {
		if changed || tx.Statement.Table != "mailbox_operators" {
			return
		}
		changed = true
		require.NoError(t, s.DB.Delete(p.Session).Error)
	}))
	_, err = s.Authenticate(raw)
	operatorTestError(t, err, 401, "mailbox_session_expired")
	require.True(t, changed)
}

func TestMailboxSessionLimiterUnavailableFailsClosed(t *testing.T) {
	s, admin := operatorTestService(t)
	item := operatorTestCreate(t, s, admin, "portal.user")
	require.NoError(t, s.DB.Migrator().DropTable(&model.MailboxLoginAttempt{}))
	p, raw, err := s.Login(context.Background(), item.Username, "password-123", "192.0.2.2")
	require.Error(t, err)
	require.Nil(t, p)
	require.Empty(t, raw)
	var count int64
	require.NoError(t, s.DB.Model(&model.MailboxSession{}).Count(&count).Error)
	require.Zero(t, count)
}

func TestMailboxSessionFailureWindowStartsAfterSuccessfulLogin(t *testing.T) {
	s, admin := operatorTestService(t)
	item := operatorTestCreate(t, s, admin, "portal.user")
	_, _, err := s.Login(context.Background(), item.Username, "password-123", "192.0.2.2")
	require.NoError(t, err)
	start := s.Now()
	s.Now = func() time.Time { return start.Add(14 * time.Minute) }
	for i := 0; i < 5; i++ {
		_, _, err = s.Login(context.Background(), item.Username, "incorrect", "192.0.2.2")
		operatorTestError(t, err, 401, "mailbox_invalid_credentials")
	}
	s.Now = func() time.Time { return start.Add(15 * time.Minute) }
	_, _, err = s.Login(context.Background(), item.Username, "password-123", "192.0.2.2")
	operatorTestError(t, err, 429, "mailbox_login_rate_limited")
	s.Now = func() time.Time { return start.Add(29 * time.Minute) }
	_, _, err = s.Login(context.Background(), item.Username, "password-123", "192.0.2.2")
	require.NoError(t, err)
}
