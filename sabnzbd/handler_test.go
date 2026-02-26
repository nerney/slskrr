package sabnzbd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/nerney/slskrr/newznab"
	"github.com/nerney/slskrr/slskd"
	"github.com/nerney/slskrr/store"
)

func newTestHandler(slskdURL string) *Handler {
	return &Handler{
		SlskdClient: slskd.NewClient(slskdURL, "testkey"),
		Store:       store.New(),
		APIKey:      "testapikey",
		DownloadDir: "/downloads/complete",
	}
}

func TestHandler_Version(t *testing.T) {
	h := newTestHandler("")

	req := httptest.NewRequest("GET", "/sabnzbd/api?mode=version", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	var resp map[string]string
	json.NewDecoder(rec.Body).Decode(&resp)

	if resp["version"] != "4.0.0" {
		t.Errorf("expected version 4.0.0, got %s", resp["version"])
	}
}

func TestHandler_Auth_ValidKey(t *testing.T) {
	h := newTestHandler("")

	req := httptest.NewRequest("GET", "/sabnzbd/api?mode=auth&apikey=testapikey", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	var resp map[string]any
	json.NewDecoder(rec.Body).Decode(&resp)

	if resp["status"] != true {
		t.Error("expected status true for valid key")
	}
}

func TestHandler_Auth_InvalidKey(t *testing.T) {
	h := newTestHandler("")

	req := httptest.NewRequest("GET", "/sabnzbd/api?mode=auth&apikey=wrongkey", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	var resp map[string]any
	json.NewDecoder(rec.Body).Decode(&resp)

	if resp["status"] != false {
		t.Error("expected status false for invalid key")
	}
}

func TestHandler_GetConfig(t *testing.T) {
	h := newTestHandler("")

	req := httptest.NewRequest("GET", "/sabnzbd/api?mode=get_config&apikey=testapikey", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	var resp map[string]any
	json.NewDecoder(rec.Body).Decode(&resp)

	config, ok := resp["config"].(map[string]any)
	if !ok {
		t.Fatal("expected config object")
	}
	misc, ok := config["misc"].(map[string]any)
	if !ok {
		t.Fatal("expected misc config")
	}
	if misc["complete_dir"] != "/downloads/complete" {
		t.Errorf("expected /downloads/complete, got %v", misc["complete_dir"])
	}
}

func TestHandler_GetCats(t *testing.T) {
	h := newTestHandler("")

	req := httptest.NewRequest("GET", "/sabnzbd/api?mode=get_cats&apikey=testapikey", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	var resp map[string]any
	json.NewDecoder(rec.Body).Decode(&resp)

	cats, ok := resp["categories"].([]any)
	if !ok {
		t.Fatal("expected categories array")
	}
	if len(cats) == 0 {
		t.Error("expected non-empty categories")
	}

	found := false
	for _, c := range cats {
		if c == "radarr" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected radarr category")
	}
}

func TestHandler_AddURL(t *testing.T) {
	mockSlskd := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" && strings.Contains(r.URL.Path, "/transfers/downloads/") {
			w.WriteHeader(http.StatusCreated)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer mockSlskd.Close()

	h := newTestHandler(mockSlskd.URL)

	token := newznab.EncodeToken("soulseekuser", `C:\Movies\Cool.Movie.2024.mkv`, 2000000000)
	nzbURL := "http://localhost:6969/api?t=get&id=" + token

	reqURL := "/sabnzbd/api?mode=addurl&apikey=testapikey&cat=radarr&name=" + url.QueryEscape(nzbURL)
	req := httptest.NewRequest("GET", reqURL, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	var resp map[string]any
	json.NewDecoder(rec.Body).Decode(&resp)

	if resp["status"] != true {
		t.Errorf("expected status true, got %v", resp["status"])
	}

	nzoIDs, ok := resp["nzo_ids"].([]any)
	if !ok || len(nzoIDs) == 0 {
		t.Fatal("expected nzo_ids")
	}

	id := nzoIDs[0].(string)
	if !strings.HasPrefix(id, "SABnzbd_nzo_") {
		t.Errorf("expected SABnzbd_nzo_ prefix, got %s", id)
	}

	// Verify it's in the queue
	queue := h.Store.Queue()
	if len(queue) != 1 {
		t.Fatalf("expected 1 in queue, got %d", len(queue))
	}
	if queue[0].Username != "soulseekuser" {
		t.Errorf("expected soulseekuser, got %s", queue[0].Username)
	}
	if queue[0].Category != "radarr" {
		t.Errorf("expected radarr category, got %s", queue[0].Category)
	}
}

func TestHandler_Queue(t *testing.T) {
	h := newTestHandler("")
	h.Store.Add("user1", `C:\Movies\movie.mkv`, 1000000000, "radarr")
	h.Store.Add("user2", `C:\TV\show.mkv`, 500000000, "sonarr")

	req := httptest.NewRequest("GET", "/sabnzbd/api?mode=queue&apikey=testapikey", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	var resp map[string]any
	json.NewDecoder(rec.Body).Decode(&resp)

	queue, ok := resp["queue"].(map[string]any)
	if !ok {
		t.Fatal("expected queue object")
	}

	slots, ok := queue["slots"].([]any)
	if !ok {
		t.Fatal("expected slots array")
	}

	if len(slots) != 2 {
		t.Errorf("expected 2 slots, got %d", len(slots))
	}
}

func TestHandler_History(t *testing.T) {
	h := newTestHandler("")
	id := h.Store.Add("user1", `C:\Movies\movie.mkv`, 1000000000, "radarr")
	h.Store.UpdateTransfer(id, 1000000000, store.StatusCompleted)

	req := httptest.NewRequest("GET", "/sabnzbd/api?mode=history&apikey=testapikey", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	var resp map[string]any
	json.NewDecoder(rec.Body).Decode(&resp)

	history, ok := resp["history"].(map[string]any)
	if !ok {
		t.Fatal("expected history object")
	}

	slots, ok := history["slots"].([]any)
	if !ok {
		t.Fatal("expected slots array")
	}

	if len(slots) != 1 {
		t.Errorf("expected 1 slot, got %d", len(slots))
	}

	slot := slots[0].(map[string]any)
	if slot["status"] != "Completed" {
		t.Errorf("expected Completed, got %v", slot["status"])
	}
	if !strings.Contains(slot["storage"].(string), "radarr") {
		t.Errorf("expected radarr in storage path, got %s", slot["storage"])
	}
	if !strings.Contains(slot["storage"].(string), "movie.mkv") {
		t.Errorf("expected movie.mkv in storage path, got %s", slot["storage"])
	}
}

func TestHandler_QueueDelete(t *testing.T) {
	h := newTestHandler("")
	id := h.Store.Add("user1", "file.mkv", 1000, "radarr")

	req := httptest.NewRequest("GET", "/sabnzbd/api?mode=queue&name=delete&value="+id+"&apikey=testapikey", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	var resp map[string]any
	json.NewDecoder(rec.Body).Decode(&resp)

	if resp["status"] != true {
		t.Error("expected status true")
	}

	dl := h.Store.Get(id)
	if dl != nil {
		t.Error("expected download to be removed")
	}
}

func TestHandler_HistoryDelete(t *testing.T) {
	h := newTestHandler("")
	id := h.Store.Add("user1", "file.mkv", 1000, "radarr")
	h.Store.UpdateTransfer(id, 1000, store.StatusCompleted)

	req := httptest.NewRequest("GET", "/sabnzbd/api?mode=history&name=delete&value="+id+"&apikey=testapikey", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	var resp map[string]any
	json.NewDecoder(rec.Body).Decode(&resp)

	if resp["status"] != true {
		t.Error("expected status true")
	}

	dl := h.Store.Get(id)
	if dl != nil {
		t.Error("expected download to be removed from history")
	}
}

func TestHandler_UnknownMode(t *testing.T) {
	h := newTestHandler("")

	req := httptest.NewRequest("GET", "/sabnzbd/api?mode=unknown", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	var resp map[string]any
	json.NewDecoder(rec.Body).Decode(&resp)

	if resp["status"] != false {
		t.Error("expected status false for unknown mode")
	}
}

func TestExtractTokenFromURL(t *testing.T) {
	token := newznab.EncodeToken("user", "file.mkv", 1000)
	url := "http://localhost:6969/api?t=get&id=" + token

	got, err := extractTokenFromURL(url)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != token {
		t.Errorf("expected %s, got %s", token, got)
	}
}

func TestExtractTokenFromURL_NoID(t *testing.T) {
	_, err := extractTokenFromURL("http://localhost:6969/api?t=get")
	if err == nil {
		t.Fatal("expected error for URL without id param")
	}
}
