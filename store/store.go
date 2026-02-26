package store

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"
)

type Status string

const (
	StatusQueued      Status = "Queued"
	StatusDownloading Status = "Downloading"
	StatusCompleted   Status = "Completed"
	StatusFailed      Status = "Failed"
)

type Download struct {
	ID              string
	Username        string
	Filename        string
	Size            int64
	BytesDownloaded int64
	Category        string
	Status          Status
	AddedAt         time.Time
	CompletedAt     time.Time
	Retries         int
	MaxRetries      int
	TransferID      string // slskd transfer ID for cancellation
}

func (d *Download) Progress() float64 {
	if d.Size == 0 {
		return 0
	}
	return float64(d.BytesDownloaded) / float64(d.Size) * 100
}

type Store struct {
	mu        sync.RWMutex
	downloads map[string]*Download
}

func New() *Store {
	return &Store{
		downloads: make(map[string]*Download),
	}
}

func generateID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return fmt.Sprintf("SABnzbd_nzo_%s", hex.EncodeToString(b))
}

// Add creates a new download entry and returns its ID.
func (s *Store) Add(username, filename string, size int64, category string) string {
	s.mu.Lock()
	defer s.mu.Unlock()

	id := generateID()
	s.downloads[id] = &Download{
		ID:         id,
		Username:   username,
		Filename:   filename,
		Size:       size,
		Category:   category,
		Status:     StatusQueued,
		AddedAt:    time.Now(),
		MaxRetries: 3,
	}
	return id
}

// Get returns a download by ID.
func (s *Store) Get(id string) *Download {
	s.mu.RLock()
	defer s.mu.RUnlock()
	dl := s.downloads[id]
	if dl == nil {
		return nil
	}
	cp := *dl
	return &cp
}

// UpdateTransfer updates download progress from slskd transfer data.
func (s *Store) UpdateTransfer(id string, bytesDownloaded int64, status Status) {
	s.mu.Lock()
	defer s.mu.Unlock()

	dl, ok := s.downloads[id]
	if !ok {
		return
	}
	dl.BytesDownloaded = bytesDownloaded
	dl.Status = status
	if (status == StatusCompleted || status == StatusFailed) && dl.CompletedAt.IsZero() {
		dl.CompletedAt = time.Now()
	}
}

// IncrementRetry bumps the retry count and resets status to Queued for re-download.
// Returns true if a retry is allowed, false if max retries exceeded.
func (s *Store) IncrementRetry(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	dl, ok := s.downloads[id]
	if !ok {
		return false
	}
	dl.Retries++
	if dl.Retries > dl.MaxRetries {
		return false
	}
	dl.Status = StatusQueued
	dl.BytesDownloaded = 0
	dl.CompletedAt = time.Time{}
	return true
}

// SetTransferID stores the slskd transfer ID for a download.
func (s *Store) SetTransferID(id, transferID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if dl, ok := s.downloads[id]; ok {
		dl.TransferID = transferID
	}
}

// Remove deletes a download entry.
func (s *Store) Remove(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.downloads, id)
}

// Queue returns all downloads that are queued or downloading.
func (s *Store) Queue() []*Download {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []*Download
	for _, dl := range s.downloads {
		if dl.Status == StatusQueued || dl.Status == StatusDownloading {
			cp := *dl
			result = append(result, &cp)
		}
	}
	return result
}

// History returns all completed or failed downloads.
func (s *Store) History() []*Download {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []*Download
	for _, dl := range s.downloads {
		if dl.Status == StatusCompleted || dl.Status == StatusFailed {
			cp := *dl
			result = append(result, &cp)
		}
	}
	return result
}

// All returns all downloads.
func (s *Store) All() []*Download {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []*Download
	for _, dl := range s.downloads {
		cp := *dl
		result = append(result, &cp)
	}
	return result
}

// FindByFile looks up a download by username and filename.
func (s *Store) FindByFile(username, filename string) *Download {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, dl := range s.downloads {
		if dl.Username == username && dl.Filename == filename {
			cp := *dl
			return &cp
		}
	}
	return nil
}
