package settings

import (
	"context"
	"database/sql"
)

// Store provides access to application settings stored in the database.
type Store struct {
	db *sql.DB
}

// New returns a new Store using the provided database.
func New(db *sql.DB) *Store {
	return &Store{db: db}
}

// Get returns the setting value for key. If the key does not exist, an empty string and nil error are returned.
func (s *Store) Get(ctx context.Context, key string) (string, error) {
	if key == "" {
		return "", nil
	}
	var val string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM app_settings WHERE key=?`, key).Scan(&val)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return val, err
}

// Set stores or updates the setting for key.
func (s *Store) Set(ctx context.Context, key, value string) error {
	if key == "" {
		return sql.ErrNoRows
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO app_settings(key, value) VALUES(?,?)
ON CONFLICT(key) DO UPDATE SET value=excluded.value, updated_at=CURRENT_TIMESTAMP`, key, value)
	return err
}

// Delete removes the setting for key.
func (s *Store) Delete(ctx context.Context, key string) error {
	if key == "" {
		return nil
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM app_settings WHERE key=?`, key)
	return err
}
