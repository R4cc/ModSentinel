package main

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/go-co-op/gocron"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	dbpkg "modsentinel/internal/db"
	"modsentinel/internal/handlers"
	"modsentinel/internal/httpx"
	logx "modsentinel/internal/logx"
	oauth "modsentinel/internal/oauth"
	pppkg "modsentinel/internal/pufferpanel"
	"modsentinel/internal/secrets"
	settingspkg "modsentinel/internal/settings"
	tokenpkg "modsentinel/internal/token"

	_ "modernc.org/sqlite"
)

//go:embed frontend/dist/* favicon.ico
var distFS embed.FS

func resolveDBPath(p string) string {
	info, err := os.Stat(p)
	if err == nil && info.IsDir() {
		return filepath.Join(p, "mods.db")
	}
	return p
}

func ensureFile(p string) error {
	info, err := os.Stat(p)
	if err == nil {
		if info.IsDir() {
			return fmt.Errorf("%s is a directory", p)
		}
		return nil
	}
	if os.IsNotExist(err) {
		f, err := os.OpenFile(p, os.O_CREATE|os.O_RDWR, 0666)
		if err != nil {
			return err
		}
		return f.Close()
	}
	return err
}

func checkDBRW(db *sql.DB) error {
	if err := db.Ping(); err != nil {
		return err
	}
	if _, err := db.Exec("CREATE TABLE IF NOT EXISTS __rw_check(id INTEGER)"); err != nil {
		return err
	}
	if _, err := db.Exec("DROP TABLE __rw_check"); err != nil {
		return err
	}
	return nil
}

func main() {
	log.Logger = zerolog.New(logx.NewRedactor(os.Stdout)).With().Timestamp().Logger()
	if len(os.Args) > 1 && os.Args[1] == "admin" {
		adminMain(os.Args[2:])
		return
	}
	path := resolveDBPath("/data/modsentinel.db")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		log.Fatal().Err(err).Str("dir", filepath.Dir(path)).Msg("create db dir")
	}
	if err := ensureFile(path); err != nil {
		log.Fatal().Err(err).Str("path", path).Msg("create db file")
	}

	db, err := sql.Open("sqlite", fmt.Sprintf("file:%s?_busy_timeout=5000&_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)", path))
	if err != nil {
		log.Fatal().Err(err).Msg("open db")
	}
	defer db.Close()

	if err := checkDBRW(db); err != nil {
		log.Fatal().Err(err).Msg("db read/write test")
	}

	if err := dbpkg.Init(db); err != nil {
		log.Fatal().Err(err).Msg("init db")
	}
	if err := dbpkg.Migrate(db); err != nil {
		log.Fatal().Err(err).Msg("migrate db")
	}
	svc := secrets.NewService(db)
	cfg := settingspkg.New(db)
	oauthSvc := oauth.New(db)
	tokenpkg.Init(svc)
	pppkg.Init(svc, cfg, oauthSvc)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	scheduler := gocron.NewScheduler(time.UTC)
	scheduler.Every(1).Hour().Do(func() { handlers.CheckUpdates(ctx, db) })
	scheduler.StartAsync()
	pppkg.StartRefresh(ctx)

	r := handlers.New(db, distFS, svc)
	var shuttingDown atomic.Bool
	handler := withShutdown(r, &shuttingDown)

	srv := &http.Server{
		Addr:         ":8080",
		Handler:      handler,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shuttingDown.Store(true)
		scheduler.Stop()
		time.Sleep(200 * time.Millisecond)
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Error().Err(err).Msg("server shutdown")
		}
	}()

	log.Info().Msg("starting server on :8080")
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal().Err(err).Msg("server failed")
	}
}

func adminMain(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: modsentinel admin <command>")
		os.Exit(1)
	}
	switch args[0] {
	default:
		fmt.Fprintln(os.Stderr, "unknown admin command")
		os.Exit(1)
	}
}

func withShutdown(next http.Handler, flag *atomic.Bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if flag.Load() {
			httpx.Write(w, r, httpx.Unavailable("server shutting down"))
			return
		}
		next.ServeHTTP(w, r)
	})
}
