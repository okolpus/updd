// Package server implements the Semantic Atlas HTTP API and serves the web UI.
package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"semanticatlas/backend/internal/llm"
	"semanticatlas/backend/internal/store"
)

const maxRequestBytes = 16 * 1024 * 1024

// Config is read from the environment by the main package.
type Config struct {
	DataDir        string
	DatabaseURL    string
	SecretKey      string
	CookieSecure   bool
	FrontendOrigin string
	Frontend       fs.FS         // built UI (index.html, assets/); nil disables the UI
	AI             *llm.Client   // local language model (Ollama); nil disables it
	AIPoll         time.Duration // how often to look for Qwen coming online (default 20s)
}

type Server struct {
	cfg    Config
	store  *store.Store
	secret []byte
	mux    *http.ServeMux
	limits *rateLimiter
	jobs   sync.WaitGroup
	slots  chan struct{}
	// stageDelay slows analysis stages down; used only by tests.
	stageDelay time.Duration
	aiPoll     time.Duration
	stop       chan struct{}
}

// New opens the database and prepares routes.
func New(cfg Config) (*Server, error) {
	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		return nil, err
	}
	secret, err := loadSecret(cfg)
	if err != nil {
		return nil, err
	}
	st, err := store.Open(cfg.DatabaseURL, cfg.DataDir)
	if err != nil {
		return nil, err
	}
	if cfg.FrontendOrigin == "" {
		cfg.FrontendOrigin = "http://127.0.0.1:5173"
	}
	s := &Server{cfg: cfg, store: st, secret: secret, mux: http.NewServeMux(), limits: newRateLimiter(), slots: make(chan struct{}, 2),
		aiPoll: 20 * time.Second, stop: make(chan struct{})}
	if cfg.AIPoll > 0 {
		s.aiPoll = cfg.AIPoll
	}
	s.routes()
	s.resumeInterrupted()
	s.watchAI()
	return s, nil
}

// Close waits for running analyses and closes the database.
func (s *Server) Close() error {
	close(s.stop)
	s.jobs.Wait()
	return s.store.Close()
}

// loadSecret uses SECRET_KEY or a key persisted in the data directory.
func loadSecret(cfg Config) ([]byte, error) {
	if cfg.SecretKey != "" {
		return []byte(cfg.SecretKey), nil
	}
	path := filepath.Join(cfg.DataDir, ".jwt-key")
	if f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600); err == nil {
		key := make([]byte, 48)
		if _, err := rand.Read(key); err != nil {
			f.Close()
			return nil, err
		}
		_, werr := f.WriteString(hex.EncodeToString(key))
		f.Close()
		if werr != nil {
			return nil, werr
		}
	} else if !errors.Is(err, os.ErrExist) {
		return nil, err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return []byte(strings.TrimSpace(string(raw))), nil
}

func (s *Server) routes() {
	m := s.mux
	m.HandleFunc("GET /api/health", s.health)
	m.HandleFunc("POST /api/auth/register", s.register)
	m.HandleFunc("POST /api/auth/login", s.login)
	m.HandleFunc("POST /api/auth/demo", s.demo)
	m.HandleFunc("GET /api/auth/me", s.authed(s.me))
	m.HandleFunc("GET /api/ai", s.authed(func(w http.ResponseWriter, r *http.Request, _ *store.User) {
		writeJSON(w, http.StatusOK, s.cfg.AI.Status(r.Context()))
	}))
	m.HandleFunc("POST /api/auth/logout", s.logout)
	m.HandleFunc("PATCH /api/profile", s.authed(s.updateProfile))
	m.HandleFunc("GET /api/documents", s.authed(s.listDocuments))
	m.HandleFunc("POST /api/documents", s.authed(s.createDocument))
	m.HandleFunc("POST /api/documents/upload", s.authed(s.uploadDocument))
	m.HandleFunc("GET /api/documents/{id}", s.authed(s.getDocument))
	m.HandleFunc("PATCH /api/documents/{id}", s.authed(s.patchDocument))
	m.HandleFunc("DELETE /api/documents/{id}", s.authed(s.deleteDocument))
	m.HandleFunc("POST /api/documents/{id}/analyze", s.authed(s.reanalyze))
	m.HandleFunc("GET /api/documents/{id}/status", s.authed(s.documentStatus))
	m.HandleFunc("GET /api/documents/{id}/notes", s.authed(s.listNotes))
	m.HandleFunc("POST /api/documents/{id}/notes", s.authed(s.createNote))
	m.HandleFunc("DELETE /api/documents/{id}/notes/{note}", s.authed(s.deleteNote))
	m.HandleFunc("GET /api/documents/{id}/questions", s.authed(s.listQuestions))
	m.HandleFunc("POST /api/documents/{id}/questions", s.authed(s.createQuestion))
	m.HandleFunc("GET /api/documents/{id}/export.pdf", s.authed(s.exportPDF))
	m.HandleFunc("GET /api/documents/{id}/export.png", s.authed(s.exportPNG))
	m.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		fail(w, http.StatusNotFound, "Маршрут API не найден.")
	})
	m.HandleFunc("/", s.spa)
}

// ServeHTTP applies cross-site and size protections and security headers.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h := w.Header()
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Referrer-Policy", "same-origin")
	h.Set("X-Frame-Options", "DENY")
	if strings.HasPrefix(r.URL.Path, "/api/") {
		h.Set("Cache-Control", "no-store")
	}
	switch r.Method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		if origin := r.Header.Get("Origin"); origin != "" && !s.allowedOrigin(r, origin) {
			fail(w, http.StatusForbidden, "Запрос с другого сайта отклонён.")
			return
		}
		if raw := r.Header.Get("Content-Length"); raw != "" {
			n, err := strconv.ParseInt(raw, 10, 64)
			if err != nil {
				fail(w, http.StatusBadRequest, "Некорректный размер запроса.")
				return
			}
			if n > maxRequestBytes {
				fail(w, http.StatusRequestEntityTooLarge, "Размер запроса больше 16 МБ.")
				return
			}
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxRequestBytes)
	}
	s.mux.ServeHTTP(w, r)
}

func (s *Server) allowedOrigin(r *http.Request, origin string) bool {
	return origin == "http://"+r.Host || origin == "https://"+r.Host || origin == s.cfg.FrontendOrigin || origin == "http://localhost:5173"
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	if err := s.store.Ping(r.Context()); err != nil {
		fail(w, http.StatusServiceUnavailable, "База данных недоступна.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "database": "ok"})
}

func (s *Server) spa(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Frontend == nil {
		fail(w, http.StatusServiceUnavailable, "Интерфейс ещё не собран. Выполните npm ci и npm run build в frontend.")
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/")
	// Only files of the build are served; any other path gets the SPA entry point.
	if strings.HasPrefix(path, "assets/") || path == "favicon.svg" {
		if data, err := fs.ReadFile(s.cfg.Frontend, path); err == nil {
			if strings.HasPrefix(path, "assets/") {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			}
			http.ServeContent(w, r, path, time.Time{}, strings.NewReader(string(data)))
			return
		}
		http.NotFound(w, r)
		return
	}
	index, err := fs.ReadFile(s.cfg.Frontend, "index.html")
	if err != nil {
		fail(w, http.StatusServiceUnavailable, "Интерфейс ещё не собран. Выполните npm ci и npm run build в frontend.")
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Write(index)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(value); err != nil {
		log.Printf("write json: %v", err)
	}
}

func fail(w http.ResponseWriter, status int, detail string) {
	writeJSON(w, status, map[string]string{"detail": detail})
}

func serverError(w http.ResponseWriter, err error) {
	log.Printf("internal error: %v", err)
	fail(w, http.StatusInternalServerError, "Внутренняя ошибка сервера. Попробуйте ещё раз.")
}

// decode reads a JSON object body into a generic map so absent and null fields differ.
func decode(w http.ResponseWriter, r *http.Request) (map[string]json.RawMessage, bool) {
	var body map[string]json.RawMessage
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(&body); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			fail(w, http.StatusRequestEntityTooLarge, "Размер запроса больше 16 МБ.")
		} else {
			fail(w, http.StatusUnprocessableEntity, "Проверьте поля формы: тело запроса должно быть JSON-объектом.")
		}
		return nil, false
	}
	if body == nil {
		body = map[string]json.RawMessage{}
	}
	return body, true
}

type rateLimiter struct {
	mu       sync.Mutex
	attempts map[string][]time.Time
}

func newRateLimiter() *rateLimiter { return &rateLimiter{attempts: map[string][]time.Time{}} }

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// allow keeps a five-minute sliding window per client and action.
func (l *rateLimiter) allow(r *http.Request, action string, limit int) bool {
	key := clientIP(r) + ":" + action
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.attempts) > 5000 {
		for k, v := range l.attempts {
			if len(v) == 0 || now.Sub(v[len(v)-1]) > 5*time.Minute {
				delete(l.attempts, k)
			}
		}
	}
	var recent []time.Time
	for _, t := range l.attempts[key] {
		if now.Sub(t) < 5*time.Minute {
			recent = append(recent, t)
		}
	}
	if len(recent) >= limit {
		l.attempts[key] = recent
		return false
	}
	l.attempts[key] = append(recent, now)
	return true
}

// Shutdown helper for graceful stop.
func Shutdown(srv *http.Server) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	srv.Shutdown(ctx)
}
