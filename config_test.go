package main

import (
	"os"
	"testing"
	"time"
)

func TestLoadConfig_RequiredFields(t *testing.T) {
	// Clear all env vars
	os.Unsetenv("SLSKD_URL")
	os.Unsetenv("SLSKD_API_KEY")

	_, err := LoadConfig()
	if err == nil {
		t.Fatal("expected error when SLSKD_URL is missing")
	}

	os.Setenv("SLSKD_URL", "http://localhost:5030")
	_, err = LoadConfig()
	if err == nil {
		t.Fatal("expected error when SLSKD_API_KEY is missing")
	}
	os.Unsetenv("SLSKD_URL")
}

func TestLoadConfig_Defaults(t *testing.T) {
	os.Setenv("SLSKD_URL", "http://localhost:5030")
	os.Setenv("SLSKD_API_KEY", "testkey")
	os.Unsetenv("LISTEN_ADDR")
	os.Unsetenv("API_KEY")
	os.Unsetenv("SEARCH_TIMEOUT")
	os.Unsetenv("DOWNLOAD_DIR")
	defer func() {
		os.Unsetenv("SLSKD_URL")
		os.Unsetenv("SLSKD_API_KEY")
	}()

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.ListenAddr != ":6969" {
		t.Errorf("expected default listen addr :6969, got %s", cfg.ListenAddr)
	}
	if cfg.SearchTimeout != 30*time.Second {
		t.Errorf("expected default search timeout 30s, got %v", cfg.SearchTimeout)
	}
	if cfg.DownloadDir != "/downloads/complete" {
		t.Errorf("expected default download dir, got %s", cfg.DownloadDir)
	}
	if cfg.APIKey != "" {
		t.Errorf("expected empty API key, got %s", cfg.APIKey)
	}
}

func TestLoadConfig_CustomValues(t *testing.T) {
	os.Setenv("SLSKD_URL", "http://myhost:5030")
	os.Setenv("SLSKD_API_KEY", "mykey")
	os.Setenv("LISTEN_ADDR", ":8080")
	os.Setenv("API_KEY", "radarrkey")
	os.Setenv("SEARCH_TIMEOUT", "1m")
	os.Setenv("DOWNLOAD_DIR", "/data/downloads")
	defer func() {
		os.Unsetenv("SLSKD_URL")
		os.Unsetenv("SLSKD_API_KEY")
		os.Unsetenv("LISTEN_ADDR")
		os.Unsetenv("API_KEY")
		os.Unsetenv("SEARCH_TIMEOUT")
		os.Unsetenv("DOWNLOAD_DIR")
	}()

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.SlskdURL != "http://myhost:5030" {
		t.Errorf("got %s", cfg.SlskdURL)
	}
	if cfg.SlskdAPIKey != "mykey" {
		t.Errorf("got %s", cfg.SlskdAPIKey)
	}
	if cfg.ListenAddr != ":8080" {
		t.Errorf("got %s", cfg.ListenAddr)
	}
	if cfg.APIKey != "radarrkey" {
		t.Errorf("got %s", cfg.APIKey)
	}
	if cfg.SearchTimeout != time.Minute {
		t.Errorf("got %v", cfg.SearchTimeout)
	}
	if cfg.DownloadDir != "/data/downloads" {
		t.Errorf("got %s", cfg.DownloadDir)
	}
}

func TestLoadConfig_InvalidTimeout(t *testing.T) {
	os.Setenv("SLSKD_URL", "http://localhost:5030")
	os.Setenv("SLSKD_API_KEY", "key")
	os.Setenv("SEARCH_TIMEOUT", "notaduration")
	defer func() {
		os.Unsetenv("SLSKD_URL")
		os.Unsetenv("SLSKD_API_KEY")
		os.Unsetenv("SEARCH_TIMEOUT")
	}()

	_, err := LoadConfig()
	if err == nil {
		t.Fatal("expected error for invalid SEARCH_TIMEOUT")
	}
}
