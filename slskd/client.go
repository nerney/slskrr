package slskd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
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
			Timeout: 30 * time.Second,
		},
	}
}

// Search types

type SearchRequest struct {
	SearchText string `json:"searchText"`
}

type SearchResult struct {
	ID             string `json:"id"`
	SearchText     string `json:"searchText"`
	State          string `json:"state"`
	IsComplete     bool   `json:"isComplete"`
	ResponseCount  int    `json:"responseCount"`
	FileCount      int    `json:"fileCount"`
}

type SearchResponse struct {
	Username        string      `json:"username"`
	FileCount       int         `json:"fileCount"`
	Files           []SlskdFile `json:"files"`
	LockedFileCount int         `json:"lockedFileCount"`
	LockedFiles     []SlskdFile `json:"lockedFiles"`
}

type SlskdFile struct {
	Filename  string `json:"filename"`
	Size      int64  `json:"size"`
	BitRate   int    `json:"bitRate,omitempty"`
	BitDepth  int    `json:"bitDepth,omitempty"`
	IsLocked  bool   `json:"isLocked,omitempty"`
}

// Download/Transfer types

type DownloadRequest struct {
	Filename string `json:"filename"`
	Size     int64  `json:"size"`
}

type Transfer struct {
	Username         string  `json:"username"`
	Direction        string  `json:"direction"`
	Filename         string  `json:"filename"`
	Size             int64   `json:"size"`
	BytesTransferred int64   `json:"bytesTransferred"`
	AverageSpeed     float64 `json:"averageSpeed"`
	State            string  `json:"state"`
}

type UserTransferGroup struct {
	Username   string      `json:"username"`
	Directories []DirectoryTransferGroup `json:"directories"`
}

type DirectoryTransferGroup struct {
	Directory string     `json:"directory"`
	Files     []Transfer `json:"files"`
}

// Search starts a new search on slskd.
func (c *Client) Search(ctx context.Context, query string) (string, error) {
	body, err := json.Marshal(SearchRequest{SearchText: query})
	if err != nil {
		return "", fmt.Errorf("marshal search request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/api/v0/searches", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("create search request: %w", err)
	}
	c.setHeaders(req)

	resp, err := c.HTTPClient.Do(req)
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
func (c *Client) GetSearch(ctx context.Context, id string) (*SearchResult, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/api/v0/searches/"+id, nil)
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

// GetSearchResponses returns the file responses for a search.
func (c *Client) GetSearchResponses(ctx context.Context, id string) ([]SearchResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/api/v0/searches/"+id+"/responses", nil)
	if err != nil {
		return nil, fmt.Errorf("create get responses request: %w", err)
	}
	c.setHeaders(req)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute get responses request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("get search responses failed with status %d", resp.StatusCode)
	}

	var responses []SearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&responses); err != nil {
		return nil, fmt.Errorf("decode search responses: %w", err)
	}

	return responses, nil
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
func (c *Client) SearchAndWait(ctx context.Context, query string, timeout time.Duration) ([]SearchResponse, error) {
	searchID, err := c.Search(ctx, query)
	if err != nil {
		return nil, err
	}

	deadline := time.After(timeout)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-deadline:
			slog.Warn("search timeout reached, returning partial results", "id", searchID, "query", query)
			// Timeout reached — return whatever we have
			responses, err := c.GetSearchResponses(ctx, searchID)
			// Clean up the search in the background
			go func() {
				_ = c.DeleteSearch(context.Background(), searchID)
			}()
			if err != nil {
				return nil, fmt.Errorf("get final search responses: %w", err)
			}
			slog.Info("search partial results", "id", searchID, "responses", len(responses), "totalFiles", countFiles(responses))
			return responses, nil
		case <-ticker.C:
			result, err := c.GetSearch(ctx, searchID)
			if err != nil {
				return nil, err
			}
			slog.Debug("search poll", "id", searchID, "state", result.State, "isComplete", result.IsComplete, "responseCount", result.ResponseCount, "fileCount", result.FileCount)
			if result.IsComplete {
				responses, err := c.GetSearchResponses(ctx, searchID)
				go func() {
					_ = c.DeleteSearch(context.Background(), searchID)
				}()
				if err != nil {
					return nil, fmt.Errorf("get search responses: %w", err)
				}
				slog.Info("search completed", "id", searchID, "state", result.State, "responses", len(responses), "totalFiles", countFiles(responses))
				return responses, nil
			}
		}
	}
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
