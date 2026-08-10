package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"forge.coltco.net/austin/vektor/internal/api"
	"forge.coltco.net/austin/vektor/internal/authn"
	"forge.coltco.net/austin/vektor/internal/config"
	"forge.coltco.net/austin/vektor/internal/db"

	"github.com/spf13/cobra"
)

var version = "dev"

func main() {
	root := &cobra.Command{
		Use:     "vek",
		Short:   "Self-hosted project management",
		Version: version,
	}
	logger := setupLogging()

	root.AddCommand(serveCmd(logger))

	if cmd, err := root.ExecuteC(); err != nil {
		if cmd != nil && cmd.SilenceErrors {
			logger.Error("startup failed", "error", err)
		}
		os.Exit(1)
	}
}

func serveCmd(logger *slog.Logger) *cobra.Command {
	return &cobra.Command{
		Use:           "serve",
		Short:         "Start the Vektor server",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			// While I could globally call `slog`, I want to make it extra clear
			// and remove any confusion around calling logs before it's been setup.
			cfg, err := config.Load(logger)
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}

			store, err := db.Open(cmd.Context(), cfg.DataDir, logger)
			if err != nil {
				return fmt.Errorf("opening database: %w", err)
			}
			defer store.Close()

			var srv http.Server
			var authenticator authn.Authenticator

			if !cfg.LocalAuth {
				ctx := context.Background()

				oidcAuth, err := authn.NewOIDC(
					ctx,
					cfg.OIDCIssuer,
					cfg.OIDCClientID,
					cfg.OIDCClientSecret,
					cfg.OIDCRedirectURL,
					[]byte(cfg.SessionSecret),
					store.DB,
					logger,
				)
				if err != nil {
					return fmt.Errorf("setting up OIDC: %w", err)
				}

				authenticator = oidcAuth
			} else {
				authenticator = authn.NewLocal(
					[]byte(cfg.SessionSecret),
					store.DB,
					logger,
				)
			}

			srv = http.Server{
				Addr:    cfg.ListenAddr,
				Handler: api.NewServer(store.DB, authenticator, logger),
			}

			// Graceful shutdown
			done := make(chan os.Signal, 1)
			signal.Notify(done, os.Interrupt, syscall.SIGTERM)

			errCh := make(chan error, 1)
			ln, err := net.Listen("tcp", cfg.ListenAddr)
			if err != nil {
				return fmt.Errorf("listen: %w", err)
			}
			logger.Info("listening", "addr", ln.Addr().String())

			go func() {
				if err := srv.Serve(ln); !errors.Is(err, http.ErrServerClosed) {
					errCh <- err
				}
			}()

			select {
			case err := <-errCh:
				return fmt.Errorf("server error: %w", err)
			case <-done:
				logger.Info("shutting down")
			}

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			return srv.Shutdown(ctx)
		},
	}
}

// Create a structure logger. Sets log level via config and if none is present,
// it will default to `slog.LevelInfo`.
func setupLogging() *slog.Logger {
	lvl := new(slog.LevelVar)
	configValue := []byte(os.Getenv("VEKTOR_LOG_LEVEL"))
	if err := lvl.UnmarshalText(configValue); err != nil {
		lvl.Set(slog.LevelInfo)
	}

	opts := &slog.HandlerOptions{Level: lvl}

	var h slog.Handler
	if os.Getenv("VEKTOR_LOG_FORMAT") == "text" {
		h = slog.NewTextHandler(os.Stdout, opts)
	} else {
		h = slog.NewJSONHandler(os.Stdout, opts)
	}

	log := slog.New(h)
	slog.SetDefault(log)
	return log
}
