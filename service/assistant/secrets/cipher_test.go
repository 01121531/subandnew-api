package secrets

import (
	"bytes"
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCipherEncryptDecryptAndRotate(t *testing.T) {
	oldKey := bytes.Repeat([]byte{1}, 32)
	newKey := bytes.Repeat([]byte{2}, 32)
	oldCipher, err := New(map[string][]byte{"v1": oldKey}, "v1")
	require.NoError(t, err)

	ciphertext, version, fingerprint, err := oldCipher.Encrypt("model-profile", "42", []byte("provider-secret"))
	require.NoError(t, err)
	require.Equal(t, "v1", version)
	require.Len(t, fingerprint, 64)
	require.NotContains(t, ciphertext, "provider-secret")

	rotatedCipher, err := New(map[string][]byte{"v1": oldKey, "v2": newKey}, "v2")
	require.NoError(t, err)
	plaintext, err := rotatedCipher.Decrypt("model-profile", "42", version, ciphertext)
	require.NoError(t, err)
	require.Equal(t, []byte("provider-secret"), plaintext)

	newCiphertext, newVersion, _, err := rotatedCipher.Encrypt("model-profile", "42", []byte("new-secret"))
	require.NoError(t, err)
	require.Equal(t, "v2", newVersion)
	plaintext, err = rotatedCipher.Decrypt("model-profile", "42", newVersion, newCiphertext)
	require.NoError(t, err)
	require.Equal(t, []byte("new-secret"), plaintext)
}

func TestCipherBindsPurposeAndRecord(t *testing.T) {
	cipher, err := New(map[string][]byte{"v1": bytes.Repeat([]byte{3}, 32)}, "v1")
	require.NoError(t, err)
	ciphertext, version, _, err := cipher.Encrypt("channel", "7", []byte("secret"))
	require.NoError(t, err)

	_, err = cipher.Decrypt("model-profile", "7", version, ciphertext)
	require.ErrorContains(t, err, "decrypt assistant secret")
	_, err = cipher.Decrypt("channel", "8", version, ciphertext)
	require.ErrorContains(t, err, "decrypt assistant secret")
}

func TestCipherFromEnvironment(t *testing.T) {
	keyV1 := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{4}, 32))
	keyV2 := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{5}, 32))
	t.Setenv(keysEnv, `{"v1":"`+keyV1+`","v2":"`+keyV2+`"}`)
	t.Setenv(currentKeyVersionEnv, "v2")
	t.Setenv(singleKeyEnv, "")

	cipher, err := NewFromEnvironment()
	require.NoError(t, err)
	require.Equal(t, "v2", cipher.CurrentVersion())
}

func TestCipherRejectsInvalidConfiguration(t *testing.T) {
	_, err := New(nil, "v1")
	require.ErrorIs(t, err, ErrKeyNotConfigured)
	_, err = New(map[string][]byte{"v1": bytes.Repeat([]byte{1}, 16)}, "v1")
	require.ErrorContains(t, err, "must be 32 bytes")
	_, err = New(map[string][]byte{"v1": bytes.Repeat([]byte{1}, 32)}, "v2")
	require.ErrorContains(t, err, "is missing")

	t.Setenv(keysEnv, "")
	t.Setenv(singleKeyEnv, "")
	_, err = NewFromEnvironment()
	require.ErrorIs(t, err, ErrKeyNotConfigured)
}
