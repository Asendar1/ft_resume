package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
)

func main() {
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))

	dbPath := env("DB_PATH", "applications.db")
	if dir := filepath.Dir(dbPath); dir != "." {
		os.MkdirAll(dir, 0o755)
	}
	store, err := Open(dbPath)
	if err != nil {
		log.Error("open database", "err", err)
		os.Exit(1)
	}
	defer store.Close()

	srv := &Server{store: store}
	if key := os.Getenv("LLM_API_KEY"); key != "" {
		srv.llm = NewLLM(
			env("LLM_BASE_URL", "https://api.openai.com/v1"),
			key,
			env("LLM_MODEL", "gpt-4o-mini"),
		)
		log.Info("model configured")
	} else {
		log.Warn("LLM_API_KEY unset; /advice will answer 503")
	}

	httpSrv := &http.Server{
		Addr:              env("ADDR", ":8080"),
		Handler:           srv.Routes(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	go func() {
		log.Info("listening", "addr", httpSrv.Addr, "db", dbPath)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("serve", "err", err)
			os.Exit(1)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpSrv.Shutdown(ctx); err != nil {
		log.Error("shutdown", "err", err)
	}
	log.Info("stopped")
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
