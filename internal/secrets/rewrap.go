package secrets

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	settings "modsentinel/internal/settings"

	"golang.org/x/crypto/argon2"
)

// Rewrap decrypts the stored master key using the current MODSENTINEL_NODE_KEY
// and re-encrypts it with a key derived from newNodeKey in a single
// transaction, updating the stored wrapped key and KDF parameters.
func Rewrap(ctx context.Context, db *sql.DB, newNodeKey string) error {
	if len(newNodeKey) < 16 {
		return errors.New("new node key must be at least 16 characters")
	}
	oldNodeKey := os.Getenv(nodeKeyEnv)
	if len(oldNodeKey) < 16 {
		return errors.New("current MODSENTINEL_NODE_KEY is invalid or missing")
	}
	store := settings.New(db)
	paramsStr, err := store.Get(ctx, kdfParamsSetting)
	if err != nil {
		return err
	}
	wrappedStr, err := store.Get(ctx, wrappedKeySetting)
	if err != nil {
		return err
	}
	if paramsStr == "" || wrappedStr == "" {
		return errors.New("master key not initialized")
	}
	var params kdfParams
	if err := json.Unmarshal([]byte(paramsStr), &params); err != nil {
		return fmt.Errorf("parse kdf params: %w", err)
	}
	salt, err := base64.StdEncoding.DecodeString(params.Salt)
	if err != nil {
		return fmt.Errorf("decode salt: %w", err)
	}
	oldKEK := argon2.IDKey([]byte(oldNodeKey), salt, argonTime, argonMemory, argonThreads, 32)
	oldWrapper, err := New(oldKEK)
	if err != nil {
		return err
	}
	var wk wrappedKey
	if err := json.Unmarshal([]byte(wrappedStr), &wk); err != nil {
		return fmt.Errorf("parse wrapped key: %w", err)
	}
	nonce, err := base64.StdEncoding.DecodeString(wk.Nonce)
	if err != nil {
		return fmt.Errorf("decode nonce: %w", err)
	}
	ct, err := base64.StdEncoding.DecodeString(wk.Ciphertext)
	if err != nil {
		return fmt.Errorf("decode ciphertext: %w", err)
	}
	mk, err := oldWrapper.Decrypt(nonce, ct)
	if err != nil {
		return fmt.Errorf("unwrap master key: %w", err)
	}

	// derive new KEK with fresh salt
	newSalt := make([]byte, saltSize)
	if _, err := rand.Read(newSalt); err != nil {
		return fmt.Errorf("generate salt: %w", err)
	}
	newKEK := argon2.IDKey([]byte(newNodeKey), newSalt, argonTime, argonMemory, argonThreads, 32)
	newWrapper, err := New(newKEK)
	if err != nil {
		return err
	}
	newNonce, newCT, err := newWrapper.Encrypt(mk)
	if err != nil {
		return err
	}
	newWK := wrappedKey{
		Nonce:      base64.StdEncoding.EncodeToString(newNonce),
		Ciphertext: base64.StdEncoding.EncodeToString(newCT),
	}
	wkJSON, _ := json.Marshal(newWK)
	paramsJSON, _ := json.Marshal(kdfParams{Salt: base64.StdEncoding.EncodeToString(newSalt)})

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO app_settings(key, value) VALUES(?,?)
ON CONFLICT(key) DO UPDATE SET value=excluded.value, updated_at=CURRENT_TIMESTAMP`, wrappedKeySetting, string(wkJSON)); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO app_settings(key, value) VALUES(?,?)
ON CONFLICT(key) DO UPDATE SET value=excluded.value, updated_at=CURRENT_TIMESTAMP`, kdfParamsSetting, string(paramsJSON)); err != nil {
		return err
	}
	return tx.Commit()
}

type HealthStatus struct {
	KeyWrapped bool   `json:"key_wrapped"`
	KDF        string `json:"kdf"`
	AEAD       string `json:"aead"`
}

// Health reports whether a wrapped master key exists and the algorithms in use.
func Health(ctx context.Context, db *sql.DB) (HealthStatus, error) {
	store := settings.New(db)
	wrapped, err := store.Get(ctx, wrappedKeySetting)
	if err != nil {
		return HealthStatus{}, err
	}
	status := HealthStatus{KDF: "argon2id", AEAD: "aes-gcm"}
	if wrapped != "" {
		status.KeyWrapped = true
	}
	return status, nil
}
