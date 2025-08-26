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
	Encrypt(plaintext []byte) (nonce, ciphertext []byte, err error)
	Decrypt(nonce, ciphertext []byte) ([]byte, error)
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

// Set encrypts and stores a secret for the given name.
func (s *Service) Set(ctx context.Context, name string, plaintext []byte) error {
	if name == "" {
		return sql.ErrNoRows
	}
	nonce, ct, err := s.km.Encrypt(plaintext)
	zero(plaintext)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO secrets(name, nonce, ciphertext) VALUES(?,?,?)
        ON CONFLICT(name) DO UPDATE SET nonce=excluded.nonce, ciphertext=excluded.ciphertext, updated_at=CURRENT_TIMESTAMP`, name, nonce, ct)
	zero(ct)
	zero(nonce)
	s.mu.Lock()
	if e, ok := s.cache[name]; ok {
		zero(e.val)
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
	if e, ok := s.cache[name]; ok {
		zero(e.val)
		delete(s.cache, name)
	}
	s.mu.Unlock()
	return err
}

// DecryptForUse retrieves and decrypts the secret of the given name.
func (s *Service) DecryptForUse(ctx context.Context, name string) ([]byte, error) {
	now := time.Now()
	s.mu.Lock()
	if e, ok := s.cache[name]; ok {
		if now.Before(e.exp) {
			v := append([]byte(nil), e.val...)
			s.mu.Unlock()
			return v, nil
		}
		zero(e.val)
		delete(s.cache, name)
	}
	s.mu.Unlock()

	var nonce, ct []byte
	err := s.db.QueryRowContext(ctx, `SELECT nonce, ciphertext FROM secrets WHERE name=?`, name).Scan(&nonce, &ct)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	b, err := s.km.Decrypt(nonce, ct)
	zero(ct)
	zero(nonce)
	if err != nil {
		zero(b)
		return nil, err
	}
	cached := append([]byte(nil), b...)
	s.mu.Lock()
	s.cache[name] = cacheEntry{val: cached, exp: now.Add(s.ttl)}
	s.mu.Unlock()
	out := append([]byte(nil), cached...)
	zero(b)
	return out, nil
}

// Status returns metadata about a stored secret including whether it exists,
// the last four characters of the secret (if present), and the last update
// time. The plaintext secret is never returned.
func (s *Service) Status(ctx context.Context, name string) (exists bool, last4 string, updatedAt time.Time, err error) {
	var nonce, ct []byte
	err = s.db.QueryRowContext(ctx, `SELECT nonce, ciphertext, updated_at FROM secrets WHERE name=?`, name).Scan(&nonce, &ct, &updatedAt)
	if err == sql.ErrNoRows {
		return false, "", time.Time{}, nil
	}
	if err != nil {
		return false, "", time.Time{}, err
	}
	exists = true
	b, err := s.km.Decrypt(nonce, ct)
	zero(ct)
	zero(nonce)
	if err != nil {
		zero(b)
		return true, "", updatedAt, err
	}
	switch name {
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
