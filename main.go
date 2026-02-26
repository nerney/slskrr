package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/nerney/slskrr/newznab"
	"github.com/nerney/slskrr/sabnzbd"
	"github.com/nerney/slskrr/slskd"
	"github.com/nerney/slskrr/store"
)

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

	cfg, err := LoadConfig()
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	slskdClient := slskd.NewClient(cfg.SlskdURL, cfg.SlskdAPIKey)
	st := store.New()

	// Compute the base URL for self-referencing download links
	baseURL := "http://localhost" + cfg.ListenAddr

	newznabHandler := &newznab.Handler{
		SlskdClient:   slskdClient,
		APIKey:        cfg.APIKey,
		SearchTimeout: cfg.SearchTimeout,
		BaseURL:       baseURL,
	}

	sabHandler := &sabnzbd.Handler{
		SlskdClient: slskdClient,
		Store:       st,
		APIKey:      cfg.APIKey,
		DownloadDir: cfg.DownloadDir,
	}

	mux := http.NewServeMux()
	mux.Handle("/api", newznabHandler)
	mux.Handle("/sabnzbd/api", sabHandler)
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	srv := &http.Server{
		Addr:         cfg.ListenAddr,
		Handler:      mux,
		ReadTimeout:  60 * time.Second,
		WriteTimeout: 120 * time.Second,
	}

	// Start background sync
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go sabHandler.SyncDownloads(ctx)

	// Graceful shutdown
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		slog.Info("shutting down...")
		cancel()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutdownCancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			slog.Error("server shutdown error", "error", err)
		}
	}()

	slog.Info("starting slskrr",
		"addr", cfg.ListenAddr,
		"slskd", cfg.SlskdURL,
		"newznab", baseURL+"/api",
		"sabnzbd", baseURL+"/sabnzbd/api",
	)

	if err := srv.ListenAndServe(); err != http.ErrServerClosed {
		slog.Error("server error", "error", err)
		os.Exit(1)
	}

	slog.Info("slskrr stopped")
}
