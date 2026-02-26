package main

import (
	"fmt"
	"os"
	"time"
)

type Config struct {
	SlskdURL      string
	SlskdAPIKey   string
	ListenAddr    string
	APIKey        string
	SearchTimeout time.Duration
	DownloadDir   string
}

func LoadConfig() (*Config, error) {
	cfg := &Config{
		SlskdURL:      os.Getenv("SLSKD_URL"),
		SlskdAPIKey:   os.Getenv("SLSKD_API_KEY"),
		ListenAddr:    os.Getenv("LISTEN_ADDR"),
		APIKey:        os.Getenv("API_KEY"),
		DownloadDir:   os.Getenv("DOWNLOAD_DIR"),
	}

	if cfg.SlskdURL == "" {
		return nil, fmt.Errorf("SLSKD_URL is required")
	}
	if cfg.SlskdAPIKey == "" {
		return nil, fmt.Errorf("SLSKD_API_KEY is required")
	}
	if cfg.ListenAddr == "" {
		cfg.ListenAddr = ":6969"
	}
	if cfg.DownloadDir == "" {
		cfg.DownloadDir = "/downloads/complete"
	}

	timeout := os.Getenv("SEARCH_TIMEOUT")
	if timeout == "" {
		cfg.SearchTimeout = 30 * time.Second
	} else {
		d, err := time.ParseDuration(timeout)
		if err != nil {
			return nil, fmt.Errorf("invalid SEARCH_TIMEOUT: %w", err)
		}
		cfg.SearchTimeout = d
	}

	return cfg, nil
}
