package sabnzbd

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/nerney/slskrr/newznab"
	"github.com/nerney/slskrr/slskd"
	"github.com/nerney/slskrr/store"
)

// Handler serves the SABnzbd API facade.
type Handler struct {
	SlskdClient *slskd.Client
	Store       *store.Store
	APIKey      string
	DownloadDir string
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	mode := q.Get("mode")

	switch mode {
	case "version":
		h.handleVersion(w)
	case "auth":
		h.handleAuth(w, r)
	case "get_config":
		h.handleGetConfig(w, r)
	case "get_cats":
		h.handleGetCats(w, r)
	case "addurl":
		h.handleAddURL(w, r)
	case "queue":
		h.handleQueue(w, r)
	case "history":
		h.handleHistory(w, r)
	default:
		writeJSON(w, map[string]any{"status": false, "error": "Unknown mode: " + mode})
	}
}

func (h *Handler) checkAPIKey(r *http.Request) bool {
	if h.APIKey == "" {
		return true
	}
	key := r.URL.Query().Get("apikey")
	return subtle.ConstantTimeCompare([]byte(key), []byte(h.APIKey)) == 1
}

func (h *Handler) handleVersion(w http.ResponseWriter) {
	writeJSON(w, map[string]string{"version": "4.0.0"})
}

func (h *Handler) handleAuth(w http.ResponseWriter, r *http.Request) {
	if h.checkAPIKey(r) {
		writeJSON(w, map[string]any{"auth": "apikey", "status": true})
	} else {
		writeJSON(w, map[string]any{"auth": "apikey", "status": false})
	}
}

func (h *Handler) handleGetConfig(w http.ResponseWriter, r *http.Request) {
	if !h.checkAPIKey(r) {
		writeJSON(w, map[string]any{"status": false, "error": "API Key Incorrect"})
		return
	}

	writeJSON(w, map[string]any{
		"config": map[string]any{
			"misc": map[string]any{
				"complete_dir":      h.DownloadDir,
				"history_retention": "all",
			},
			"categories": []map[string]string{
				{"name": "Default", "dir": ""},
				{"name": "radarr", "dir": "radarr"},
				{"name": "sonarr-tv", "dir": "sonarr-tv"},
				{"name": "tv-sonarr", "dir": "tv-sonarr"},
				{"name": "sonarr", "dir": "sonarr"},
			},
		},
	})
}

func (h *Handler) handleGetCats(w http.ResponseWriter, r *http.Request) {
	if !h.checkAPIKey(r) {
		writeJSON(w, map[string]any{"status": false, "error": "API Key Incorrect"})
		return
	}
	writeJSON(w, map[string]any{
		"categories": []string{"Default", "radarr", "sonarr-tv", "tv-sonarr", "sonarr"},
	})
}

func (h *Handler) handleAddURL(w http.ResponseWriter, r *http.Request) {
	if !h.checkAPIKey(r) {
		writeJSON(w, map[string]any{"status": false, "error": "API Key Incorrect"})
		return
	}

	q := r.URL.Query()
	nzbURL := q.Get("name")
	category := q.Get("cat")

	if nzbURL == "" {
		writeJSON(w, map[string]any{"status": false, "error": "Missing name parameter"})
		return
	}

	// Parse the URL to extract the token directly instead of HTTP loopback
	token, err := extractTokenFromURL(nzbURL)
	if err != nil {
		slog.Error("failed to extract token from URL", "url", nzbURL, "error", err)
		writeJSON(w, map[string]any{"status": false, "error": "Invalid NZB URL"})
		return
	}

	fileToken, err := newznab.DecodeToken(token)
	if err != nil {
		slog.Error("failed to decode token", "error", err)
		writeJSON(w, map[string]any{"status": false, "error": "Invalid token"})
		return
	}

	slog.Info("queueing download",
		"username", fileToken.Username,
		"filename", fileToken.Filename,
		"size", fileToken.Size,
		"category", category,
	)

	// Queue the download in slskd
	err = h.SlskdClient.Download(r.Context(), fileToken.Username, []slskd.DownloadRequest{
		{Filename: fileToken.Filename, Size: fileToken.Size},
	})
	if err != nil {
		slog.Error("slskd download failed", "error", err)
		writeJSON(w, map[string]any{"status": false, "error": "Failed to queue download"})
		return
	}

	// Track in our store
	id := h.Store.Add(fileToken.Username, fileToken.Filename, fileToken.Size, category)

	slog.Info("download queued", "id", id, "filename", fileToken.Filename)

	writeJSON(w, map[string]any{
		"status":  true,
		"nzo_ids": []string{id},
	})
}

func (h *Handler) handleQueue(w http.ResponseWriter, r *http.Request) {
	if !h.checkAPIKey(r) {
		writeJSON(w, map[string]any{"status": false, "error": "API Key Incorrect"})
		return
	}

	q := r.URL.Query()

	// Handle delete sub-command
	if q.Get("name") == "delete" {
		h.handleQueueDelete(w, r)
		return
	}

	queue := h.Store.Queue()
	slots := make([]map[string]any, 0, len(queue))

	for _, dl := range queue {
		basename := path.Base(strings.ReplaceAll(dl.Filename, "\\", "/"))
		mb := float64(dl.Size) / (1024 * 1024)
		mbLeft := mb - (mb * dl.Progress() / 100)
		pct := fmt.Sprintf("%.0f", dl.Progress())

		timeleft := "00:00:00"
		if dl.Status == store.StatusDownloading && dl.Progress() > 0 {
			elapsed := time.Since(dl.AddedAt).Seconds()
			rate := float64(dl.BytesDownloaded) / elapsed
			if rate > 0 {
				remaining := float64(dl.Size-dl.BytesDownloaded) / rate
				h := int(remaining) / 3600
				m := (int(remaining) % 3600) / 60
				s := int(remaining) % 60
				timeleft = fmt.Sprintf("%02d:%02d:%02d", h, m, s)
			}
		}

		slots = append(slots, map[string]any{
			"nzo_id":     dl.ID,
			"filename":   basename,
			"mb":         fmt.Sprintf("%.2f", mb),
			"mbleft":     fmt.Sprintf("%.2f", mbLeft),
			"percentage": pct,
			"status":     string(dl.Status),
			"timeleft":   timeleft,
			"cat":        dl.Category,
			"eta":        "unknown",
			"priority":   "Normal",
		})
	}

	writeJSON(w, map[string]any{
		"queue": map[string]any{
			"paused":            false,
			"slots":             slots,
			"speed":             "0",
			"size":              "0",
			"noofslots_total":   len(slots),
			"status":            "Downloading",
			"diskspacetotal1":   "100.0",
			"diskspace1":        "50.0",
		},
	})
}

func (h *Handler) handleQueueDelete(w http.ResponseWriter, r *http.Request) {
	value := r.URL.Query().Get("value")
	if value == "" {
		writeJSON(w, map[string]any{"status": false, "error": "Missing value"})
		return
	}

	h.Store.Remove(value)
	slog.Info("removed from queue", "id", value)
	writeJSON(w, map[string]any{"status": true, "nzo_ids": []string{value}})
}

func (h *Handler) handleHistory(w http.ResponseWriter, r *http.Request) {
	if !h.checkAPIKey(r) {
		writeJSON(w, map[string]any{"status": false, "error": "API Key Incorrect"})
		return
	}

	q := r.URL.Query()

	// Handle delete sub-command
	if q.Get("name") == "delete" {
		h.handleHistoryDelete(w, r)
		return
	}

	history := h.Store.History()
	slots := make([]map[string]any, 0, len(history))

	for _, dl := range history {
		basename := path.Base(strings.ReplaceAll(dl.Filename, "\\", "/"))
		status := "Completed"
		if dl.Status == store.StatusFailed {
			status = "Failed"
		}

		storagePath := h.DownloadDir
		if dl.Category != "" {
			storagePath = path.Join(storagePath, dl.Category)
		}
		storagePath = path.Join(storagePath, basename)

		downloadTime := int64(0)
		if !dl.CompletedAt.IsZero() {
			downloadTime = int64(math.Max(1, dl.CompletedAt.Sub(dl.AddedAt).Seconds()))
		}

		completedTS := int64(0)
		if !dl.CompletedAt.IsZero() {
			completedTS = dl.CompletedAt.Unix()
		}

		slots = append(slots, map[string]any{
			"nzo_id":        dl.ID,
			"name":          basename,
			"nzb_name":      basename + ".nzb",
			"status":        status,
			"storage":       storagePath,
			"category":      dl.Category,
			"bytes":         dl.Size,
			"download_time": downloadTime,
			"completed":     completedTS,
			"action_line":   "",
			"fail_message":  "",
			"script_line":   "",
			"loaded":        true,
		})
	}

	writeJSON(w, map[string]any{
		"history": map[string]any{
			"slots":           slots,
			"noofslots":       len(slots),
			"last_history_update": time.Now().Unix(),
		},
	})
}

func (h *Handler) handleHistoryDelete(w http.ResponseWriter, r *http.Request) {
	value := r.URL.Query().Get("value")
	if value == "" {
		writeJSON(w, map[string]any{"status": false, "error": "Missing value"})
		return
	}

	h.Store.Remove(value)
	slog.Info("removed from history", "id", value)
	writeJSON(w, map[string]any{"status": true, "nzo_ids": []string{value}})
}

// SyncDownloads polls slskd for transfer status and updates the store.
func (h *Handler) SyncDownloads(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			h.syncOnce(ctx)
		}
	}
}

func (h *Handler) syncOnce(ctx context.Context) {
	groups, err := h.SlskdClient.GetAllDownloads(ctx)
	if err != nil {
		slog.Error("failed to get slskd downloads", "error", err)
		return
	}

	// Build a map of username+filename → transfer state for quick lookup
	type transferKey struct {
		username string
		filename string
	}
	transfers := make(map[transferKey]*slskd.Transfer)
	for i := range groups {
		for j := range groups[i].Directories {
			for k := range groups[i].Directories[j].Files {
				t := &groups[i].Directories[j].Files[k]
				key := transferKey{username: groups[i].Username, filename: t.Filename}
				transfers[key] = t
			}
		}
	}

	// Update our tracked downloads
	for _, dl := range h.Store.All() {
		if dl.Status == store.StatusCompleted || dl.Status == store.StatusFailed {
			continue
		}

		key := transferKey{username: dl.Username, filename: dl.Filename}
		t, ok := transfers[key]
		if !ok {
			continue
		}

		var newStatus store.Status
		switch {
		case strings.Contains(t.State, "Completed") && strings.Contains(t.State, "Succeeded"):
			newStatus = store.StatusCompleted
		case strings.Contains(t.State, "Completed"):
			// Completed but with error/cancelled/timed out
			newStatus = store.StatusFailed
		case strings.Contains(t.State, "InProgress"):
			newStatus = store.StatusDownloading
		default:
			newStatus = store.StatusQueued
		}

		h.Store.UpdateTransfer(dl.ID, t.BytesTransferred, newStatus)
	}
}

func extractTokenFromURL(rawURL string) (string, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("parse URL: %w", err)
	}
	token := parsed.Query().Get("id")
	if token == "" {
		return "", fmt.Errorf("no id parameter in URL")
	}
	return token, nil
}

func writeJSON(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(data); err != nil {
		slog.Error("failed to write JSON response", "error", err)
	}
}
