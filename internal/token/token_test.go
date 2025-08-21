package token

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	dbpkg "modsentinel/internal/db"
	"modsentinel/internal/secrets"

	_ "modernc.org/sqlite"
)

func initSvc(t *testing.T) {
	t.Helper()
	db, err := sql.Open("sqlite", "file:memdb1?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := dbpkg.Init(db); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Setenv("SECRET_KEYSET", `{"primary":"1","keys":{"1":"000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f"}}`)
	km, err := secrets.Load(context.Background())
	if err != nil {
		t.Fatalf("load manager: %v", err)
	}
	Init(secrets.NewService(db, km))
}

func TestTokenStorage(t *testing.T) {
	initSvc(t)
	tok := "abcdef123456"
	if err := SetToken(tok); err != nil {
		t.Fatalf("set token: %v", err)
	}
	got, err := GetToken()
	if err != nil {
		t.Fatalf("get token: %v", err)
	}
	if got != tok {
		t.Fatalf("got %q want %q", got, tok)
	}
	if err := ClearToken(); err != nil {
		t.Fatalf("clear token: %v", err)
	}
	got, err = GetToken()
	if err != nil {
		t.Fatalf("get after clear: %v", err)
	}
	if got != "" {
		t.Fatalf("expected empty token, got %q", got)
	}
}

func TestTokenRedaction(t *testing.T) {
	initSvc(t)
	tok := "abcdef1234567890"
	if err := SetToken(tok); err != nil {
		t.Fatalf("set token: %v", err)
	}
	stored, redacted, err := TokenForLog()
	if err != nil {
		t.Fatalf("token for log: %v", err)
	}
	if stored != tok {
		t.Fatalf("stored token mismatch: got %q want %q", stored, tok)
	}
	if stored == redacted {
		t.Fatalf("redacted token matches original")
	}
	middle := tok[4 : len(tok)-4]
	if strings.Contains(redacted, middle) {
		t.Fatalf("redacted token reveals middle: %q", redacted)
	}
	if !strings.HasPrefix(redacted, tok[:4]) || !strings.HasSuffix(redacted, tok[len(tok)-4:]) {
		t.Fatalf("redacted token missing expected prefix or suffix: %q", redacted)
	}
}
