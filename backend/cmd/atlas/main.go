// Command atlas runs the Semantic Atlas server: API plus the embedded web UI.
package main

import (
	"errors"
	"flag"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"semanticatlas/backend/internal/server"
	"semanticatlas/frontend"
)

func env(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func main() {
	addr := flag.String("addr", env("ATLAS_ADDR", "127.0.0.1:8765"), "listen address")
	frontendDir := flag.String("frontend", os.Getenv("FRONTEND_DIR"), "serve the UI from this directory instead of the embedded build")
	flag.Parse()

	var ui fs.FS = frontend.Dist()
	if *frontendDir != "" {
		ui = os.DirFS(*frontendDir)
	}
	srv, err := server.New(server.Config{
		DataDir:        env("DATA_DIR", "backend/data"),
		DatabaseURL:    os.Getenv("DATABASE_URL"),
		SecretKey:      os.Getenv("SECRET_KEY"),
		CookieSecure:   strings.EqualFold(os.Getenv("COOKIE_SECURE"), "true"),
		FrontendOrigin: os.Getenv("FRONTEND_ORIGIN"),
		Frontend:       ui,
	})
	if err != nil {
		log.Fatalf("Не удалось запустить сервер: %v", err)
	}
	defer srv.Close()

	httpServer := &http.Server{Addr: *addr, Handler: srv, ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout: 2 * time.Minute, WriteTimeout: 5 * time.Minute, IdleTimeout: 2 * time.Minute}
	go func() {
		stop := make(chan os.Signal, 1)
		signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
		<-stop
		server.Shutdown(httpServer)
	}()
	log.Printf("Семантический атлас: http://%s", *addr)
	if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}
