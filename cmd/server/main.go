package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/Servitor108/go-webhook-platform/internal/api"
	"github.com/Servitor108/go-webhook-platform/internal/config"
	"github.com/Servitor108/go-webhook-platform/internal/delivery"
	"github.com/Servitor108/go-webhook-platform/internal/store"
)

func main() {
	cfg := config.Load()
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	db, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil { logger.Error("connect database", "error", err); os.Exit(1) }
	defer db.Close()
	if err := db.Ping(ctx); err != nil { logger.Error("ping database", "error", err); os.Exit(1) }

	repository := store.New(db)
	mux := http.NewServeMux()
	api.New(repository, logger).Register(mux)
	server := &http.Server{Addr: cfg.HTTPAddr, Handler: mux, ReadHeaderTimeout: 5*time.Second}

	go func() {
		logger.Info("http server started", "addr", cfg.HTTPAddr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("http server", "error", err); stop()
		}
	}()
	worker := delivery.NewWorker(repository, logger, cfg.WorkerConcurrency, cfg.RequestTimeout, cfg.MaxAttempts)
	go worker.Run(ctx)

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil { logger.Error("shutdown", "error", err) }
}
