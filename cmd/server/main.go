package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mycoorigyn/mycoorigyn-marketing-api/internal/config"
	"github.com/mycoorigyn/mycoorigyn-marketing-api/internal/httpapi"
	"github.com/mycoorigyn/mycoorigyn-marketing-api/internal/pageviews"
	"github.com/mycoorigyn/mycoorigyn-marketing-api/internal/postgres"
	"github.com/mycoorigyn/mycoorigyn-marketing-api/internal/submissions"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	cfg, err := config.Load()
	if err != nil {
		logger.Error("configuration error", "error", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		logger.Error("open database", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	pingCtx, pingCancel := context.WithTimeout(ctx, cfg.ReadTimeout)
	defer pingCancel()
	if err := pool.Ping(pingCtx); err != nil {
		logger.Error("connect database", "error", err)
		os.Exit(1)
	}

	store := postgres.NewStore(pool)
	service := submissions.NewService(store, submissions.ServiceOptions{})
	pageViewService := pageviews.NewService(store, pageviews.ServiceOptions{})
	handler := httpapi.NewServer(service, pageViewService, httpapi.Options{
		CORSAllowedOrigins: cfg.PublicCORSAllowedOrigins,
		Logger:             logger,
	})

	server := &http.Server{
		Addr:              cfg.HTTPAddr(),
		Handler:           handler,
		ReadTimeout:       cfg.ReadTimeout,
		WriteTimeout:      cfg.WriteTimeout,
		IdleTimeout:       cfg.IdleTimeout,
		ReadHeaderTimeout: cfg.ReadTimeout,
	}

	errs := make(chan error, 1)
	go func() {
		logger.Info("server listening", "addr", cfg.HTTPAddr())
		errs <- server.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		logger.Info("shutdown requested")
	case err := <-errs:
		if !errors.Is(err, http.ErrServerClosed) {
			logger.Error("server stopped", "error", err)
			os.Exit(1)
		}
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("shutdown failed", "error", err)
		os.Exit(1)
	}
}
