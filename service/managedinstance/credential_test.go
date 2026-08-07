package managedinstance

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCredentialCipherRoundTripAndInstanceBinding(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	cipher, err := NewCredentialCipher(key, "v7")
	require.NoError(t, err)

	ciphertext, version, fingerprint, err := cipher.Encrypt(42, "bearer_pat", CredentialPayload{Secret: "admin-secret", UserID: "1"})
	require.NoError(t, err)
	require.Equal(t, "v7", version)
	require.Len(t, fingerprint, 64)
	require.NotContains(t, ciphertext, "admin-secret")

	payload, err := cipher.Decrypt(42, "bearer_pat", version, ciphertext)
	require.NoError(t, err)
	require.Equal(t, CredentialPayload{Secret: "admin-secret", UserID: "1"}, payload)

	_, err = cipher.Decrypt(43, "bearer_pat", version, ciphertext)
	require.Error(t, err)
	_, err = cipher.Decrypt(42, "admin_token", version, ciphertext)
	require.Error(t, err)
	_, err = cipher.Decrypt(42, "bearer_pat", "v8", ciphertext)
	require.Error(t, err)
}

func TestCredentialCipherRejectsTampering(t *testing.T) {
	cipher, err := NewCredentialCipher(make([]byte, 32), "v1")
	require.NoError(t, err)
	ciphertext, version, _, err := cipher.Encrypt(1, "admin_token", CredentialPayload{Secret: "secret"})
	require.NoError(t, err)

	encoded, err := base64.RawStdEncoding.DecodeString(ciphertext)
	require.NoError(t, err)
	encoded[len(encoded)-1] ^= 0xff
	_, err = cipher.Decrypt(1, "admin_token", version, base64.RawStdEncoding.EncodeToString(encoded))
	require.Error(t, err)
}

func TestCredentialCipherEnvironmentValidation(t *testing.T) {
	t.Setenv(managedInstanceSecretKeyEnv, "")
	_, err := NewCredentialCipherFromEnvironment()
	require.ErrorIs(t, err, ErrCredentialKeyNotConfigured)

	t.Setenv(managedInstanceSecretKeyEnv, strings.Repeat("x", 32))
	_, err = NewCredentialCipherFromEnvironment()
	require.Error(t, err)
}
