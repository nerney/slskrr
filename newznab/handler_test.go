package newznab

import (
	"encoding/json"
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nerney/slskrr/slskd"
)

func TestEncodeDecodeToken(t *testing.T) {
	token := EncodeToken("testuser", `C:\Music\movie.mkv`, 123456789)
	decoded, err := DecodeToken(token)
	if err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if decoded.Username != "testuser" {
		t.Errorf("expected testuser, got %s", decoded.Username)
	}
	if decoded.Filename != `C:\Music\movie.mkv` {
		t.Errorf("expected C:\\Music\\movie.mkv, got %s", decoded.Filename)
	}
	if decoded.Size != 123456789 {
		t.Errorf("expected 123456789, got %d", decoded.Size)
	}
}

func TestDecodeToken_Invalid(t *testing.T) {
	_, err := DecodeToken("not-valid-base64!!!")
	if err == nil {
		t.Fatal("expected error for invalid base64")
	}

	_, err = DecodeToken("bm90anNvbg==") // "notjson"
	if err == nil {
		t.Fatal("expected error for invalid json")
	}
}

func TestHandler_Caps(t *testing.T) {
	h := &Handler{}

	req := httptest.NewRequest("GET", "/api?t=caps", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}

	ct := rec.Header().Get("Content-Type")
	if !strings.Contains(ct, "application/xml") {
		t.Errorf("expected XML content type, got %s", ct)
	}

	body := rec.Body.String()
	if !strings.Contains(body, "<caps>") {
		t.Error("response should contain <caps>")
	}
	if !strings.Contains(body, `id="2000"`) {
		t.Error("response should contain Movies category")
	}
	if !strings.Contains(body, `id="5000"`) {
		t.Error("response should contain TV category")
	}
	if !strings.Contains(body, `tv-search available="yes"`) {
		t.Error("response should declare tv-search available")
	}

	// Verify it's valid XML
	var caps struct{}
	if err := xml.Unmarshal(rec.Body.Bytes(), &caps); err != nil {
		t.Errorf("caps XML should be valid: %v", err)
	}
}

func TestHandler_Search_NoAPIKey(t *testing.T) {
	h := &Handler{
		APIKey: "secret",
	}

	req := httptest.NewRequest("GET", "/api?t=search&q=test", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, "Incorrect user credentials") {
		t.Errorf("expected auth error, got: %s", body)
	}
}

func TestHandler_Search_WithMockSlskd(t *testing.T) {
	// Mock slskd server
	searchCreated := false
	mockSlskd := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "POST" && strings.HasPrefix(r.URL.Path, "/api/v0/searches"):
			searchCreated = true
			json.NewEncoder(w).Encode(slskd.SearchResult{
				ID:    "test-search-id",
				State: "InProgress",
			})
		case r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/test-search-id"):
			json.NewEncoder(w).Encode(slskd.SearchResult{
				ID:    "test-search-id",
				State: "Completed",
			})
		case r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/responses"):
			json.NewEncoder(w).Encode([]slskd.SearchResponse{
				{
					Username: "cooluser",
					Files: []slskd.SlskdFile{
						{Filename: `C:\Movies\The.Matrix.1999.1080p.mkv`, Size: 2000000000},
						{Filename: `C:\Movies\sample.avi`, Size: 5000000}, // too small, should be filtered
						{Filename: `C:\Movies\subs.srt`, Size: 50000},     // wrong extension, should be filtered
					},
				},
			})
		case r.Method == "DELETE":
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer mockSlskd.Close()

	client := slskd.NewClient(mockSlskd.URL, "testkey")
	h := &Handler{
		SlskdClient:   client,
		SearchTimeout: 5 * time.Second,
		BaseURL:       "http://localhost:6969",
	}

	req := httptest.NewRequest("GET", "/api?t=search&q=The+Matrix", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if !searchCreated {
		t.Error("expected search to be created on slskd")
	}

	body := rec.Body.String()
	if !strings.Contains(body, "The.Matrix.1999.1080p.mkv") {
		t.Errorf("expected movie file in results, got: %s", body)
	}
	if strings.Contains(body, "sample.avi") {
		t.Error("sample file should be filtered out (too small)")
	}
	if strings.Contains(body, "subs.srt") {
		t.Error("subtitle file should be filtered out (wrong extension)")
	}
	if !strings.Contains(body, "application/x-nzb") {
		t.Error("enclosure should have NZB type")
	}
	if !strings.Contains(body, `<newznab:attr name="size"`) {
		t.Error("should have newznab size attribute")
	}
}

func TestHandler_TVSearch_QueryConstruction(t *testing.T) {
	var receivedQuery string
	mockSlskd := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "POST" && strings.HasPrefix(r.URL.Path, "/api/v0/searches"):
			var req slskd.SearchRequest
			json.NewDecoder(r.Body).Decode(&req)
			receivedQuery = req.SearchText
			json.NewEncoder(w).Encode(slskd.SearchResult{ID: "s1", State: "Completed"})
		case strings.HasSuffix(r.URL.Path, "/responses"):
			json.NewEncoder(w).Encode([]slskd.SearchResponse{})
		case r.Method == "DELETE":
			w.WriteHeader(http.StatusNoContent)
		default:
			json.NewEncoder(w).Encode(slskd.SearchResult{ID: "s1", State: "Completed"})
		}
	}))
	defer mockSlskd.Close()

	client := slskd.NewClient(mockSlskd.URL, "testkey")
	h := &Handler{
		SlskdClient:   client,
		SearchTimeout: 5 * time.Second,
		BaseURL:       "http://localhost:6969",
	}

	req := httptest.NewRequest("GET", "/api?t=tvsearch&q=Breaking+Bad&season=1&ep=5", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}

	if receivedQuery != "Breaking Bad S01E05" {
		t.Errorf("expected 'Breaking Bad S01E05', got '%s'", receivedQuery)
	}
}

func TestHandler_Get(t *testing.T) {
	h := &Handler{
		BaseURL: "http://localhost:6969",
	}

	token := EncodeToken("testuser", `C:\Movies\movie.mkv`, 1000000)
	req := httptest.NewRequest("GET", "/api?t=get&id="+token, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}

	ct := rec.Header().Get("Content-Type")
	if ct != "application/x-nzb" {
		t.Errorf("expected application/x-nzb, got %s", ct)
	}

	disp := rec.Header().Get("Content-Disposition")
	if !strings.Contains(disp, "movie.mkv.nzb") {
		t.Errorf("expected movie.mkv.nzb in disposition, got %s", disp)
	}

	body := rec.Body.String()
	if !strings.Contains(body, "testuser") {
		t.Error("NZB should contain username")
	}
	if !strings.Contains(body, "movie.mkv") {
		t.Error("NZB should contain filename")
	}
}

func TestHandler_UnknownAction(t *testing.T) {
	h := &Handler{}

	req := httptest.NewRequest("GET", "/api?t=unknown", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, "No such function") {
		t.Errorf("expected error for unknown action, got: %s", body)
	}
}

func TestHandler_EmptySearch(t *testing.T) {
	h := &Handler{
		BaseURL: "http://localhost:6969",
	}

	req := httptest.NewRequest("GET", "/api?t=search&q=", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, "<rss") {
		t.Errorf("expected RSS XML for empty search, got: %s", body)
	}
	// Empty search returns a mock test item for Prowlarr compatibility
	if !strings.Contains(body, "<item>") {
		t.Error("expected mock test item for empty search (Prowlarr compatibility)")
	}
	if !strings.Contains(body, "slskrr-test") {
		t.Error("expected mock item to contain slskrr-test title")
	}
}
