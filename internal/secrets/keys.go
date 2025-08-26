package secrets

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/rs/zerolog/log"
	settings "modsentinel/internal/settings"

	"golang.org/x/crypto/argon2"
	"strings"
)

// Manager provides envelope encryption using a single master key.
type Manager struct {
	aead cipher.AEAD
}

// New creates a Manager from a raw 32-byte key.
func New(key []byte) (*Manager, error) {
	if len(key) < 32 {
		return nil, fmt.Errorf("key must be at least 32 bytes, got %d", len(key))
	}
	block, err := aes.NewCipher(key[:32])
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Manager{aead: aead}, nil
}

// Encrypt seals plaintext using AES-256-GCM and returns nonce and ciphertext.
func (m *Manager) Encrypt(plaintext []byte) (nonce, ciphertext []byte, err error) {
	nonce = make([]byte, m.aead.NonceSize())
	if _, err = io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, nil, err
	}
	ciphertext = m.aead.Seal(nil, nonce, plaintext, nil)
	return nonce, ciphertext, nil
}

// Decrypt opens ciphertext with the given nonce.
func (m *Manager) Decrypt(nonce, ciphertext []byte) ([]byte, error) {
	return m.aead.Open(nil, nonce, ciphertext, nil)
}

const (
	nodeKeyEnv        = "MODSENTINEL_NODE_KEY"
	wrappedKeySetting = "crypto.wrapped_mk"
	kdfParamsSetting  = "crypto.kdf_params"

	argonTime    uint32 = 1
	argonMemory  uint32 = 64 * 1024
	argonThreads uint8  = 4
	saltSize            = 16
)

type kdfParams struct {
	Salt string `json:"salt"`
}

type wrappedKey struct {
	Nonce      string `json:"nonce"`
	Ciphertext string `json:"ciphertext"`
}

// Load derives an encryption key from MODSENTINEL_NODE_KEY and returns a Manager.
// On first boot, a new 32-byte master key is generated, wrapped with the derived
// key-encryption key (KEK), and persisted to app_settings.
func Load(ctx context.Context, db *sql.DB) (*Manager, error) {
	nodeKey := os.Getenv(nodeKeyEnv)
	if len(nodeKey) < 16 {
		return nil, errors.New("MODSENTINEL_NODE_KEY must be at least 16 characters")
	}
	if len(nodeKey) < 32 {
		log.Warn().Int("length", len(nodeKey)).Msg("MODSENTINEL_NODE_KEY appears weak")
	}
	store := settings.New(db)

	paramsStr, err := store.Get(ctx, kdfParamsSetting)
	if err != nil {
		return nil, err
	}
	wrappedStr, err := store.Get(ctx, wrappedKeySetting)
	if err != nil {
		return nil, err
	}

	var mk []byte

	if paramsStr == "" || wrappedStr == "" {
		// First boot: generate salt and master key.
		salt := make([]byte, saltSize)
		if _, err := rand.Read(salt); err != nil {
			return nil, fmt.Errorf("generate salt: %w", err)
		}
		kek := argon2.IDKey([]byte(nodeKey), salt, argonTime, argonMemory, argonThreads, 32)
		wrapper, err := New(kek)
		if err != nil {
			return nil, err
		}
		mk = make([]byte, 32)
		if _, err := rand.Read(mk); err != nil {
			return nil, fmt.Errorf("generate master key: %w", err)
		}
		nonce, ct, err := wrapper.Encrypt(mk)
		if err != nil {
			return nil, err
		}
		wk := wrappedKey{
			Nonce:      base64.StdEncoding.EncodeToString(nonce),
			Ciphertext: base64.StdEncoding.EncodeToString(ct),
		}
		wkJSON, _ := json.Marshal(wk)
		paramsJSON, _ := json.Marshal(kdfParams{Salt: base64.StdEncoding.EncodeToString(salt)})
		if err := store.Set(ctx, wrappedKeySetting, string(wkJSON)); err != nil {
			return nil, err
		}
		if err := store.Set(ctx, kdfParamsSetting, string(paramsJSON)); err != nil {
			return nil, err
		}
	} else {
		// Existing installation: derive KEK using stored salt and unwrap MK.
		var params kdfParams
		if err := json.Unmarshal([]byte(paramsStr), &params); err != nil {
			return nil, fmt.Errorf("parse kdf params: %w", err)
		}
		salt, err := base64.StdEncoding.DecodeString(params.Salt)
		if err != nil {
			return nil, fmt.Errorf("decode salt: %w", err)
		}
		kek := argon2.IDKey([]byte(nodeKey), salt, argonTime, argonMemory, argonThreads, 32)
		wrapper, err := New(kek)
		if err != nil {
			return nil, err
		}
		var wk wrappedKey
		if err := json.Unmarshal([]byte(wrappedStr), &wk); err != nil {
			return nil, fmt.Errorf("parse wrapped key: %w", err)
		}
		nonce, err := base64.StdEncoding.DecodeString(wk.Nonce)
		if err != nil {
			return nil, fmt.Errorf("decode nonce: %w", err)
		}
		ct, err := base64.StdEncoding.DecodeString(wk.Ciphertext)
		if err != nil {
			return nil, fmt.Errorf("decode ciphertext: %w", err)
		}
		mk, err = wrapper.Decrypt(nonce, ct)
		if err != nil {
			if strings.Contains(err.Error(), "authentication failed") {
				return nil, fmt.Errorf("unwrap master key: authentication failed")
			}
			return nil, fmt.Errorf("unwrap master key: %w", err)
		}
	}

	m, err := New(mk)
	if err != nil {
		return nil, err
	}
	nonce, ct, err := m.Encrypt([]byte("sentinel"))
	if err != nil {
		return nil, fmt.Errorf("sentinel encrypt: %w", err)
	}
	pt, err := m.Decrypt(nonce, ct)
	if err != nil {
		if strings.Contains(err.Error(), "authentication failed") {
			return nil, fmt.Errorf("sentinel decrypt: authentication failed")
		}
		return nil, fmt.Errorf("sentinel decrypt: %w", err)
	}
	if !bytes.Equal(pt, []byte("sentinel")) {
		return nil, errors.New("sentinel mismatch")
	}
	return m, nil
}
