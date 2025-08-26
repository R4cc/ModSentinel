package secrets

import (
	"bytes"
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"

	dbpkg "modsentinel/internal/db"

	"golang.org/x/crypto/argon2"
	_ "modernc.org/sqlite"
)

const nodeKey = "0123456789abcdef"

func TestEncryptDecryptRoundtrip(t *testing.T) {
	key := make([]byte, 32)
	m, err := New(key)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	nonce, ct, err := m.Encrypt([]byte("hello"))
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	pt, err := m.Decrypt(nonce, ct)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if string(pt) != "hello" {
		t.Fatalf("got %q", pt)
	}
}

func TestArgon2Deterministic(t *testing.T) {
	salt := make([]byte, saltSize)
	for i := range salt {
		salt[i] = byte(i)
	}
	k1 := argon2.IDKey([]byte("passphrase"), salt, argonTime, argonMemory, argonThreads, 32)
	k2 := argon2.IDKey([]byte("passphrase"), salt, argonTime, argonMemory, argonThreads, 32)
	if !bytes.Equal(k1, k2) {
		t.Fatalf("keys differ")
	}
	k3 := argon2.IDKey([]byte("passphrase2"), salt, argonTime, argonMemory, argonThreads, 32)
	if bytes.Equal(k1, k3) {
		t.Fatalf("expected different keys")
	}
}

func TestNonceUniqueness(t *testing.T) {
	key := make([]byte, 32)
	m, err := New(key)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	seen := make(map[string]struct{})
	for i := 0; i < 1000; i++ {
		nonce, _, err := m.Encrypt([]byte("data"))
		if err != nil {
			t.Fatalf("encrypt: %v", err)
		}
		s := string(nonce)
		if _, ok := seen[s]; ok {
			t.Fatalf("duplicate nonce at %d", i)
		}
		seen[s] = struct{}{}
	}
}

func TestBootstrapCreatesWrappedKey(t *testing.T) {
	db, err := sql.Open("sqlite", "file:memdb1?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()
	if err := dbpkg.Init(db); err != nil {
		t.Fatalf("init db: %v", err)
	}
	if err := dbpkg.Migrate(db); err != nil {
		t.Fatalf("migrate db: %v", err)
	}
	t.Setenv("MODSENTINEL_NODE_KEY", nodeKey)
	km1, err := Load(context.Background(), db)
	if err != nil {
		t.Fatalf("load1: %v", err)
	}
	var mk, params string
	if err := db.QueryRow(`SELECT value FROM app_settings WHERE key=?`, wrappedKeySetting).Scan(&mk); err != nil || mk == "" {
		t.Fatalf("wrapped key not stored: %v %q", err, mk)
	}
	if err := db.QueryRow(`SELECT value FROM app_settings WHERE key=?`, kdfParamsSetting).Scan(&params); err != nil || params == "" {
		t.Fatalf("kdf params not stored: %v %q", err, params)
	}
	km2, err := Load(context.Background(), db)
	if err != nil {
		t.Fatalf("load2: %v", err)
	}
	nonce, ct, err := km1.Encrypt([]byte("hi"))
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	pt, err := km2.Decrypt(nonce, ct)
	if err != nil || string(pt) != "hi" {
		t.Fatalf("decrypt with persisted key: %v %q", err, pt)
	}
}

func TestLoadFailsWithWrongNodeKey(t *testing.T) {
	db, err := sql.Open("sqlite", "file:memdb1?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()
	if err := dbpkg.Init(db); err != nil {
		t.Fatalf("init db: %v", err)
	}
	if err := dbpkg.Migrate(db); err != nil {
		t.Fatalf("migrate db: %v", err)
	}
	t.Setenv("MODSENTINEL_NODE_KEY", nodeKey)
	if _, err := Load(context.Background(), db); err != nil {
		t.Fatalf("first load: %v", err)
	}
	t.Setenv("MODSENTINEL_NODE_KEY", "differentsecret000")
	if _, err := Load(context.Background(), db); err == nil || !strings.Contains(err.Error(), "authentication failed") {
		t.Fatalf("expected authentication failed, got %v", err)
	}
}

func TestLoadRequiresNodeKey(t *testing.T) {
	db, err := sql.Open("sqlite", "file:memdb1?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()
	if err := dbpkg.Init(db); err != nil {
		t.Fatalf("init db: %v", err)
	}
	if err := dbpkg.Migrate(db); err != nil {
		t.Fatalf("migrate db: %v", err)
	}
	// missing key
	os.Unsetenv("MODSENTINEL_NODE_KEY")
	if _, err := Load(context.Background(), db); err == nil {
		t.Fatalf("expected error for missing node key")
	}
	// short key
	t.Setenv("MODSENTINEL_NODE_KEY", "short")
	if _, err := Load(context.Background(), db); err == nil {
		t.Fatalf("expected error for short node key")
	}
}

func TestRewrap(t *testing.T) {
	db, err := sql.Open("sqlite", "file:memdb1?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()
	if err := dbpkg.Init(db); err != nil {
		t.Fatalf("init db: %v", err)
	}
	if err := dbpkg.Migrate(db); err != nil {
		t.Fatalf("migrate db: %v", err)
	}
	t.Setenv("MODSENTINEL_NODE_KEY", nodeKey)
	if _, err := Load(context.Background(), db); err != nil {
		t.Fatalf("load: %v", err)
	}
	newKey := "abcdefghijklmnopqrstuvwx123456"
	if err := Rewrap(context.Background(), db, newKey); err != nil {
		t.Fatalf("rewrap: %v", err)
	}
	t.Setenv("MODSENTINEL_NODE_KEY", newKey)
	if _, err := Load(context.Background(), db); err != nil {
		t.Fatalf("load new: %v", err)
	}
	t.Setenv("MODSENTINEL_NODE_KEY", nodeKey)
	if _, err := Load(context.Background(), db); err == nil {
		t.Fatalf("old key should fail")
	}
}
