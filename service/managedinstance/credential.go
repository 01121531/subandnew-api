package managedinstance

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

const (
	managedInstanceSecretKeyEnv        = "MANAGED_INSTANCE_SECRET_KEY"
	managedInstanceSecretKeyVersionEnv = "MANAGED_INSTANCE_SECRET_KEY_VERSION"
)

var ErrCredentialKeyNotConfigured = errors.New("managed instance credential key is not configured")

type CredentialPayload struct {
	Secret string `json:"secret"`
	UserID string `json:"user_id,omitempty"`
}

type CredentialCipher struct {
	aead       cipher.AEAD
	keyVersion string
}

func NewCredentialCipher(key []byte, keyVersion string) (*CredentialCipher, error) {
	if len(key) != 32 {
		return nil, errors.New("managed instance credential key must be 32 bytes")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	keyVersion = strings.TrimSpace(keyVersion)
	if keyVersion == "" {
		keyVersion = "v1"
	}
	return &CredentialCipher{aead: aead, keyVersion: keyVersion}, nil
}

func NewCredentialCipherFromEnvironment() (*CredentialCipher, error) {
	encodedKey := strings.TrimSpace(os.Getenv(managedInstanceSecretKeyEnv))
	if encodedKey == "" {
		return nil, ErrCredentialKeyNotConfigured
	}
	key, err := base64.StdEncoding.DecodeString(encodedKey)
	if err != nil {
		return nil, fmt.Errorf("decode %s: %w", managedInstanceSecretKeyEnv, err)
	}
	return NewCredentialCipher(key, os.Getenv(managedInstanceSecretKeyVersionEnv))
}

func (c *CredentialCipher) Encrypt(instanceID int64, authType string, payload CredentialPayload) (string, string, string, error) {
	if c == nil || c.aead == nil {
		return "", "", "", errors.New("credential cipher is nil")
	}
	payload.Secret = strings.TrimSpace(payload.Secret)
	authType = strings.TrimSpace(authType)
	if instanceID <= 0 || authType == "" || payload.Secret == "" {
		return "", "", "", errors.New("instance id, auth type, and credential secret are required")
	}
	plaintext, err := json.Marshal(payload)
	if err != nil {
		return "", "", "", err
	}
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", "", "", err
	}
	sealed := c.aead.Seal(nil, nonce, plaintext, credentialAssociatedData(instanceID, authType, c.keyVersion))
	encoded := base64.RawStdEncoding.EncodeToString(append(nonce, sealed...))
	fingerprint := sha256.Sum256([]byte(payload.Secret))
	return encoded, c.keyVersion, hex.EncodeToString(fingerprint[:]), nil
}

func (c *CredentialCipher) Decrypt(instanceID int64, authType string, keyVersion string, ciphertext string) (CredentialPayload, error) {
	if c == nil || c.aead == nil {
		return CredentialPayload{}, errors.New("credential cipher is nil")
	}
	if keyVersion != c.keyVersion {
		return CredentialPayload{}, fmt.Errorf("unsupported credential key version %q", keyVersion)
	}
	encoded, err := base64.RawStdEncoding.DecodeString(ciphertext)
	if err != nil {
		return CredentialPayload{}, errors.New("invalid credential ciphertext")
	}
	nonceSize := c.aead.NonceSize()
	if len(encoded) <= nonceSize {
		return CredentialPayload{}, errors.New("invalid credential ciphertext")
	}
	plaintext, err := c.aead.Open(nil, encoded[:nonceSize], encoded[nonceSize:], credentialAssociatedData(instanceID, authType, keyVersion))
	if err != nil {
		return CredentialPayload{}, errors.New("decrypt managed instance credential")
	}
	var payload CredentialPayload
	if err := json.Unmarshal(plaintext, &payload); err != nil {
		return CredentialPayload{}, errors.New("decode managed instance credential")
	}
	return payload, nil
}

func credentialAssociatedData(instanceID int64, authType string, keyVersion string) []byte {
	return []byte("managed-instance-credential:v1:" + strconv.FormatInt(instanceID, 10) + ":" + authType + ":" + keyVersion)
}
