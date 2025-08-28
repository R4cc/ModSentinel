package secrets

import (
	"context"
	"database/sql"
	"encoding/json"
	"sync"
	"time"
)

// Service provides plaintext secret storage backed by a database.
type Service struct {
	db    *sql.DB
	ttl   time.Duration
	mu    sync.Mutex
	cache map[string]cacheEntry
}

// NewService creates a Service using the provided database.
func NewService(db *sql.DB) *Service {
	return &Service{db: db, ttl: 10 * time.Minute, cache: make(map[string]cacheEntry)}
}

type cacheEntry struct {
	val []byte
	exp time.Time
}

// Set stores a secret for the given name in plaintext.
func (s *Service) Set(ctx context.Context, name string, plaintext []byte) error {
	if name == "" {
		return sql.ErrNoRows
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO secrets(name, value) VALUES(?,?)
       ON CONFLICT(name) DO UPDATE SET value=excluded.value, updated_at=CURRENT_TIMESTAMP`, name, plaintext)
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
	cached := append([]byte(nil), ct...)
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
	switch name {
	case "pufferpanel":
		var c struct {
			ClientSecret string `json:"client_secret"`
		}
		if err := json.Unmarshal(ct, &c); err == nil {
			if n := len(c.ClientSecret); n > 4 {
				last4 = c.ClientSecret[n-4:]
			} else {
				last4 = c.ClientSecret
			}
		}
	default:
		s := string(ct)
		if n := len(s); n > 4 {
			last4 = s[n-4:]
		} else {
			last4 = s
		}
	}
	return
}
