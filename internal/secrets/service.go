package secrets

import (
	"context"
	"database/sql"
	"encoding/json"
	"sync"
	"time"
)

// KeyManager abstracts encryption key operations.
type KeyManager interface {
	Encrypt(plaintext []byte) (ciphertext, iv []byte, keyID string, err error)
	Decrypt(ciphertext, iv []byte, keyID string) ([]byte, error)
}

// Service provides encrypted secret storage backed by a database.
type Service struct {
	db    *sql.DB
	km    KeyManager
	ttl   time.Duration
	mu    sync.Mutex
	cache map[string]cacheEntry
}

// NewService creates a Service using the provided database and key manager.
func NewService(db *sql.DB, km KeyManager) *Service {
	return &Service{db: db, km: km, ttl: 10 * time.Minute, cache: make(map[string]cacheEntry)}
}

type cacheEntry struct {
	val []byte
	exp time.Time
}

func zero(b []byte) {
	for i := range b {
		b[i] = 0
	}
}

// Set encrypts and stores a secret for the given type.
func (s *Service) Set(ctx context.Context, typ string, plaintext []byte) error {
	if typ == "" {
		return sql.ErrNoRows
	}
	ct, iv, keyID, err := s.km.Encrypt(plaintext)
	zero(plaintext)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO secrets(type, value_enc, key_id, iv) VALUES(?,?,?,?)
        ON CONFLICT(type) DO UPDATE SET value_enc=excluded.value_enc, key_id=excluded.key_id, iv=excluded.iv, updated_at=CURRENT_TIMESTAMP`, typ, ct, keyID, iv)
	zero(ct)
	zero(iv)
	s.mu.Lock()
	if e, ok := s.cache[typ]; ok {
		zero(e.val)
		delete(s.cache, typ)
	}
	s.mu.Unlock()
	return err
}

// Exists returns whether a secret of the given type is stored.
func (s *Service) Exists(ctx context.Context, typ string) (bool, error) {
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(1) FROM secrets WHERE type=?`, typ).Scan(&n); err != nil {
		return false, err
	}
	return n > 0, nil
}

// Delete removes a stored secret of the given type.
func (s *Service) Delete(ctx context.Context, typ string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM secrets WHERE type=?`, typ)
	s.mu.Lock()
	if e, ok := s.cache[typ]; ok {
		zero(e.val)
		delete(s.cache, typ)
	}
	s.mu.Unlock()
	return err
}

// DecryptForUse retrieves and decrypts the secret of the given type.
func (s *Service) DecryptForUse(ctx context.Context, typ string) ([]byte, error) {
	now := time.Now()
	s.mu.Lock()
	if e, ok := s.cache[typ]; ok {
		if now.Before(e.exp) {
			v := append([]byte(nil), e.val...)
			s.mu.Unlock()
			return v, nil
		}
		zero(e.val)
		delete(s.cache, typ)
	}
	s.mu.Unlock()

	var ct, iv []byte
	var keyID string
	err := s.db.QueryRowContext(ctx, `SELECT value_enc, key_id, iv FROM secrets WHERE type=?`, typ).Scan(&ct, &keyID, &iv)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	b, err := s.km.Decrypt(ct, iv, keyID)
	zero(ct)
	zero(iv)
	if err != nil {
		zero(b)
		return nil, err
	}
	cached := append([]byte(nil), b...)
	s.mu.Lock()
	s.cache[typ] = cacheEntry{val: cached, exp: now.Add(s.ttl)}
	s.mu.Unlock()
	out := append([]byte(nil), cached...)
	zero(b)
	return out, nil
}

// Status returns metadata about a stored secret including whether it exists,
// the last four characters of the secret (if present), and the last update
// time. The plaintext secret is never returned.
func (s *Service) Status(ctx context.Context, typ string) (exists bool, last4 string, updatedAt time.Time, err error) {
	var ct, iv []byte
	var keyID string
	err = s.db.QueryRowContext(ctx, `SELECT value_enc, key_id, iv, updated_at FROM secrets WHERE type=?`, typ).Scan(&ct, &keyID, &iv, &updatedAt)
	if err == sql.ErrNoRows {
		return false, "", time.Time{}, nil
	}
	if err != nil {
		return false, "", time.Time{}, err
	}
	exists = true
	b, err := s.km.Decrypt(ct, iv, keyID)
	zero(ct)
	zero(iv)
	if err != nil {
		zero(b)
		return true, "", updatedAt, err
	}
	switch typ {
	case "pufferpanel":
		var c struct {
			ClientSecret string `json:"client_secret"`
		}
		if err := json.Unmarshal(b, &c); err == nil {
			if n := len(c.ClientSecret); n > 4 {
				last4 = c.ClientSecret[n-4:]
			} else {
				last4 = c.ClientSecret
			}
		}
	default:
		s := string(b)
		if n := len(s); n > 4 {
			last4 = s[n-4:]
		} else {
			last4 = s
		}
	}
	zero(b)
	return
}
