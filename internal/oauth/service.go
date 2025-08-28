package oauth

import (
	"context"
	"database/sql"
	"time"
)

// Record represents stored OAuth tokens for a provider.
type Record struct {
	Subject      string
	Scope        string
	AccessToken  string
	RefreshToken string
	Expiry       time.Time
}

// Service manages storage of OAuth tokens.
type Service struct {
	db *sql.DB
}

// New creates a Service using the provided database.
func New(db *sql.DB) *Service {
	return &Service{db: db}
}

// Store saves the OAuth tokens for a provider.
func (s *Service) Store(ctx context.Context, provider string, r Record) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO oauth_tokens(provider, subject, scope, access_token, refresh_token, expiry_utc) VALUES(?,?,?,?,?,?)
ON CONFLICT(provider) DO UPDATE SET subject=excluded.subject, scope=excluded.scope, access_token=excluded.access_token, refresh_token=excluded.refresh_token, expiry_utc=excluded.expiry_utc, updated_at=CURRENT_TIMESTAMP`, provider, r.Subject, r.Scope, r.AccessToken, r.RefreshToken, r.Expiry.UTC())
	if err == nil {
		_, err = tx.ExecContext(ctx, `INSERT INTO oauth_audit(provider) VALUES(?)`, provider)
	}
	if err != nil {
		tx.Rollback()
	} else {
		err = tx.Commit()
	}
	return err
}

// Get retrieves stored tokens for a provider.
func (s *Service) Get(ctx context.Context, provider string) (Record, error) {
	var r Record
	var exp sql.NullTime
	err := s.db.QueryRowContext(ctx, `SELECT subject, scope, access_token, refresh_token, expiry_utc FROM oauth_tokens WHERE provider=?`, provider).Scan(&r.Subject, &r.Scope, &r.AccessToken, &r.RefreshToken, &exp)
	if err == sql.ErrNoRows {
		return Record{}, nil
	}
	if err != nil {
		return Record{}, err
	}
	if exp.Valid {
		r.Expiry = exp.Time
	}
	return r, nil
}

// Clear removes stored tokens for a provider.
func (s *Service) Clear(ctx context.Context, provider string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM oauth_tokens WHERE provider=?`, provider); err != nil {
		tx.Rollback()
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO oauth_audit(provider) VALUES(?)`, provider); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}
