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

	"github.com/mycoorigyn/mycoorigyn-marketing-api/internal/approvals"
	"github.com/mycoorigyn/mycoorigyn-marketing-api/internal/config"
	"github.com/mycoorigyn/mycoorigyn-marketing-api/internal/httpapi"
	"github.com/mycoorigyn/mycoorigyn-marketing-api/internal/pageviews"
	"github.com/mycoorigyn/mycoorigyn-marketing-api/internal/postgres"
	"github.com/mycoorigyn/mycoorigyn-marketing-api/internal/securetokens"
	"github.com/mycoorigyn/mycoorigyn-marketing-api/internal/submissions"
	"github.com/mycoorigyn/mycoorigyn-marketing-api/internal/transactionalemail"
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
	tokenStore, err := securetokens.NewFileStore(cfg.TokenSecretRoot)
	if err != nil {
		logger.Error("initialize protected token store", "error", err)
		os.Exit(1)
	}
	var emailSender transactionalemail.Sender = transactionalemail.DisabledSender{}
	if cfg.EmailProvider == "resend" {
		emailSender, err = transactionalemail.NewResendSender(cfg.ResendAPIKeyFile, "", cfg.WriteTimeout)
		if err != nil {
			logger.Error("initialize transactional email", "error", err)
			os.Exit(1)
		}
	}
	service := submissions.NewService(store, submissions.ServiceOptions{
		ReviewLifetime: cfg.ReviewLifetime,
		Tokens:         tokenStore,
		Email:          emailSender,
		EmailFrom:      cfg.EmailFrom,
		EmailReplyTo:   cfg.EmailReplyTo,
		ReviewerEmail:  cfg.ReviewRecipient,
		ReviewBaseURL:  cfg.ReviewBaseURL,
		Logger:         logger,
	})
	approvalService := approvals.NewService(store, approvals.ServiceOptions{
		Tokens:        tokenStore,
		Email:         emailSender,
		From:          cfg.EmailFrom,
		ReplyTo:       cfg.EmailReplyTo,
		SignupBaseURL: cfg.HostedSignupBaseURL,
		GrantLifetime: cfg.GrantLifetime,
		ClaimLifetime: cfg.ClaimLifetime,
		Logger:        logger,
	})
	pageViewService := pageviews.NewService(store, pageviews.ServiceOptions{})
	handler := httpapi.NewServer(service, pageViewService, httpapi.Options{
		CORSAllowedOrigins: cfg.PublicCORSAllowedOrigins,
		Logger:             logger,
		Approvals:          &approvalService,
		ProvisioningSecret: cfg.ProvisioningSecret,
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
