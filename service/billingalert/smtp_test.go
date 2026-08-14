package billingalert

import (
	"encoding/base64"
	"os"
	"strings"
	"testing"

	"github.com/01121531/subandnew-api/model"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestSMTPCipherUsesIndependentAssociatedData(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef")
	t.Setenv("MANAGED_INSTANCE_SECRET_KEY", base64.StdEncoding.EncodeToString(key))
	t.Setenv("MANAGED_INSTANCE_SECRET_KEY_VERSION", "test-v1")
	service, err := newSMTPCipherFromEnvironment()
	require.NoError(t, err)
	ciphertext, version, err := service.encrypt(1, "secret-password")
	require.NoError(t, err)
	require.NotContains(t, ciphertext, "secret-password")
	plaintext, err := service.decrypt(1, version, ciphertext)
	require.NoError(t, err)
	require.Equal(t, "secret-password", plaintext)
	_, err = service.decrypt(2, version, ciphertext)
	require.Error(t, err)
}

func TestSMTPSettingNeverReturnsPassword(t *testing.T) {
	previous := model.DB
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.SMTPSetting{}))
	model.DB = db
	t.Cleanup(func() { model.DB = previous })
	t.Setenv("MANAGED_INSTANCE_SECRET_KEY", base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef")))

	view, err := UpdateSMTPSetting(SMTPSettingInput{
		Host: "smtp.example.com", Port: 587, Security: SMTPSecurityStartTLS,
		Username: "mailer@example.com", Password: "do-not-return",
		FromAddress: "mailer@example.com", AlertRecipients: "ops@example.com,OPS@example.com;admin@example.com", Enabled: true,
	}, 1)
	require.NoError(t, err)
	require.True(t, view.PasswordStored)
	require.NotContains(t, strings.ToLower(strings.TrimSpace(os.Getenv("MANAGED_INSTANCE_SECRET_KEY"))), "do-not-return")

	var stored model.SMTPSetting
	require.NoError(t, model.DB.First(&stored, 1).Error)
	require.NotEmpty(t, stored.PasswordCipher)
	require.NotContains(t, stored.PasswordCipher, "do-not-return")
	loaded, err := GetSMTPSetting()
	require.NoError(t, err)
	require.True(t, loaded.PasswordStored)
	require.Equal(t, "ops@example.com,admin@example.com", loaded.AlertRecipients)
}

func TestParseRecipientListNormalizesAndRejectsInvalidValues(t *testing.T) {
	recipients, err := ParseRecipientList("ops@example.com; admin@example.com\nOPS@example.com")
	require.NoError(t, err)
	require.Equal(t, []string{"ops@example.com", "admin@example.com"}, recipients)
	_, err = ParseRecipientList("not-an-email")
	require.ErrorIs(t, err, ErrInvalidBillingInput)
}
