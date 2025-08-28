package secrets

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Service provides plaintext secret storage backed by a database.
type Service struct {
	db      *sql.DB
	ttl     time.Duration
	mu      sync.Mutex
	cache   map[string]cacheEntry
	key     []byte
	keyPath string
}

// NewService creates a Service using the provided database. An optional key
// file path may be provided; otherwise a default within the OS temp directory
// is used. The key is created on first use and persisted for future runs.
func NewService(db *sql.DB, keyPath ...string) *Service {
	s := &Service{db: db, ttl: 10 * time.Minute, cache: make(map[string]cacheEntry)}
	if len(keyPath) > 0 && keyPath[0] != "" {
		s.keyPath = keyPath[0]
	} else {
		s.keyPath = filepath.Join(os.TempDir(), "modsentinel.key")
	}
	if k, err := loadOrCreateKey(s.keyPath); err == nil {
		s.key = k
	}
	return s
}

type cacheEntry struct {
	val []byte
	exp time.Time
}

func loadOrCreateKey(path string) ([]byte, error) {
	if b, err := os.ReadFile(path); err == nil && len(b) == 32 {
		return b, nil
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, key, 0o600); err != nil {
		return nil, err
	}
	return key, nil
}

func (s *Service) encrypt(b []byte) ([]byte, error) {
	if s.key == nil {
		return b, nil
	}
	block, err := aes.NewCipher(s.key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	ct := gcm.Seal(nil, nonce, b, nil)
	out := make([]byte, 3+len(nonce)+len(ct))
	copy(out, []byte("v1:"))
	copy(out[3:], nonce)
	copy(out[3+len(nonce):], ct)
	return out, nil
}

func (s *Service) decrypt(b []byte) ([]byte, error) {
	if s.key == nil {
		return b, nil
	}
	if len(b) > 3 && string(b[:3]) == "v1:" {
		b = b[3:]
		block, err := aes.NewCipher(s.key)
		if err != nil {
			return nil, err
		}
		gcm, err := cipher.NewGCM(block)
		if err != nil {
			return nil, err
		}
		ns := gcm.NonceSize()
		if len(b) < ns {
			return nil, io.ErrUnexpectedEOF
		}
		nonce, ct := b[:ns], b[ns:]
		pt, err := gcm.Open(nil, nonce, ct, nil)
		if err != nil {
			return nil, err
		}
		return pt, nil
	}
	// plaintext fallback for legacy values
	return b, nil
}

func isEncrypted(b []byte) bool {
	return len(b) > 3 && string(b[:3]) == "v1:"
}

// Set stores a secret for the given name, encrypting it at rest.
func (s *Service) Set(ctx context.Context, name string, plaintext []byte) error {
	if name == "" {
		return sql.ErrNoRows
	}
	val, err := s.encrypt(plaintext)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO secrets(name, value) VALUES(?,?)
       ON CONFLICT(name) DO UPDATE SET value=excluded.value, updated_at=CURRENT_TIMESTAMP`, name, val)
	s.mu.Lock()
	if _, ok := s.cache[name]; ok {
		delete(s.cache, name)
	}
	s.mu.Unlock()
	return err
}

// Exists returns whether a secret with the given name is stored.
func (s *Service) Exists(ctx context.Context, name string) (bool, error) {
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(1) FROM secrets WHERE name=?`, name).Scan(&n); err != nil {
		return false, err
	}
	return n > 0, nil
}

// Delete removes a stored secret of the given name.
func (s *Service) Delete(ctx context.Context, name string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM secrets WHERE name=?`, name)
	s.mu.Lock()
	if _, ok := s.cache[name]; ok {
		delete(s.cache, name)
	}
	s.mu.Unlock()
	return err
}

// Get retrieves the secret of the given name.
func (s *Service) Get(ctx context.Context, name string) ([]byte, error) {
	now := time.Now()
	s.mu.Lock()
	if e, ok := s.cache[name]; ok {
		if now.Before(e.exp) {
			v := append([]byte(nil), e.val...)
			s.mu.Unlock()
			return v, nil
		}
		delete(s.cache, name)
	}
	s.mu.Unlock()

	var ct []byte
	err := s.db.QueryRowContext(ctx, `SELECT value FROM secrets WHERE name=?`, name).Scan(&ct)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	pt, err := s.decrypt(ct)
	if err != nil {
		return nil, err
	}
	// upgrade legacy plaintext values
	if s.key != nil && !isEncrypted(ct) {
		if enc, err := s.encrypt(pt); err == nil {
			_, _ = s.db.ExecContext(ctx, `UPDATE secrets SET value=?, updated_at=CURRENT_TIMESTAMP WHERE name=?`, enc, name)
		}
	}
	cached := append([]byte(nil), pt...)
	s.mu.Lock()
	s.cache[name] = cacheEntry{val: cached, exp: now.Add(s.ttl)}
	s.mu.Unlock()
	out := append([]byte(nil), cached...)
	return out, nil
}

// Status returns metadata about a stored secret including whether it exists,
// the last four characters of the secret (if present), and the last update
// time. The plaintext secret is never returned.
func (s *Service) Status(ctx context.Context, name string) (exists bool, last4 string, updatedAt time.Time, err error) {
	var ct []byte
	err = s.db.QueryRowContext(ctx, `SELECT value, updated_at FROM secrets WHERE name=?`, name).Scan(&ct, &updatedAt)
	if err == sql.ErrNoRows {
		return false, "", time.Time{}, nil
	}
	if err != nil {
		return false, "", time.Time{}, err
	}
	exists = true
	pt, err := s.decrypt(ct)
	if err != nil {
		return false, "", time.Time{}, err
	}
	switch name {
	case "pufferpanel":
		var c struct {
			ClientSecret string `json:"client_secret"`
		}
		if err := json.Unmarshal(pt, &c); err == nil {
			if n := len(c.ClientSecret); n > 4 {
				last4 = c.ClientSecret[n-4:]
			} else {
				last4 = c.ClientSecret
			}
		}
	default:
		s := string(pt)
		if n := len(s); n > 4 {
			last4 = s[n-4:]
		} else {
			last4 = s
		}
	}
	return
}
