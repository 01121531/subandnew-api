package billingalert

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/mail"
	"net/smtp"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/01121531/subandnew-api/model"
	"gorm.io/gorm"
)

const (
	SMTPSecurityNone     = "none"
	SMTPSecurityStartTLS = "starttls"
	SMTPSecurityTLS      = "tls"
)

var (
	ErrSMTPNotConfigured  = errors.New("smtp is not configured")
	ErrSMTPKeyUnavailable = errors.New("smtp encryption key is unavailable")
)

type SMTPSettingInput struct {
	Host            string `json:"host"`
	Port            int    `json:"port"`
	Security        string `json:"security"`
	Username        string `json:"username"`
	Password        string `json:"password"`
	FromName        string `json:"from_name"`
	FromAddress     string `json:"from_address"`
	ReplyTo         string `json:"reply_to"`
	AlertRecipients string `json:"alert_recipients"`
	Enabled         bool   `json:"enabled"`
}

type SMTPSettingView struct {
	ID              int64  `json:"id"`
	Host            string `json:"host"`
	Port            int    `json:"port"`
	Security        string `json:"security"`
	Username        string `json:"username"`
	PasswordStored  bool   `json:"password_stored"`
	FromName        string `json:"from_name"`
	FromAddress     string `json:"from_address"`
	ReplyTo         string `json:"reply_to"`
	AlertRecipients string `json:"alert_recipients"`
	Enabled         bool   `json:"enabled"`
	UpdatedBy       int    `json:"updated_by"`
	CreatedAt       int64  `json:"created_at"`
	UpdatedAt       int64  `json:"updated_at"`
}

type SMTPTestInput struct {
	Recipient string `json:"recipient"`
}

type SMTPMessage struct {
	Recipients []string
	Subject    string
	TextBody   string
	HTMLBody   string
}

type smtpCipher struct {
	aead       cipher.AEAD
	keyVersion string
}

func GetSMTPSetting() (*SMTPSettingView, error) {
	var setting model.SMTPSetting
	err := model.DB.First(&setting, 1).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return &SMTPSettingView{ID: 1, Port: 587, Security: SMTPSecurityStartTLS}, nil
	}
	if err != nil {
		return nil, err
	}
	return smtpSettingView(&setting), nil
}

func UpdateSMTPSetting(input SMTPSettingInput, actorID int) (*SMTPSettingView, error) {
	input.Host = strings.TrimSpace(input.Host)
	input.Username = strings.TrimSpace(input.Username)
	input.FromName = strings.TrimSpace(input.FromName)
	input.FromAddress = strings.TrimSpace(input.FromAddress)
	input.ReplyTo = strings.TrimSpace(input.ReplyTo)
	alertRecipients, recipientsErr := ParseRecipientList(input.AlertRecipients)
	if actorID <= 0 || input.Host == "" || input.Port <= 0 || input.Port > 65535 ||
		!validSMTPSecurity(input.Security) || !validEmail(input.FromAddress) || input.ReplyTo != "" && !validEmail(input.ReplyTo) || recipientsErr != nil {
		return nil, ErrInvalidBillingInput
	}
	var current model.SMTPSetting
	err := model.DB.First(&current, 1).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	passwordCipher := current.PasswordCipher
	keyVersion := current.KeyVersion
	if input.Password != "" {
		cipherService, err := newSMTPCipherFromEnvironment()
		if err != nil {
			return nil, err
		}
		passwordCipher, keyVersion, err = cipherService.encrypt(1, input.Password)
		if err != nil {
			return nil, err
		}
	}
	if input.Username != "" && passwordCipher == "" {
		return nil, ErrInvalidBillingInput
	}
	setting := &model.SMTPSetting{
		ID: 1, Host: input.Host, Port: input.Port, Security: input.Security,
		Username: input.Username, PasswordCipher: passwordCipher, KeyVersion: keyVersion,
		FromName: input.FromName, FromAddress: input.FromAddress, ReplyTo: input.ReplyTo,
		AlertRecipients: strings.Join(alertRecipients, ","),
		Enabled:         input.Enabled, UpdatedBy: actorID,
	}
	setting.CreatedAt = current.CreatedAt
	setting.UpdatedAt = time.Now().Unix()
	if err := model.DB.Save(setting).Error; err != nil {
		return nil, err
	}
	return smtpSettingView(setting), nil
}

func SendSMTPTest(ctx context.Context, recipient string) error {
	recipient = strings.TrimSpace(recipient)
	if !validEmail(recipient) {
		return ErrInvalidBillingInput
	}
	message := SMTPMessage{
		Recipients: []string{recipient}, Subject: "SubAndNew API 账单预警测试邮件",
		TextBody: "SMTP 配置测试成功。此邮件由 SubAndNew API 控制台发送。",
		HTMLBody: "<p><strong>SMTP 配置测试成功</strong></p><p>此邮件由 SubAndNew API 控制台发送。</p>",
	}
	setting, password, err := loadSMTPSetting(false)
	if err != nil {
		return err
	}
	return deliverSMTP(ctx, setting, password, message.Recipients, buildSMTPMessage(setting, message))
}

func SendSMTPMessage(ctx context.Context, message SMTPMessage) error {
	setting, password, err := loadSMTPSetting(true)
	if err != nil {
		return err
	}
	if len(message.Recipients) == 0 || strings.TrimSpace(message.Subject) == "" {
		return ErrInvalidBillingInput
	}
	for _, recipient := range message.Recipients {
		if !validEmail(recipient) {
			return ErrInvalidBillingInput
		}
	}
	raw := buildSMTPMessage(setting, message)
	return deliverSMTP(ctx, setting, password, message.Recipients, raw)
}

func ManagedInstanceAlertRecipients() ([]string, error) {
	var setting model.SMTPSetting
	if err := model.DB.First(&setting, 1).Error; err != nil || !setting.Enabled {
		return nil, ErrSMTPNotConfigured
	}
	recipients, err := ParseRecipientList(setting.AlertRecipients)
	if err != nil || len(recipients) == 0 {
		return nil, ErrSMTPNotConfigured
	}
	return recipients, nil
}

func ParseRecipientList(value string) ([]string, error) {
	parts := strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || r == ';' || r == '\n' || r == '\r'
	})
	seen := make(map[string]struct{}, len(parts))
	recipients := make([]string, 0, len(parts))
	for _, part := range parts {
		recipient := strings.TrimSpace(part)
		if recipient == "" {
			continue
		}
		if !validEmail(recipient) {
			return nil, ErrInvalidBillingInput
		}
		key := strings.ToLower(recipient)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		recipients = append(recipients, recipient)
	}
	return recipients, nil
}

func loadSMTPSetting(requireEnabled bool) (*model.SMTPSetting, string, error) {
	var setting model.SMTPSetting
	if err := model.DB.First(&setting, 1).Error; err != nil || requireEnabled && !setting.Enabled {
		return nil, "", ErrSMTPNotConfigured
	}
	password := ""
	if setting.PasswordCipher != "" {
		cipherService, err := newSMTPCipherFromEnvironment()
		if err != nil {
			return nil, "", err
		}
		password, err = cipherService.decrypt(setting.ID, setting.KeyVersion, setting.PasswordCipher)
		if err != nil {
			return nil, "", err
		}
	}
	return &setting, password, nil
}

func deliverSMTP(ctx context.Context, setting *model.SMTPSetting, password string, recipients []string, message []byte) error {
	address := net.JoinHostPort(setting.Host, strconv.Itoa(setting.Port))
	dialer := &net.Dialer{Timeout: 15 * time.Second}
	var connection net.Conn
	var err error
	if setting.Security == SMTPSecurityTLS {
		connection, err = tls.DialWithDialer(dialer, "tcp", address, &tls.Config{ServerName: setting.Host, MinVersion: tls.VersionTLS12})
	} else {
		connection, err = dialer.DialContext(ctx, "tcp", address)
	}
	if err != nil {
		return fmt.Errorf("smtp_connect_failed: %w", err)
	}
	defer connection.Close()
	if deadline, ok := ctx.Deadline(); ok {
		_ = connection.SetDeadline(deadline)
	} else {
		_ = connection.SetDeadline(time.Now().Add(30 * time.Second))
	}
	client, err := smtp.NewClient(connection, setting.Host)
	if err != nil {
		return fmt.Errorf("smtp_handshake_failed: %w", err)
	}
	defer client.Close()
	if setting.Security == SMTPSecurityStartTLS {
		if ok, _ := client.Extension("STARTTLS"); !ok {
			return errors.New("smtp_starttls_unavailable")
		}
		if err := client.StartTLS(&tls.Config{ServerName: setting.Host, MinVersion: tls.VersionTLS12}); err != nil {
			return fmt.Errorf("smtp_tls_failed: %w", err)
		}
	}
	if setting.Username != "" {
		if err := client.Auth(smtp.PlainAuth("", setting.Username, password, setting.Host)); err != nil {
			return fmt.Errorf("smtp_auth_failed: %w", err)
		}
	}
	if err := client.Mail(setting.FromAddress); err != nil {
		return fmt.Errorf("smtp_sender_rejected: %w", err)
	}
	for _, recipient := range recipients {
		if err := client.Rcpt(recipient); err != nil {
			return fmt.Errorf("smtp_recipient_rejected: %w", err)
		}
	}
	writer, err := client.Data()
	if err != nil {
		return fmt.Errorf("smtp_data_failed: %w", err)
	}
	if _, err := writer.Write(message); err != nil {
		_ = writer.Close()
		return fmt.Errorf("smtp_write_failed: %w", err)
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("smtp_delivery_failed: %w", err)
	}
	return client.Quit()
}

func buildSMTPMessage(setting *model.SMTPSetting, message SMTPMessage) []byte {
	var builder strings.Builder
	from := mail.Address{Name: setting.FromName, Address: setting.FromAddress}
	builder.WriteString("From: " + from.String() + "\r\n")
	builder.WriteString("To: " + strings.Join(message.Recipients, ", ") + "\r\n")
	if setting.ReplyTo != "" {
		builder.WriteString("Reply-To: " + setting.ReplyTo + "\r\n")
	}
	builder.WriteString("Subject: " + mime.QEncoding.Encode("UTF-8", message.Subject) + "\r\n")
	builder.WriteString("MIME-Version: 1.0\r\n")
	if message.HTMLBody != "" {
		boundary := "billing-alert-boundary"
		builder.WriteString("Content-Type: multipart/alternative; boundary=" + boundary + "\r\n\r\n")
		builder.WriteString("--" + boundary + "\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n" + message.TextBody + "\r\n")
		builder.WriteString("--" + boundary + "\r\nContent-Type: text/html; charset=UTF-8\r\n\r\n" + message.HTMLBody + "\r\n")
		builder.WriteString("--" + boundary + "--\r\n")
	} else {
		builder.WriteString("Content-Type: text/plain; charset=UTF-8\r\n\r\n" + message.TextBody + "\r\n")
	}
	return []byte(builder.String())
}

func newSMTPCipherFromEnvironment() (*smtpCipher, error) {
	encoded := strings.TrimSpace(os.Getenv("MANAGED_INSTANCE_SECRET_KEY"))
	if encoded == "" {
		return nil, ErrSMTPKeyUnavailable
	}
	key, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(key) != 32 {
		return nil, ErrSMTPKeyUnavailable
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	version := strings.TrimSpace(os.Getenv("MANAGED_INSTANCE_SECRET_KEY_VERSION"))
	if version == "" {
		version = "v1"
	}
	return &smtpCipher{aead: aead, keyVersion: version}, nil
}

func (service *smtpCipher) encrypt(id int64, plaintext string) (string, string, error) {
	nonce := make([]byte, service.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", "", err
	}
	sealed := service.aead.Seal(nil, nonce, []byte(plaintext), smtpAssociatedData(id, service.keyVersion))
	return base64.RawStdEncoding.EncodeToString(append(nonce, sealed...)), service.keyVersion, nil
}

func (service *smtpCipher) decrypt(id int64, version string, ciphertext string) (string, error) {
	if version != service.keyVersion {
		return "", ErrSMTPKeyUnavailable
	}
	encoded, err := base64.RawStdEncoding.DecodeString(ciphertext)
	if err != nil || len(encoded) <= service.aead.NonceSize() {
		return "", ErrSMTPKeyUnavailable
	}
	plaintext, err := service.aead.Open(nil, encoded[:service.aead.NonceSize()], encoded[service.aead.NonceSize():], smtpAssociatedData(id, version))
	if err != nil {
		return "", ErrSMTPKeyUnavailable
	}
	return string(plaintext), nil
}

func smtpAssociatedData(id int64, version string) []byte {
	return []byte("billing-smtp:v1:" + strconv.FormatInt(id, 10) + ":" + version)
}

func smtpSettingView(setting *model.SMTPSetting) *SMTPSettingView {
	return &SMTPSettingView{
		ID: setting.ID, Host: setting.Host, Port: setting.Port, Security: setting.Security,
		Username: setting.Username, PasswordStored: setting.PasswordCipher != "", FromName: setting.FromName,
		FromAddress: setting.FromAddress, ReplyTo: setting.ReplyTo, AlertRecipients: setting.AlertRecipients, Enabled: setting.Enabled,
		UpdatedBy: setting.UpdatedBy, CreatedAt: setting.CreatedAt, UpdatedAt: setting.UpdatedAt,
	}
}

func validSMTPSecurity(value string) bool {
	return value == SMTPSecurityNone || value == SMTPSecurityStartTLS || value == SMTPSecurityTLS
}

func validEmail(value string) bool {
	address, err := mail.ParseAddress(strings.TrimSpace(value))
	return err == nil && address.Address != ""
}
