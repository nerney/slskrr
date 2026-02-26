package slskd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"time"
)

type Client struct {
	BaseURL    string
	APIKey     string
	HTTPClient *http.Client
}

func NewClient(baseURL, apiKey string) *Client {
	return &Client{
		BaseURL: baseURL,
		APIKey:  apiKey,
		HTTPClient: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

// Search types

type SearchRequest struct {
	SearchText               string `json:"searchText"`
	SearchTimeout            int    `json:"searchTimeout"`            // milliseconds
	FileLimit                int    `json:"fileLimit"`                // max files in results
	FilterResponses          bool   `json:"filterResponses"`          // let slskd pre-filter
	ResponseLimit            int    `json:"responseLimit"`            // max peer responses
	MinimumResponseFileCount int    `json:"minimumResponseFileCount"` // min files per response
	MaximumPeerQueueLength   int    `json:"maximumPeerQueueLength"`   // max peer queue depth
	MinimumPeerUploadSpeed   int    `json:"minimumPeerUploadSpeed"`   // min peer speed (bytes/s)
}

type SearchResult struct {
	ID            string           `json:"id"`
	SearchText    string           `json:"searchText"`
	State         string           `json:"state"`
	IsComplete    bool             `json:"isComplete"`
	ResponseCount int              `json:"responseCount"`
	FileCount     int              `json:"fileCount"`
	Responses     []SearchResponse `json:"responses,omitempty"`
}

type SearchResponse struct {
	Username        string      `json:"username"`
	FileCount       int         `json:"fileCount"`
	Files           []SlskdFile `json:"files"`
	LockedFileCount int         `json:"lockedFileCount"`
	LockedFiles     []SlskdFile `json:"lockedFiles"`
	HasFreeUploadSlot bool     `json:"hasFreeUploadSlot"`
	UploadSpeed     int64       `json:"uploadSpeed"`
	QueueLength     int         `json:"queueLength"`
}

type SlskdFile struct {
	Filename   string `json:"filename"`
	Size       int64  `json:"size"`
	BitRate    int    `json:"bitRate,omitempty"`
	BitDepth   int    `json:"bitDepth,omitempty"`
	SampleRate int    `json:"sampleRate,omitempty"`
	Length     int    `json:"length,omitempty"`
	IsLocked   bool   `json:"isLocked,omitempty"`
	Extension  string `json:"extension,omitempty"`
}

// Download/Transfer types

type DownloadRequest struct {
	Filename string `json:"filename"`
	Size     int64  `json:"size"`
}

type Transfer struct {
	ID               string  `json:"id"`
	Username         string  `json:"username"`
	Direction        string  `json:"direction"`
	Filename         string  `json:"filename"`
	Size             int64   `json:"size"`
	BytesTransferred int64   `json:"bytesTransferred"`
	AverageSpeed     float64 `json:"averageSpeed"`
	State            string  `json:"state"`
}

type UserTransferGroup struct {
	Username    string                   `json:"username"`
	Directories []DirectoryTransferGroup `json:"directories"`
}

type DirectoryTransferGroup struct {
	Directory string     `json:"directory"`
	Files     []Transfer `json:"files"`
}

// Search starts a new search on slskd.
func (c *Client) Search(ctx context.Context, query string, timeout time.Duration) (string, error) {
	req := SearchRequest{
		SearchText:               query,
		SearchTimeout:            int(timeout.Milliseconds()),
		FileLimit:                10000,
		FilterResponses:          true,
		ResponseLimit:            100,
		MinimumResponseFileCount: 1,
		MaximumPeerQueueLength:   1000000,
		MinimumPeerUploadSpeed:   0,
	}

	body, err := json.Marshal(req)
	if err != nil {
		return "", fmt.Errorf("marshal search request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/api/v0/searches", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("create search request: %w", err)
	}
	c.setHeaders(httpReq)

	resp, err := c.HTTPClient.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("execute search request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("search request failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	var result SearchResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode search response: %w", err)
	}

	return result.ID, nil
}

// GetSearch returns the current state of a search.
func (c *Client) GetSearch(ctx context.Context, id string, includeResponses bool) (*SearchResult, error) {
	url := c.BaseURL + "/api/v0/searches/" + id
	if includeResponses {
		url += "?includeResponses=true"
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create get search request: %w", err)
	}
	c.setHeaders(req)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute get search request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("get search failed with status %d", resp.StatusCode)
	}

	var result SearchResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode search result: %w", err)
	}

	return &result, nil
}

// DeleteSearch removes a completed search.
func (c *Client) DeleteSearch(ctx context.Context, id string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.BaseURL+"/api/v0/searches/"+id, nil)
	if err != nil {
		return fmt.Errorf("create delete search request: %w", err)
	}
	c.setHeaders(req)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("execute delete search request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("delete search failed with status %d", resp.StatusCode)
	}

	return nil
}

// SearchAndWait starts a search and polls until complete or timeout.
// It sends searchTimeout to slskd as 80% of the polling timeout so slskd
// finishes before we give up, and uses adaptive polling that speeds up
// as results stream in.
func (c *Client) SearchAndWait(ctx context.Context, query string, timeout time.Duration) ([]SearchResponse, error) {
	// Tell slskd to stop searching at 80% of our timeout so it completes
	// before our polling deadline.
	slskdTimeout := time.Duration(float64(timeout) * 0.8)
	searchID, err := c.Search(ctx, query, slskdTimeout)
	if err != nil {
		return nil, err
	}

	deadline := time.After(timeout)
	// Start with a 2-second initial delay before first poll
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()

	const fileLimit = 10000 // matches the fileLimit sent in Search

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-deadline:
			slog.Warn("search timeout reached, returning partial results", "id", searchID, "query", query)
			result, err := c.GetSearch(ctx, searchID, true)
			go func() {
				_ = c.DeleteSearch(context.Background(), searchID)
			}()
			if err != nil {
				return nil, fmt.Errorf("get final search responses: %w", err)
			}
			slog.Info("search partial results", "id", searchID, "responses", len(result.Responses), "totalFiles", countFiles(result.Responses))
			return result.Responses, nil
		case <-timer.C:
			result, err := c.GetSearch(ctx, searchID, false)
			if err != nil {
				return nil, err
			}
			slog.Debug("search poll", "id", searchID, "state", result.State, "isComplete", result.IsComplete, "responseCount", result.ResponseCount, "fileCount", result.FileCount)

			if result.IsComplete {
				// Fetch final results with responses included in one call
				full, err := c.GetSearch(ctx, searchID, true)
				go func() {
					_ = c.DeleteSearch(context.Background(), searchID)
				}()
				if err != nil {
					return nil, fmt.Errorf("get search responses: %w", err)
				}
				slog.Info("search completed", "id", searchID, "state", result.State, "responses", len(full.Responses), "totalFiles", countFiles(full.Responses))
				return full.Responses, nil
			}

			// Adaptive delay: U-shaped curve — slow at start/end, fast in the middle
			progress := math.Min(float64(result.FileCount)/float64(fileLimit), 1.0)
			delay := adaptiveDelay(progress)
			timer.Reset(delay)
		}
	}
}

// adaptiveDelay returns a polling interval based on search progress.
// Uses a quadratic curve: slow at 0% (5s), fastest at ~50% (1s), slow at 100% (5s).
func adaptiveDelay(progress float64) time.Duration {
	const (
		a = 16.0
		b = -16.0
		c = 5.0
	)
	seconds := a*math.Pow(progress, 2) + b*progress + c
	seconds = math.Max(0.5, math.Min(5.0, seconds))
	return time.Duration(seconds * float64(time.Second))
}

// Download queues files for download from a specific user.
func (c *Client) Download(ctx context.Context, username string, files []DownloadRequest) error {
	body, err := json.Marshal(files)
	if err != nil {
		return fmt.Errorf("marshal download request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/api/v0/transfers/downloads/"+username, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create download request: %w", err)
	}
	c.setHeaders(req)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("execute download request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusNoContent {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("download request failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}

// CancelDownload cancels an active transfer then removes the record.
func (c *Client) CancelDownload(ctx context.Context, username, id string) error {
	// Phase 1: cancel the active transfer
	cancelURL := fmt.Sprintf("%s/api/v0/transfers/downloads/%s/%s", c.BaseURL, username, id)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, cancelURL, nil)
	if err != nil {
		return fmt.Errorf("create cancel request: %w", err)
	}
	c.setHeaders(req)
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("execute cancel request: %w", err)
	}
	resp.Body.Close()

	// Brief pause for slskd to process the cancellation
	time.Sleep(500 * time.Millisecond)

	// Phase 2: remove the transfer record
	removeURL := cancelURL + "?remove=true"
	req, err = http.NewRequestWithContext(ctx, http.MethodDelete, removeURL, nil)
	if err != nil {
		return fmt.Errorf("create remove request: %w", err)
	}
	c.setHeaders(req)
	resp, err = c.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("execute remove request: %w", err)
	}
	resp.Body.Close()

	return nil
}

// GetAllDownloads returns all current download transfers.
func (c *Client) GetAllDownloads(ctx context.Context) ([]UserTransferGroup, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/api/v0/transfers/downloads", nil)
	if err != nil {
		return nil, fmt.Errorf("create get downloads request: %w", err)
	}
	c.setHeaders(req)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute get downloads request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("get downloads failed with status %d", resp.StatusCode)
	}

	var groups []UserTransferGroup
	if err := json.NewDecoder(resp.Body).Decode(&groups); err != nil {
		return nil, fmt.Errorf("decode downloads response: %w", err)
	}

	return groups, nil
}

// GetOptions returns slskd's runtime configuration.
func (c *Client) GetOptions(ctx context.Context) (map[string]any, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/api/v0/options", nil)
	if err != nil {
		return nil, fmt.Errorf("create get options request: %w", err)
	}
	c.setHeaders(req)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute get options request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("get options failed with status %d", resp.StatusCode)
	}

	var opts map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&opts); err != nil {
		return nil, fmt.Errorf("decode options response: %w", err)
	}

	return opts, nil
}

// GetDownloadDir fetches slskd's configured download directory from the options API.
func (c *Client) GetDownloadDir(ctx context.Context) (string, error) {
	opts, err := c.GetOptions(ctx)
	if err != nil {
		return "", err
	}

	dirs, ok := opts["directories"].(map[string]any)
	if !ok {
		return "", fmt.Errorf("directories not found in options")
	}
	downloads, ok := dirs["downloads"].(string)
	if !ok {
		return "", fmt.Errorf("downloads directory not found in options")
	}
	return downloads, nil
}

func (c *Client) setHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", c.APIKey)
}

func countFiles(responses []SearchResponse) int {
	n := 0
	for _, r := range responses {
		n += len(r.Files) + len(r.LockedFiles)
	}
	return n
}

// MapTransferState maps slskd's compound transfer state strings to a simple status.
func MapTransferState(state string) string {
	switch state {
	case "Completed, Succeeded":
		return "completed"
	case "Completed, Cancelled",
		"Completed, TimedOut",
		"Completed, Errored",
		"Completed, Rejected":
		return "failed"
	case "InProgress":
		return "downloading"
	case "Requested",
		"Queued, Locally",
		"Queued, Remotely",
		"Initializing":
		return "queued"
	default:
		// Fallback for unknown states
		if len(state) >= 9 && state[:9] == "Completed" {
			return "failed"
		}
		return "queued"
	}
}
