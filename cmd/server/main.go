package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"

	"github.com/977ADAM/marketing-agents/internal/config"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	cfg, err := config.Load()
	if err != nil {
		logger.Error("config", "err", err)
		os.Exit(1)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	logger.Info("listening", "addr", cfg.HTTPAddr)
	srv := &http.Server{Addr: cfg.HTTPAddr, Handler: mux}
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.Error("serve", "err", err)
		os.Exit(1)
	}
	_ = context.Background()
}
