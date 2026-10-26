package main

import (
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
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
		Addr:    env("ADDR", ":8080"),
		Handler: srv.Routes(),
		// The one timeout worth keeping: a socket that never finishes its headers
		// is the cheapest way to starve this server.
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Info("listening", "addr", httpSrv.Addr, "db", dbPath)
	log.Error("serve", "err", httpSrv.ListenAndServe())
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
