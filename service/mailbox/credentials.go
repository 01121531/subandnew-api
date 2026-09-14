package mailbox

import (
	"context"
	"encoding/base32"
	"encoding/json"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/01121531/subandnew-api/model"
	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
)

const mailboxCredentialKind = "mailbox-account:v1"

type CredentialView struct {
	Password   string `json:"password,omitempty"`
	Code       string `json:"code,omitempty"`
	ExpiresAt  int64  `json:"expires_at,omitempty"`
	ServerTime int64  `json:"server_time"`
}

type mailboxOTPConfig struct {
	Secret    string        `json:"secret"`
	Algorithm otp.Algorithm `json:"algorithm"`
	Digits    otp.Digits    `json:"digits"`
	Period    uint          `json:"period"`
}

type mailboxCredentialSecret struct {
	Password string           `json:"password"`
	OTP      mailboxOTPConfig `json:"otp"`
}

func validMailboxText(value string) bool {
	return utf8.ValidString(value) && !strings.ContainsRune(value, '\x00')
}

func parseMailboxOTP(raw string) (mailboxOTPConfig, error) {
	config := mailboxOTPConfig{Algorithm: otp.AlgorithmSHA1, Digits: otp.DigitsSix, Period: 30}
	invalid := func() (mailboxOTPConfig, error) { return mailboxOTPConfig{}, fail(400, "mailbox_import_invalid_otp") }
	if !validMailboxText(raw) || len(raw) > 8192 {
		return invalid()
	}
	raw = strings.TrimSpace(raw)
	secret := raw
	if strings.Contains(raw, ":") {
		u, err := url.Parse(raw)
		if err != nil || u.Scheme != "otpauth" || u.Host != "totp" || u.User != nil || u.Fragment != "" || strings.Trim(u.Path, "/ ") == "" {
			return invalid()
		}
		params, err := url.ParseQuery(u.RawQuery)
		if err != nil {
			return invalid()
		}
		for key, values := range params {
			if len(values) != 1 || values[0] == "" {
				return invalid()
			}
			switch key {
			case "secret", "issuer":
			case "algorithm":
				switch strings.ToUpper(values[0]) {
				case "SHA1":
					config.Algorithm = otp.AlgorithmSHA1
				case "SHA256":
					config.Algorithm = otp.AlgorithmSHA256
				case "SHA512":
					config.Algorithm = otp.AlgorithmSHA512
				default:
					return invalid()
				}
			case "digits":
				if values[0] != "6" && values[0] != "8" {
					return invalid()
				}
				if values[0] == "8" {
					config.Digits = otp.DigitsEight
				}
			case "period":
				period, err := strconv.Atoi(values[0])
				if err != nil || period < 15 || period > 120 || strconv.Itoa(period) != values[0] {
					return invalid()
				}
				config.Period = uint(period)
			default:
				return invalid()
			}
		}
		secret = params.Get("secret")
	}
	secret = strings.ToUpper(strings.TrimSpace(secret))
	var decoded []byte
	var err error
	if strings.Contains(secret, "=") {
		decoded, err = base32.StdEncoding.DecodeString(secret)
	} else {
		decoded, err = base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(secret)
	}
	if err != nil || len(decoded) == 0 {
		return invalid()
	}
	config.Secret = base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(decoded)
	if strings.TrimRight(secret, "=") != config.Secret {
		return invalid()
	}
	return config, nil
}

func (config mailboxOTPConfig) code(now time.Time) (string, error) {
	if config.Period < 15 || config.Period > 120 || (config.Digits != otp.DigitsSix && config.Digits != otp.DigitsEight) ||
		(config.Algorithm != otp.AlgorithmSHA1 && config.Algorithm != otp.AlgorithmSHA256 && config.Algorithm != otp.AlgorithmSHA512) || config.Secret == "" {
		return "", fail(503, "mailbox_credentials_unavailable")
	}
	return totp.GenerateCodeCustom(config.Secret, now, totp.ValidateOpts{Period: config.Period, Digits: config.Digits, Algorithm: config.Algorithm})
}

func (s *Service) auditMailboxFailure(actor Actor, action string, accountID int64, err error) {
	if err != nil {
		status, code := HTTPError(err)
		_ = s.Audit(actor, action, accountID, 0, status, code)
	}
}

func (s *Service) credentialAccount(actor Actor, accountID int64) (*model.MailboxAccount, error) {
	account, err := s.AccountForActor(actor, accountID, true)
	if err != nil {
		return nil, err
	}
	if actor.Admin == nil {
		var count int64
		if err := s.DB.Model(&model.MailboxAssignment{}).Where("id = ? AND account_id = ? AND operator_id = ? AND revoked_at = 0 AND status IN ?",
			account.ActiveAssignmentID, account.ID, actor.OperatorID, []string{StatusPending, StatusSubmitted, StatusRejected}).Count(&count).Error; err != nil {
			return nil, err
		}
		if count != 1 {
			return nil, fail(403, "mailbox_credentials_revoked")
		}
	}
	return account, nil
}

func (s *Service) Credentials(ctx context.Context, actor Actor, accountID int64, kind string) (view *CredentialView, err error) {
	s = s.WithDB(s.DB.WithContext(ctx))
	// Never put client-supplied kind or decrypted material into an audit entry.
	action := "credentials"
	if kind == "password" || kind == "otp" {
		action += "_" + kind
	}
	defer func() { s.auditMailboxFailure(actor, action, accountID, err) }()
	account, err := s.credentialAccount(actor, accountID)
	if err != nil {
		return nil, err
	}
	if kind != "password" && kind != "otp" {
		return nil, fail(400, "mailbox_invalid_credential_kind")
	}
	cipher, err := s.Cipher()
	if err != nil {
		return nil, fail(503, "mailbox_credentials_unavailable")
	}
	payload, err := cipher.Decrypt(account.ID, mailboxCredentialKind, account.KeyVersion, account.Ciphertext)
	if err != nil {
		return nil, fail(503, "mailbox_credentials_unavailable")
	}
	var secret mailboxCredentialSecret
	if json.Unmarshal([]byte(payload.Secret), &secret) != nil || secret.Password == "" || !validMailboxText(secret.Password) {
		return nil, fail(503, "mailbox_credentials_unavailable")
	}
	now := s.Now()
	view = &CredentialView{ServerTime: now.Unix()}
	if kind == "password" {
		view.Password = secret.Password
	} else {
		view.Code, err = secret.OTP.code(now)
		if err != nil {
			return nil, fail(503, "mailbox_credentials_unavailable")
		}
		period := int64(secret.OTP.Period)
		view.ExpiresAt = (now.Unix()/period + 1) * period
	}
	current, err := s.credentialAccount(actor, accountID)
	if err != nil {
		return nil, err
	}
	if current.Version != account.Version || current.ActiveAssignmentID != account.ActiveAssignmentID || current.Ciphertext != account.Ciphertext || current.KeyVersion != account.KeyVersion {
		return nil, fail(409, "mailbox_credentials_changed")
	}
	if err := s.Audit(actor, action, account.ID, account.ActiveAssignmentID, 200, ""); err != nil {
		return nil, fail(500, "mailbox_service_unavailable")
	}
	return view, nil
}
