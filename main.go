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

func main() {
	log.Logger = log.Output(zerolog.New(os.Stdout).With().Timestamp().Logger())
	path := resolveDBPath("mods.db")
	if err := ensureFile(path); err != nil {
		log.Fatal().Err(err).Str("path", path).Msg("create db file")
	}

	db, err := sql.Open("sqlite", fmt.Sprintf("file:%s?_busy_timeout=5000&_pragma=foreign_keys(1)", path))
	if err != nil {
		log.Fatal().Err(err).Msg("open db")
	}
	defer db.Close()

	if err := dbpkg.Init(db); err != nil {
		log.Fatal().Err(err).Msg("init db")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	scheduler := gocron.NewScheduler(time.UTC)
	scheduler.Every(1).Hour().Do(func() { handlers.CheckUpdates(ctx, db) })
	scheduler.StartAsync()

	r := handlers.New(db, distFS)
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

func withShutdown(next http.Handler, flag *atomic.Bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if flag.Load() {
			httpx.Write(w, r, httpx.Unavailable("server shutting down"))
			return
		}
		next.ServeHTTP(w, r)
	})
}
