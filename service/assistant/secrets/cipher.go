package secrets

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
	"strings"
)

const (
	keysEnv              = "ASSISTANT_SECRET_KEYS"
	currentKeyVersionEnv = "ASSISTANT_SECRET_CURRENT_KEY_VERSION"
	singleKeyEnv         = "ASSISTANT_SECRET_KEY"
)

var ErrKeyNotConfigured = errors.New("assistant secret key is not configured")

// Cipher encrypts assistant credentials with a versioned keyring. New values
// use the current key while old versions remain decryptable during rotation.
type Cipher struct {
	keys           map[string]cipher.AEAD
	currentVersion string
}

func New(keyring map[string][]byte, currentVersion string) (*Cipher, error) {
	currentVersion = strings.TrimSpace(currentVersion)
	if currentVersion == "" {
		return nil, errors.New("assistant current secret key version is required")
	}
	if len(keyring) == 0 {
		return nil, ErrKeyNotConfigured
	}

	keys := make(map[string]cipher.AEAD, len(keyring))
	for version, key := range keyring {
		version = strings.TrimSpace(version)
		if version == "" {
			return nil, errors.New("assistant secret key version is required")
		}
		if len(key) != 32 {
			return nil, fmt.Errorf("assistant secret key %q must be 32 bytes", version)
		}
		block, err := aes.NewCipher(key)
		if err != nil {
			return nil, fmt.Errorf("create assistant secret key %q: %w", version, err)
		}
		aead, err := cipher.NewGCM(block)
		if err != nil {
			return nil, fmt.Errorf("create assistant secret cipher %q: %w", version, err)
		}
		keys[version] = aead
	}
	if _, ok := keys[currentVersion]; !ok {
		return nil, fmt.Errorf("assistant current secret key version %q is missing", currentVersion)
	}
	return &Cipher{keys: keys, currentVersion: currentVersion}, nil
}

// NewFromEnvironment reads ASSISTANT_SECRET_KEYS as a JSON object whose values
// are base64-encoded 32-byte keys. ASSISTANT_SECRET_KEY is supported for a
// single-key deployment and is assigned to the current version (default v1).
func NewFromEnvironment() (*Cipher, error) {
	currentVersion := strings.TrimSpace(os.Getenv(currentKeyVersionEnv))
	if currentVersion == "" {
		currentVersion = "v1"
	}

	encodedKeyring := strings.TrimSpace(os.Getenv(keysEnv))
	if encodedKeyring == "" {
		encodedKey := strings.TrimSpace(os.Getenv(singleKeyEnv))
		if encodedKey == "" {
			return nil, ErrKeyNotConfigured
		}
		key, err := base64.StdEncoding.DecodeString(encodedKey)
		if err != nil {
			return nil, fmt.Errorf("decode %s: %w", singleKeyEnv, err)
		}
		return New(map[string][]byte{currentVersion: key}, currentVersion)
	}

	var encodedKeys map[string]string
	if err := json.Unmarshal([]byte(encodedKeyring), &encodedKeys); err != nil {
		return nil, fmt.Errorf("decode %s: %w", keysEnv, err)
	}
	keyring := make(map[string][]byte, len(encodedKeys))
	for version, encodedKey := range encodedKeys {
		key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encodedKey))
		if err != nil {
			return nil, fmt.Errorf("decode assistant secret key %q: %w", version, err)
		}
		keyring[version] = key
	}
	return New(keyring, currentVersion)
}

func (c *Cipher) Encrypt(purpose string, recordID string, plaintext []byte) (ciphertext string, keyVersion string, fingerprint string, err error) {
	purpose = strings.TrimSpace(purpose)
	recordID = strings.TrimSpace(recordID)
	if c == nil || len(c.keys) == 0 {
		return "", "", "", errors.New("assistant secret cipher is nil")
	}
	if purpose == "" || recordID == "" || len(plaintext) == 0 {
		return "", "", "", errors.New("assistant secret purpose, record id, and plaintext are required")
	}
	aead := c.keys[c.currentVersion]
	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", "", "", err
	}
	sealed := aead.Seal(nil, nonce, plaintext, associatedData(purpose, recordID, c.currentVersion))
	digest := sha256.Sum256(plaintext)
	return base64.RawStdEncoding.EncodeToString(append(nonce, sealed...)), c.currentVersion, hex.EncodeToString(digest[:]), nil
}

func (c *Cipher) Decrypt(purpose string, recordID string, keyVersion string, ciphertext string) ([]byte, error) {
	if c == nil || len(c.keys) == 0 {
		return nil, errors.New("assistant secret cipher is nil")
	}
	aead, ok := c.keys[strings.TrimSpace(keyVersion)]
	if !ok {
		return nil, fmt.Errorf("unsupported assistant secret key version %q", keyVersion)
	}
	encoded, err := base64.RawStdEncoding.DecodeString(strings.TrimSpace(ciphertext))
	if err != nil {
		return nil, errors.New("invalid assistant secret ciphertext")
	}
	nonceSize := aead.NonceSize()
	if len(encoded) <= nonceSize {
		return nil, errors.New("invalid assistant secret ciphertext")
	}
	plaintext, err := aead.Open(nil, encoded[:nonceSize], encoded[nonceSize:], associatedData(strings.TrimSpace(purpose), strings.TrimSpace(recordID), strings.TrimSpace(keyVersion)))
	if err != nil {
		return nil, errors.New("decrypt assistant secret")
	}
	return plaintext, nil
}

func (c *Cipher) CurrentVersion() string {
	if c == nil {
		return ""
	}
	return c.currentVersion
}

func associatedData(purpose string, recordID string, keyVersion string) []byte {
	return []byte("assistant-secret:v1:" + purpose + ":" + recordID + ":" + keyVersion)
}
