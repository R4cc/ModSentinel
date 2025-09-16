package handlers

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"modsentinel/internal/httpx"
	pppkg "modsentinel/internal/pufferpanel"
)

func recordLatency(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		dur := time.Since(start).Milliseconds()
		latencyMu.Lock()
		latencySamples = append(latencySamples, dur)
		if len(latencySamples) > 100 {
			latencySamples = latencySamples[1:]
		}
		samples := append([]int64(nil), latencySamples...)
		latencyMu.Unlock()
		if len(samples) == 0 {
			return
		}
		sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
		latencyP50.Store(samples[len(samples)/2])
		idx := (len(samples) * 95) / 100
		if idx >= len(samples) {
			idx = len(samples) - 1
		}
		latencyP95.Store(samples[idx])
	})
}

func requestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := uuid.NewString()
		ctx := pppkg.WithRequestID(r.Context(), id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		styleElem := "style-src-elem 'self'"
		styleAttr := "style-src-attr 'unsafe-inline'"
		ctx := r.Context()
		if os.Getenv("APP_ENV") == "production" {
			nonceBytes := make([]byte, 16)
			if _, err := rand.Read(nonceBytes); err == nil {
				nonce := base64.StdEncoding.EncodeToString(nonceBytes)
				styleElem += " 'nonce-" + nonce + "'"
				ctx = context.WithValue(ctx, nonceCtxKey{}, nonce)
			}
		} else {
			styleElem += " 'unsafe-inline'"
		}
		connect := "connect-src 'self'"
		if host := pppkg.APIHost(); host != "" {
			connect += " " + host
		}
		csp := strings.Join([]string{
			"default-src 'self'",
			"frame-ancestors 'none'",
			"base-uri 'none'",
			styleElem,
			styleAttr,
			connect,
			"img-src 'self' data: https:",
		}, "; ")
		w.Header().Set("Content-Security-Policy", csp)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func csrfMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
			http.SetCookie(w, &http.Cookie{Name: "csrf_token", Value: csrfToken, Path: "/", HttpOnly: false, SameSite: http.SameSiteStrictMode})
			next.ServeHTTP(w, r)
			return
		}
		c, err := r.Cookie("csrf_token")
		token := r.Header.Get("X-CSRF-Token")
		if err != nil || token == "" || c.Value != token || token != csrfToken {
			httpx.Write(w, r, httpx.Forbidden("invalid csrf token"))
			return
		}
		next.ServeHTTP(w, r)
	})
}

func requireAdmin() func(http.Handler) http.Handler {
	adminToken := os.Getenv("ADMIN_TOKEN")
	if adminToken == "" {
		return func(next http.Handler) http.Handler { return next }
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := r.Header.Get("Authorization")
			if !strings.HasPrefix(h, "Bearer ") || strings.TrimPrefix(h, "Bearer ") != adminToken {
				httpx.Write(w, r, httpx.Forbidden("admin only"))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func requireAuth() func(http.Handler) http.Handler {
	token := os.Getenv("ADMIN_TOKEN")
	if token == "" {
		return func(next http.Handler) http.Handler { return next }
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := r.Header.Get("Authorization")
			if !strings.HasPrefix(h, "Bearer ") || strings.TrimPrefix(h, "Bearer ") != token {
				httpx.Write(w, r, httpx.Unauthorized("token required"))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func methodNotAllowed(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Allow", http.MethodPost)
	w.WriteHeader(http.StatusMethodNotAllowed)
}

func goneHandler(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusGone)
}
