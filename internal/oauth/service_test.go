package oauth

import (
	"context"
	"database/sql"
	"testing"
	"time"

	dbpkg "modsentinel/internal/db"

	_ "modernc.org/sqlite"
)

func setup(t *testing.T) (*Service, context.Context, *sql.DB) {
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
	svc := New(db)
	return svc, context.Background(), db
}

func TestStoreAndGet(t *testing.T) {
	svc, ctx, db := setup(t)
	exp := time.Now().Add(time.Hour).UTC()
	if err := svc.Store(ctx, "prov", Record{Subject: "sub", Scope: "s1", AccessToken: "a", RefreshToken: "r", Expiry: exp}); err != nil {
		t.Fatalf("store: %v", err)
	}
	var cnt int
	if err := db.QueryRow(`SELECT COUNT(*) FROM oauth_audit WHERE provider='prov'`).Scan(&cnt); err != nil {
		t.Fatalf("audit count: %v", err)
	}
	if cnt != 1 {
		t.Fatalf("expected 1 audit row, got %d", cnt)
	}
	rec, err := svc.Get(ctx, "prov")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if rec.AccessToken != "a" || rec.RefreshToken != "r" || rec.Subject != "sub" || rec.Scope != "s1" || rec.Expiry.Unix() != exp.Unix() {
		t.Fatalf("got %#v", rec)
	}
	if err := svc.Clear(ctx, "prov"); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM oauth_audit WHERE provider='prov'`).Scan(&cnt); err != nil {
		t.Fatalf("audit count 2: %v", err)
	}
	if cnt != 2 {
		t.Fatalf("expected 2 audit rows, got %d", cnt)
	}
	rec, err = svc.Get(ctx, "prov")
	if err != nil {
		t.Fatalf("get2: %v", err)
	}
	if rec != (Record{}) {
		t.Fatalf("record not cleared: %#v", rec)
	}
}
