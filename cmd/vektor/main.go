package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"forge.coltco.net/austin/vektor/internal/api"
	"forge.coltco.net/austin/vektor/internal/auth"
	"forge.coltco.net/austin/vektor/internal/config"
	"forge.coltco.net/austin/vektor/internal/db"

	"github.com/spf13/cobra"
)

var version = "dev"

func main() {
	root := &cobra.Command{
		Use:     "vektor",
		Short:   "Self-hosted project management",
		Version: version,
	}

	root.AddCommand(serveCmd())

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

func serveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Start the Vektor server",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}

			database, err := db.Open(cfg.DataDir)
			if err != nil {
				return fmt.Errorf("opening database: %w", err)
			}
			defer database.Close()

			ctx := context.Background()
			oidcAuth, err := auth.New(ctx, cfg.OIDCIssuer, cfg.OIDCClientID, cfg.OIDCClientSecret, cfg.OIDCRedirectURL)
			if err != nil {
				return fmt.Errorf("setting up OIDC: %w", err)
			}

			srv := &http.Server{
				Addr:    cfg.ListenAddr,
				Handler: api.NewServer(database, oidcAuth),
			}

			// Graceful shutdown
			done := make(chan os.Signal, 1)
			signal.Notify(done, os.Interrupt, syscall.SIGTERM)

			go func() {
				log.Printf("vektor listening on %s", cfg.ListenAddr)
				if err := srv.ListenAndServe(); err != http.ErrServerClosed {
					log.Fatalf("server error: %v", err)
				}
			}()

			<-done
			log.Println("shutting down...")

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			return srv.Shutdown(ctx)
		},
	}
}
