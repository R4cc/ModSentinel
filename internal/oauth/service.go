package oauth

import (
	"context"
	"database/sql"
	"time"

	"modsentinel/internal/secrets"
)

// Record represents stored OAuth tokens for a provider.
type Record struct {
	Subject      string
	Scope        string
	AccessToken  string
	RefreshToken string
	Expiry       time.Time
}

// Service manages encrypted storage of OAuth tokens.
type Service struct {
	db *sql.DB
	km secrets.KeyManager
}

// New creates a Service using the provided database and key manager.
func New(db *sql.DB, km secrets.KeyManager) *Service {
	return &Service{db: db, km: km}
}

func zero(b []byte) {
	for i := range b {
		b[i] = 0
	}
}

// Store encrypts and stores the OAuth tokens for a provider.
func (s *Service) Store(ctx context.Context, provider string, r Record) error {
	an, ac, err := s.km.Encrypt([]byte(r.AccessToken))
	if err != nil {
		return err
	}
	rn, rc, err := s.km.Encrypt([]byte(r.RefreshToken))
	if err != nil {
		zero(ac)
		zero(an)
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		zero(ac)
		zero(an)
		zero(rc)
		zero(rn)
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO oauth_tokens(provider, subject, scope, access_nonce, access_cipher, refresh_nonce, refresh_cipher, expiry_utc) VALUES(?,?,?,?,?,?,?,?)
ON CONFLICT(provider) DO UPDATE SET subject=excluded.subject, scope=excluded.scope, access_nonce=excluded.access_nonce, access_cipher=excluded.access_cipher, refresh_nonce=excluded.refresh_nonce, refresh_cipher=excluded.refresh_cipher, expiry_utc=excluded.expiry_utc, updated_at=CURRENT_TIMESTAMP`, provider, r.Subject, r.Scope, an, ac, rn, rc, r.Expiry.UTC())
	if err == nil {
		_, err = tx.ExecContext(ctx, `INSERT INTO oauth_audit(provider) VALUES(?)`, provider)
	}
	if err != nil {
		tx.Rollback()
	} else {
		err = tx.Commit()
	}
	zero(ac)
	zero(an)
	zero(rc)
	zero(rn)
	return err
}

// Get retrieves and decrypts stored tokens for a provider.
func (s *Service) Get(ctx context.Context, provider string) (Record, error) {
	var r Record
	var an, ac, rn, rc []byte
	var exp sql.NullTime
	err := s.db.QueryRowContext(ctx, `SELECT subject, scope, access_nonce, access_cipher, refresh_nonce, refresh_cipher, expiry_utc FROM oauth_tokens WHERE provider=?`, provider).Scan(&r.Subject, &r.Scope, &an, &ac, &rn, &rc, &exp)
	if err == sql.ErrNoRows {
		return Record{}, nil
	}
	if err != nil {
		return Record{}, err
	}
	ab, err := s.km.Decrypt(an, ac)
	zero(ac)
	zero(an)
	if err != nil {
		zero(ab)
		return Record{}, err
	}
	r.AccessToken = string(ab)
	zero(ab)
	rb, err := s.km.Decrypt(rn, rc)
	zero(rc)
	zero(rn)
	if err != nil {
		zero(rb)
		return Record{}, err
	}
	r.RefreshToken = string(rb)
	zero(rb)
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
