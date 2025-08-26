package token

import (
	"context"
	"database/sql"
	"os"
	"strconv"
	"strings"
	"testing"

	dbpkg "modsentinel/internal/db"
	"modsentinel/internal/secrets"

	_ "modernc.org/sqlite"
)

const nodeKey = "0123456789abcdef"

func TestMain(m *testing.M) {
	os.Setenv("MODSENTINEL_NODE_KEY", nodeKey)
	code := m.Run()
	os.Unsetenv("MODSENTINEL_NODE_KEY")
	os.Exit(code)
}

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
	if err := dbpkg.Migrate(db); err != nil {
		t.Fatalf("migrate db: %v", err)
	}
	t.Setenv("MODSENTINEL_NODE_KEY", nodeKey)
	km, err := secrets.Load(context.Background(), db)
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
	if redacted == tok {
		t.Fatalf("redacted token matches original")
	}
	if !strings.Contains(redacted, "***redacted***") {
		t.Fatalf("missing redaction: %q", redacted)
	}
	if !strings.Contains(redacted, strconv.Itoa(len(tok))) {
		t.Fatalf("missing length: %q", redacted)
	}
}
