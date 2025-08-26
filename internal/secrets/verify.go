package secrets

import (
	"context"
	"database/sql"
	"fmt"
)

// VerifyAll attempts to decrypt all stored secrets to ensure the master key is correct.
func VerifyAll(ctx context.Context, db *sql.DB, km KeyManager) error {
	rows, err := db.QueryContext(ctx, `SELECT name, nonce, ciphertext FROM secrets`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		var nonce, ct []byte
		if err := rows.Scan(&name, &nonce, &ct); err != nil {
			return err
		}
		if _, err := km.Decrypt(nonce, ct); err != nil {
			return fmt.Errorf("decrypt %s: %w", name, err)
		}
	}
	return rows.Err()
}
