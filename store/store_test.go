package store

import (
	"strings"
	"sync"
	"testing"
)

func TestStore_AddAndGet(t *testing.T) {
	s := New()

	id := s.Add("user1", "path/to/file.mkv", 1000, "radarr")
	if id == "" {
		t.Fatal("expected non-empty ID")
	}
	if !strings.HasPrefix(id, "SABnzbd_nzo_") {
		t.Errorf("ID should have SABnzbd_nzo_ prefix, got %s", id)
	}

	dl := s.Get(id)
	if dl == nil {
		t.Fatal("expected download, got nil")
	}
	if dl.Username != "user1" {
		t.Errorf("expected username user1, got %s", dl.Username)
	}
	if dl.Filename != "path/to/file.mkv" {
		t.Errorf("expected filename path/to/file.mkv, got %s", dl.Filename)
	}
	if dl.Size != 1000 {
		t.Errorf("expected size 1000, got %d", dl.Size)
	}
	if dl.Category != "radarr" {
		t.Errorf("expected category radarr, got %s", dl.Category)
	}
	if dl.Status != StatusQueued {
		t.Errorf("expected status Queued, got %s", dl.Status)
	}
}

func TestStore_GetNonExistent(t *testing.T) {
	s := New()
	dl := s.Get("nonexistent")
	if dl != nil {
		t.Fatal("expected nil for non-existent download")
	}
}

func TestStore_QueueAndHistory(t *testing.T) {
	s := New()

	id1 := s.Add("user1", "file1.mkv", 100, "radarr")
	id2 := s.Add("user2", "file2.mkv", 200, "sonarr")
	id3 := s.Add("user3", "file3.mkv", 300, "radarr")

	// All should be in queue initially
	queue := s.Queue()
	if len(queue) != 3 {
		t.Errorf("expected 3 in queue, got %d", len(queue))
	}

	history := s.History()
	if len(history) != 0 {
		t.Errorf("expected 0 in history, got %d", len(history))
	}

	// Complete one, fail another
	s.UpdateTransfer(id1, 100, StatusCompleted)
	s.UpdateTransfer(id2, 50, StatusFailed)

	queue = s.Queue()
	if len(queue) != 1 {
		t.Errorf("expected 1 in queue, got %d", len(queue))
	}
	if queue[0].ID != id3 {
		t.Errorf("expected %s in queue, got %s", id3, queue[0].ID)
	}

	history = s.History()
	if len(history) != 2 {
		t.Errorf("expected 2 in history, got %d", len(history))
	}
}

func TestStore_UpdateTransfer(t *testing.T) {
	s := New()
	id := s.Add("user1", "file.mkv", 1000, "radarr")

	s.UpdateTransfer(id, 500, StatusDownloading)
	dl := s.Get(id)
	if dl.BytesDownloaded != 500 {
		t.Errorf("expected 500 bytes downloaded, got %d", dl.BytesDownloaded)
	}
	if dl.Status != StatusDownloading {
		t.Errorf("expected Downloading, got %s", dl.Status)
	}
	if dl.CompletedAt.IsZero() == false {
		t.Error("should not have completed time while downloading")
	}

	s.UpdateTransfer(id, 1000, StatusCompleted)
	dl = s.Get(id)
	if dl.CompletedAt.IsZero() {
		t.Error("should have completed time after completion")
	}
}

func TestStore_Remove(t *testing.T) {
	s := New()
	id := s.Add("user1", "file.mkv", 1000, "radarr")

	s.Remove(id)
	dl := s.Get(id)
	if dl != nil {
		t.Fatal("expected nil after remove")
	}
}

func TestStore_FindByFile(t *testing.T) {
	s := New()
	s.Add("user1", "path/to/file.mkv", 1000, "radarr")
	s.Add("user2", "other/file.mp4", 2000, "sonarr")

	dl := s.FindByFile("user1", "path/to/file.mkv")
	if dl == nil {
		t.Fatal("expected to find download")
	}
	if dl.Username != "user1" {
		t.Errorf("wrong username: %s", dl.Username)
	}

	dl = s.FindByFile("user1", "nonexistent.mkv")
	if dl != nil {
		t.Fatal("expected nil for non-matching filename")
	}
}

func TestStore_Progress(t *testing.T) {
	s := New()
	id := s.Add("user1", "file.mkv", 1000, "radarr")

	dl := s.Get(id)
	if dl.Progress() != 0 {
		t.Errorf("expected 0 progress, got %f", dl.Progress())
	}

	s.UpdateTransfer(id, 500, StatusDownloading)
	dl = s.Get(id)
	if dl.Progress() != 50 {
		t.Errorf("expected 50 progress, got %f", dl.Progress())
	}

	s.UpdateTransfer(id, 1000, StatusCompleted)
	dl = s.Get(id)
	if dl.Progress() != 100 {
		t.Errorf("expected 100 progress, got %f", dl.Progress())
	}
}

func TestStore_ConcurrentAccess(t *testing.T) {
	s := New()
	var wg sync.WaitGroup

	// Concurrent adds
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			s.Add("user", "file.mkv", int64(n), "radarr")
		}(i)
	}
	wg.Wait()

	all := s.All()
	if len(all) != 100 {
		t.Errorf("expected 100 downloads, got %d", len(all))
	}
}
