package secrets

import (
	"bytes"
	"context"
	"database/sql"
	"testing"
	"time"

	dbpkg "modsentinel/internal/db"

	_ "modernc.org/sqlite"
)

func testManager(t *testing.T) *Manager {
	t.Helper()
	t.Setenv("SECRET_KEYSET", `{"primary":"1","keys":{"1":"000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f"}}`)
	km, err := Load(context.Background())
	if err != nil {
		t.Fatalf("load manager: %v", err)
	}
	return km
}

func openDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", "file:memdb1?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := dbpkg.Init(db); err != nil {
		t.Fatalf("init db: %v", err)
	}
	return db
}

type countingKM struct {
	KeyManager
	decrypts int
}

func (km *countingKM) Decrypt(ciphertext, iv []byte, keyID string) ([]byte, error) {
	km.decrypts++
	return km.KeyManager.Decrypt(ciphertext, iv, keyID)
}

func TestService_RoundTrip(t *testing.T) {
	db := openDB(t)
	defer db.Close()
	svc := NewService(db, testManager(t))
	ctx := context.Background()
	if err := svc.Set(ctx, "modrinth", []byte("secret")); err != nil {
		t.Fatalf("set: %v", err)
	}
	ok, err := svc.Exists(ctx, "modrinth")
	if err != nil || !ok {
		t.Fatalf("exists: %v %v", ok, err)
	}
	b, err := svc.DecryptForUse(ctx, "modrinth")
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if string(b) != "secret" {
		t.Fatalf("got %q", b)
	}
	if err := svc.Delete(ctx, "modrinth"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	ok, err = svc.Exists(ctx, "modrinth")
	if err != nil || ok {
		t.Fatalf("exists after delete: %v %v", ok, err)
	}
}
func TestService_RotateUpdatesKeyID(t *testing.T) {
	db := openDB(t)
	defer db.Close()
	// prepare manager with two keys and primary 1
	km := &Manager{keys: map[string][]byte{
		"1": make([]byte, 32),
		"2": bytes.Repeat([]byte{1}, 32),
	}, primary: "1"}
	svc := NewService(db, km)
	ctx := context.Background()
	if err := svc.Set(ctx, "modrinth", []byte("secret")); err != nil {
		t.Fatalf("set: %v", err)
	}
	var keyID1 string
	if err := db.QueryRow(`SELECT key_id FROM secrets WHERE type=?`, "modrinth").Scan(&keyID1); err != nil {
		t.Fatalf("query key_id1: %v", err)
	}
	// rotate primary key and set again
	km.primary = "2"
	if err := svc.Set(ctx, "modrinth", []byte("secret")); err != nil {
		t.Fatalf("set after rotate: %v", err)
	}
	var keyID2 string
	if err := db.QueryRow(`SELECT key_id FROM secrets WHERE type=?`, "modrinth").Scan(&keyID2); err != nil {
		t.Fatalf("query key_id2: %v", err)
	}
	if keyID1 == keyID2 {
		t.Fatalf("key_id did not update: %s", keyID2)
	}
	b, err := svc.DecryptForUse(ctx, "modrinth")
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if string(b) != "secret" {
		t.Fatalf("got %q", b)
	}
}

func TestService_Cache(t *testing.T) {
	db := openDB(t)
	defer db.Close()
	km := &countingKM{KeyManager: testManager(t)}
	svc := NewService(db, km)
	svc.ttl = 50 * time.Millisecond
	ctx := context.Background()
	if err := svc.Set(ctx, "modrinth", []byte("secret")); err != nil {
		t.Fatalf("set: %v", err)
	}
	if _, err := svc.DecryptForUse(ctx, "modrinth"); err != nil {
		t.Fatalf("decrypt1: %v", err)
	}
	if km.decrypts != 1 {
		t.Fatalf("decrypts1=%d", km.decrypts)
	}
	if _, err := svc.DecryptForUse(ctx, "modrinth"); err != nil {
		t.Fatalf("decrypt2: %v", err)
	}
	if km.decrypts != 1 {
		t.Fatalf("decrypts2=%d", km.decrypts)
	}
	time.Sleep(60 * time.Millisecond)
	if _, err := svc.DecryptForUse(ctx, "modrinth"); err != nil {
		t.Fatalf("decrypt3: %v", err)
	}
	if km.decrypts != 2 {
		t.Fatalf("decrypts3=%d", km.decrypts)
	}
}
