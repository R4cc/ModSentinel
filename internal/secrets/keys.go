package secrets

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	kms "cloud.google.com/go/kms/apiv1"
	kmspb "cloud.google.com/go/kms/apiv1/kmspb"
)

// Manager handles encryption and decryption of secret data.
type Manager struct {
	keys    map[string][]byte
	primary string
}

type keyset struct {
	Primary string            `json:"primary"`
	Keys    map[string]string `json:"keys"`
}

// Load initializes a Manager by loading keys from KMS if configured or
// falling back to the SECRET_KEYSET environment variable.
func Load(ctx context.Context) (*Manager, error) {
	if m, err := loadFromKMS(ctx); err == nil {
		return m, nil
	}
	return loadFromEnv()
}

func loadFromEnv() (*Manager, error) {
	s := os.Getenv("SECRET_KEYSET")
	if s == "" {
		return nil, errors.New("SECRET_KEYSET not set")
	}
	return parseKeyset([]byte(s))
}

func loadFromKMS(ctx context.Context) (*Manager, error) {
	name := os.Getenv("KMS_KEY_NAME")
	ct := os.Getenv("SECRET_KEYSET_CIPHERTEXT")
	if name == "" || ct == "" {
		return nil, errors.New("kms not configured")
	}
	b, err := base64.StdEncoding.DecodeString(ct)
	if err != nil {
		return nil, err
	}
	client, err := kms.NewKeyManagementClient(ctx)
	if err != nil {
		return nil, err
	}
	defer client.Close()
	resp, err := client.Decrypt(ctx, &kmspb.DecryptRequest{
		Name:       name,
		Ciphertext: b,
	})
	if err != nil {
		return nil, err
	}
	return parseKeyset(resp.Plaintext)
}

func parseKeyset(data []byte) (*Manager, error) {
	var ks keyset
	if err := json.Unmarshal(data, &ks); err != nil {
		return nil, err
	}
	if ks.Primary == "" {
		return nil, errors.New("primary key missing")
	}
	m := &Manager{keys: make(map[string][]byte), primary: ks.Primary}
	for id, hexKey := range ks.Keys {
		b, err := hex.DecodeString(hexKey)
		if err != nil {
			return nil, fmt.Errorf("decode key %s: %w", id, err)
		}
		if len(b) != 32 {
			return nil, fmt.Errorf("key %s must be 32 bytes", id)
		}
		m.keys[id] = b
	}
	if _, ok := m.keys[ks.Primary]; !ok {
		return nil, errors.New("primary key not found in keyset")
	}
	return m, nil
}

// Encrypt encrypts plaintext using the primary key, returning ciphertext,
// IV, and key ID used.
func (m *Manager) Encrypt(plaintext []byte) (ciphertext, iv []byte, keyID string, err error) {
	keyID = m.primary
	key := m.keys[keyID]
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, nil, "", err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, nil, "", err
	}
	iv = make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, iv); err != nil {
		return nil, nil, "", err
	}
	ciphertext = aead.Seal(nil, iv, plaintext, nil)
	return ciphertext, iv, keyID, nil
}

// Decrypt decrypts ciphertext with the provided key ID and IV.
func (m *Manager) Decrypt(ciphertext, iv []byte, keyID string) ([]byte, error) {
	key, ok := m.keys[keyID]
	if !ok {
		return nil, fmt.Errorf("unknown key id %s", keyID)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return aead.Open(nil, iv, ciphertext, nil)
}
