package profile

import (
	"bytes"
	"testing"

	"github.com/01121531/subandnew-api/model"
	"github.com/01121531/subandnew-api/service/assistant/secrets"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func newProfileService(t *testing.T) *Service {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.AssistantModelProfile{}))
	cipher, err := secrets.New(map[string][]byte{"v1": bytes.Repeat([]byte{7}, 32)}, "v1")
	require.NoError(t, err)
	service, err := NewService(db, cipher)
	require.NoError(t, err)
	return service
}

func TestProfileLifecycleEncryptsSecret(t *testing.T) {
	service := newProfileService(t)
	created, err := service.Create(CreateInput{
		Name: "primary", BaseURL: "https://api.example.com/v1", Model: "model-a", APIKey: "top-secret", Enabled: true, IsPrimary: true,
	})
	require.NoError(t, err)
	require.NotZero(t, created.Id)
	require.Equal(t, 120, created.TimeoutSeconds)
	require.NotContains(t, created.APIKeyCiphertext, "top-secret")
	require.Len(t, created.APIKeyFingerprint, 64)

	profiles, err := service.List()
	require.NoError(t, err)
	require.Len(t, profiles, 1)
	require.Equal(t, created.Id, profiles[0].Id)

	name := "renamed"
	newSecret := "rotated-secret"
	updated, err := service.Update(created.Id, UpdateInput{Name: &name, APIKey: &newSecret})
	require.NoError(t, err)
	require.Equal(t, name, updated.Name)
	require.NotEqual(t, created.APIKeyFingerprint, updated.APIKeyFingerprint)

	client, primary, err := service.PrimaryClient()
	require.NoError(t, err)
	require.NotNil(t, client)
	require.Equal(t, created.Id, primary.Id)

	require.NoError(t, service.Delete(created.Id))
	_, err = service.Get(created.Id)
	require.ErrorIs(t, err, ErrNotFound)
}

func TestProfileOnlyOnePrimary(t *testing.T) {
	service := newProfileService(t)
	first, err := service.Create(CreateInput{Name: "first", BaseURL: "https://one.example/v1", Model: "one", Enabled: true, IsPrimary: true})
	require.NoError(t, err)
	second, err := service.Create(CreateInput{Name: "second", BaseURL: "https://two.example/v1", Model: "two", Enabled: true, IsPrimary: true})
	require.NoError(t, err)

	first, err = service.Get(first.Id)
	require.NoError(t, err)
	require.False(t, first.IsPrimary)
	require.True(t, second.IsPrimary)
}

func TestProfileRejectsInvalidInputAndMissingCipher(t *testing.T) {
	service := newProfileService(t)
	_, err := service.Create(CreateInput{Name: "bad", BaseURL: "file:///tmp/model", Model: "model"})
	require.ErrorIs(t, err, ErrInvalidInput)

	db := service.db
	withoutCipher, err := NewService(db, nil)
	require.NoError(t, err)
	_, err = withoutCipher.Create(CreateInput{Name: "secret", BaseURL: "https://example.com/v1", Model: "model", APIKey: "secret"})
	require.ErrorIs(t, err, ErrSecretMissing)
	disabled, err := service.Create(CreateInput{Name: "disabled", BaseURL: "https://example.com/v1", Model: "model", Enabled: false})
	require.NoError(t, err)
	_, _, err = service.Client(disabled.Id)
	require.ErrorIs(t, err, ErrInvalidInput)
}
