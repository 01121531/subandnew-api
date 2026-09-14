package mailbox

import (
	"context"
	"encoding/base32"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/01121531/subandnew-api/common"
	"github.com/01121531/subandnew-api/model"
	"github.com/01121531/subandnew-api/service/authz"
	"github.com/01121531/subandnew-api/service/managedinstance"
	"github.com/pquerna/otp"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestMailboxOTPRFC6238Vectors(t *testing.T) {
	vectors := []struct {
		timestamp int64
		codes     [3]string
	}{
		{59, [3]string{"94287082", "46119246", "90693936"}},
		{1111111109, [3]string{"07081804", "68084774", "25091201"}},
		{1111111111, [3]string{"14050471", "67062674", "99943326"}},
		{1234567890, [3]string{"89005924", "91819424", "93441116"}},
		{2000000000, [3]string{"69279037", "90698825", "38618901"}},
		{20000000000, [3]string{"65353130", "77737706", "47863826"}},
	}
	secrets := []string{"12345678901234567890", "12345678901234567890123456789012", "1234567890123456789012345678901234567890123456789012345678901234"}
	for algorithm, name := range []string{"SHA1", "SHA256", "SHA512"} {
		for _, vector := range vectors {
			t.Run(fmt.Sprintf("%s/%d", name, vector.timestamp), func(t *testing.T) {
				secret := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString([]byte(secrets[algorithm]))
				config, err := parseMailboxOTP("otpauth://totp/Test:account?secret=" + secret + "&algorithm=" + name + "&digits=8&period=30&issuer=Test")
				require.NoError(t, err)
				code, err := config.code(time.Unix(vector.timestamp, 0))
				require.NoError(t, err)
				require.Equal(t, vector.codes[algorithm], code)
			})
		}
	}
}

func TestMailboxOTPStrictParsing(t *testing.T) {
	valid := []string{mailboxImportTestSecret, strings.ToLower(mailboxImportTestSecret), " MY====== ", "otpauth://totp/account?secret=" + mailboxImportTestSecret + "&period=15", "otpauth://totp/account?secret=" + mailboxImportTestSecret + "&algorithm=SHA512&digits=8&period=120"}
	for _, raw := range valid {
		_, err := parseMailboxOTP(raw)
		require.NoError(t, err, raw)
	}
	base := "otpauth://totp/account?secret=" + mailboxImportTestSecret
	invalid := []string{"", " ", "null", "[]", "A", "========", "not-a-secret!", "M Y======", "MY===", "MY\x00", "https://example.com", "otpauth://hotp/account?secret=" + mailboxImportTestSecret + "&counter=0", "otpauth://totp/?secret=" + mailboxImportTestSecret, "otpauth://totp/account", "otpauth://user@totp/account?secret=" + mailboxImportTestSecret,
		base + "&secret=" + mailboxImportTestSecret, base + "&%73ecret=" + mailboxImportTestSecret, base + "&algorithm=MD5", base + "&algorithm=SHA1&algorithm=SHA256", base + "&digits=7", base + "&digits=06", base + "&period=14", base + "&period=121", base + "&period=0", base + "&period=30.0", base + "&period=+30", base + "&period=", base + "&counter=1", base + "&SECRET=MY", base + "&issuer=a&issuer=b", base + "&issuer=%zz", base + ";digits=8", base + "#secret", base + "&unknown=value"}
	for i, raw := range invalid {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			_, err := parseMailboxOTP(raw)
			require.EqualError(t, err, "mailbox_import_invalid_otp")
		})
	}
	config, err := parseMailboxOTP(mailboxImportTestSecret)
	require.NoError(t, err)
	require.Equal(t, otp.AlgorithmSHA1, config.Algorithm)
	require.Equal(t, otp.DigitsSix, config.Digits)
	require.Equal(t, uint(30), config.Period)
}

func mailboxCredentialTestOperator(t *testing.T, s *Service, accountID int64) Actor {
	t.Helper()
	operator := model.MailboxOperator{Username: "credential-operator", DisplayName: "Operator", PasswordHash: "unused", Enabled: true, AuthVersion: 1, Version: 1}
	require.NoError(t, s.DB.Create(&operator).Error)
	session := model.MailboxSession{TokenHash: "persisted-session-hash", OperatorID: operator.ID, AuthVersion: 1, ExpiresAt: 10000}
	require.NoError(t, s.DB.Create(&session).Error)
	assignment := model.MailboxAssignment{AccountID: accountID, OperatorID: operator.ID, Status: StatusPending, Version: 1}
	require.NoError(t, s.DB.Create(&assignment).Error)
	require.NoError(t, s.DB.Model(&model.MailboxAccount{}).Where("id = ?", accountID).Update("active_assignment_id", assignment.ID).Error)
	return Actor{OperatorID: operator.ID, AuthVersion: 1, SessionHash: session.TokenHash}
}

func TestMailboxCredentialsViewsAndExpiry(t *testing.T) {
	s, actor := newMailboxImportTestService(t)
	account := mailboxImportTestAccount(t, s, actor, "one@example.com", "secret-password", mailboxImportTestSecret)
	for _, timestamp := range []int64{59, 60} {
		s.Now = func() time.Time { return time.Unix(timestamp, 0) }
		view, err := s.Credentials(context.Background(), actor, account.ID, "otp")
		require.NoError(t, err)
		require.Equal(t, timestamp, view.ServerTime)
		require.Equal(t, (timestamp/30+1)*30, view.ExpiresAt)
		require.Len(t, view.Code, 6)
		if timestamp == 59 {
			require.Equal(t, "287082", view.Code)
		}
		encoded, err := json.Marshal(view)
		require.NoError(t, err)
		require.NotContains(t, string(encoded), "password")
		require.NotContains(t, string(encoded), mailboxImportTestSecret)
	}
	custom := mailboxImportTestAccount(t, s, actor, "custom@example.com", "pw", "otpauth://totp/account?secret="+mailboxImportTestSecret+"&digits=8&period=15")
	view, err := s.Credentials(context.Background(), actor, custom.ID, "otp")
	require.NoError(t, err)
	require.Len(t, view.Code, 8)
	require.Equal(t, int64(75), view.ExpiresAt)
	_, err = s.Credentials(context.Background(), actor, account.ID, "raw-secret")
	require.EqualError(t, err, "mailbox_invalid_credential_kind")
}

func TestMailboxCredentialsAccountAndKindCipherBinding(t *testing.T) {
	s, actor := newMailboxImportTestService(t)
	one := mailboxImportTestAccount(t, s, actor, "one@example.com", "first-password", mailboxImportTestSecret)
	two := mailboxImportTestAccount(t, s, actor, "two@example.com", "second-password", mailboxImportTestSecret)
	cipher, err := s.Cipher()
	require.NoError(t, err)
	_, err = cipher.Decrypt(one.ID, "bearer_pat", one.KeyVersion, one.Ciphertext)
	require.Error(t, err)
	require.NoError(t, s.DB.Model(&model.MailboxAccount{}).Where("id = ?", two.ID).Update("ciphertext", one.Ciphertext).Error)
	view, err := s.Credentials(context.Background(), actor, two.ID, "password")
	require.Nil(t, view)
	require.EqualError(t, err, "mailbox_credentials_unavailable")
	var audit model.MailboxAudit
	require.NoError(t, s.DB.Last(&audit).Error)
	require.Equal(t, 503, audit.StatusCode)
	require.Equal(t, "mailbox_credentials_unavailable", audit.ErrorCode)
}

func TestMailboxCredentialsOperatorStatesAndReauthorization(t *testing.T) {
	for _, state := range []string{StatusPending, StatusSubmitted, StatusRejected, StatusApproved, "invalid", "recalled", "disabled", "session", "version", "expired", "other-owner", "late-approved", "late-disabled", "late-recall", "late-version"} {
		t.Run(state, func(t *testing.T) {
			s, admin := newMailboxImportTestService(t)
			account := mailboxImportTestAccount(t, s, admin, "one@example.com", "sensitive-password", mailboxImportTestSecret)
			actor := mailboxCredentialTestOperator(t, s, account.ID)
			change := func() {
				var err error
				switch state {
				case "recalled", "late-recall":
					err = s.DB.Model(&model.MailboxAccount{}).Where("id = ?", account.ID).Update("active_assignment_id", 0).Error
				case "disabled", "late-disabled":
					err = s.DB.Model(&model.MailboxOperator{}).Where("id = ?", actor.OperatorID).Update("enabled", false).Error
				case "session":
					err = s.DB.Where("token_hash = ?", actor.SessionHash).Delete(&model.MailboxSession{}).Error
				case "version":
					err = s.DB.Model(&model.MailboxOperator{}).Where("id = ?", actor.OperatorID).Update("auth_version", 2).Error
				case "expired":
					err = s.DB.Model(&model.MailboxSession{}).Where("token_hash = ?", actor.SessionHash).Update("expires_at", 59).Error
				case "other-owner":
					err = s.DB.Model(&model.MailboxAssignment{}).Where("account_id = ?", account.ID).Update("operator_id", actor.OperatorID+1).Error
				case "late-version":
					err = s.DB.Model(&model.MailboxAccount{}).Where("id = ?", account.ID).Update("version", 2).Error
				default:
					status := state
					if state == "late-approved" {
						status = StatusApproved
					}
					err = s.DB.Model(&model.MailboxAssignment{}).Where("account_id = ?", account.ID).Update("status", status).Error
				}
				require.NoError(t, err)
			}
			if strings.HasPrefix(state, "late-") {
				original := s.Cipher
				s.Cipher = func() (*managedinstance.CredentialCipher, error) { change(); return original() }
			} else {
				change()
			}
			view, err := s.Credentials(context.Background(), actor, account.ID, "password")
			if state == StatusPending || state == StatusSubmitted || state == StatusRejected {
				require.NoError(t, err)
				require.Equal(t, "sensitive-password", view.Password)
			} else {
				require.Error(t, err)
				require.Nil(t, view)
			}
		})
	}
}

func TestMailboxImportAndCredentialsRequireGrants(t *testing.T) {
	s, admin := newMailboxImportTestService(t)
	account := mailboxImportTestAccount(t, s, admin, "one@example.com", "pw", mailboxImportTestSecret)
	operator := mailboxCredentialTestOperator(t, s, account.ID)
	require.NoError(t, s.DB.Create(&model.User{Id: 2, Username: "restricted", Role: common.RoleAdminUser, Status: common.UserStatusEnabled}).Error)
	access, err := authz.LoadDataAccess(s.DB, 2)
	require.NoError(t, err)
	for _, actor := range []Actor{{}, {Admin: access}, operator} {
		_, err := s.PreviewImport(context.Background(), actor, "text", nil)
		require.Error(t, err)
		_, err = s.Import(context.Background(), actor, "text", nil)
		require.Error(t, err)
		if actor.OperatorID == 0 {
			view, err := s.Credentials(context.Background(), actor, account.ID, "password")
			require.Error(t, err)
			require.Nil(t, view)
		}
	}
	original := s.Cipher
	s.Cipher = func() (*managedinstance.CredentialCipher, error) {
		require.NoError(t, s.DB.Model(&model.User{}).Where("id = ?", admin.Admin.UserID).Update("authorization_version", admin.Admin.Version+1).Error)
		return original()
	}
	view, err := s.Credentials(context.Background(), admin, account.ID, "password")
	require.Nil(t, view)
	require.ErrorIs(t, err, authz.ErrAuthorizationChanged)
}

func TestMailboxCredentialsAuditAndCipherFailuresWithholdSecrets(t *testing.T) {
	for _, mode := range []string{"audit", "key", "null", "invalid-json", "invalid-config"} {
		t.Run(mode, func(t *testing.T) {
			s, actor := newMailboxImportTestService(t)
			account := mailboxImportTestAccount(t, s, actor, "one@example.com", "secret-password", mailboxImportTestSecret)
			switch mode {
			case "audit":
				require.NoError(t, s.DB.Callback().Create().Before("gorm:create").Register("mailbox_credential_test_failure", func(tx *gorm.DB) {
					if tx.Statement.Table == "mailbox_audits" {
						tx.AddError(errors.New("sensitive-diagnostic"))
					}
				}))
			case "key":
				s.Cipher = func() (*managedinstance.CredentialCipher, error) { return nil, errors.New("sensitive-key-diagnostic") }
			default:
				cipher, err := s.Cipher()
				require.NoError(t, err)
				payload := "null"
				if mode == "invalid-json" {
					payload = "{"
				}
				if mode == "invalid-config" {
					payload = `{"password":"secret-password","otp":{"secret":"MY","algorithm":99,"digits":6,"period":30}}`
				}
				ciphertext, _, _, err := cipher.Encrypt(account.ID, mailboxCredentialKind, managedinstance.CredentialPayload{Secret: payload})
				require.NoError(t, err)
				require.NoError(t, s.DB.Model(&model.MailboxAccount{}).Where("id = ?", account.ID).Update("ciphertext", ciphertext).Error)
			}
			view, err := s.Credentials(context.Background(), actor, account.ID, "otp")
			require.Nil(t, view)
			require.Error(t, err)
			require.NotContains(t, err.Error(), "sensitive")
		})
	}
}
