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
	m, err := New(bytes.Repeat([]byte{0x01}, 32))
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	return m
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

func (km *countingKM) Decrypt(nonce, ct []byte) ([]byte, error) {
	km.decrypts++
	return km.KeyManager.Decrypt(nonce, ct)
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
