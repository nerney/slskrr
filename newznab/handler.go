package newznab

import (
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/nerney/slskrr/slskd"
)

// videoExtensions are file extensions we consider relevant for Movies/TV.
var videoExtensions = map[string]bool{
	".mkv":  true,
	".mp4":  true,
	".avi":  true,
	".m4v":  true,
	".webm": true,
	".ts":   true,
	".wmv":  true,
}

// audioExtensions are file extensions we consider relevant for Music.
var audioExtensions = map[string]bool{
	".mp3":  true,
	".flac": true,
	".ogg":  true,
	".opus": true,
	".m4a":  true,
	".aac":  true,
	".wav":  true,
	".wma":  true,
	".ape":  true,
	".alac": true,
}

// minVideoFileSize is the minimum file size (50MB) to filter out samples/trailers.
const minVideoFileSize = 50 * 1024 * 1024

// minAudioFileSize is the minimum file size (1MB) to filter out tiny/corrupt files.
const minAudioFileSize = 1 * 1024 * 1024

// FileToken encodes the slskd file info needed to queue a download later.
type FileToken struct {
	Username string `json:"u"`
	Filename string `json:"f"`
	Size     int64  `json:"s"`
}

func EncodeToken(username, filename string, size int64) string {
	t := FileToken{Username: username, Filename: filename, Size: size}
	b, _ := json.Marshal(t)
	return base64.URLEncoding.EncodeToString(b)
}

func DecodeToken(token string) (*FileToken, error) {
	b, err := base64.URLEncoding.DecodeString(token)
	if err != nil {
		return nil, fmt.Errorf("decode base64: %w", err)
	}
	var t FileToken
	if err := json.Unmarshal(b, &t); err != nil {
		return nil, fmt.Errorf("unmarshal token: %w", err)
	}
	return &t, nil
}

// Handler serves the Newznab API facade.
type Handler struct {
	SlskdClient   *slskd.Client
	APIKey        string
	SearchTimeout time.Duration
	BaseURL       string // e.g. "http://localhost:6969" for constructing download URLs
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	action := q.Get("t")

	switch action {
	case "caps":
		h.handleCaps(w, r)
	case "search", "tvsearch", "movie", "music":
		h.handleSearch(w, r, action)
	case "get":
		h.handleGet(w, r)
	default:
		writeError(w, 202, "No such function ("+action+")")
	}
}

func (h *Handler) checkAPIKey(r *http.Request) bool {
	if h.APIKey == "" {
		return true
	}
	key := r.URL.Query().Get("apikey")
	return subtle.ConstantTimeCompare([]byte(key), []byte(h.APIKey)) == 1
}

func (h *Handler) handleCaps(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	fmt.Fprint(w, capsXML)
}

func (h *Handler) handleSearch(w http.ResponseWriter, r *http.Request, action string) {
	if !h.checkAPIKey(r) {
		writeError(w, 100, "Incorrect user credentials")
		return
	}

	q := r.URL.Query()
	query := q.Get("q")

	// Build search query based on action type
	switch action {
	case "tvsearch":
		season := q.Get("season")
		ep := q.Get("ep")
		if query != "" && season != "" && ep != "" {
			query = fmt.Sprintf("%s S%02sE%02s", query, zeroPad(season), zeroPad(ep))
		} else if query != "" && season != "" {
			query = fmt.Sprintf("%s S%02s", query, zeroPad(season))
		}
	case "movie":
		// q already contains the movie title from Radarr
		// imdbid alone can't be resolved without an external service
	case "music":
		artist := q.Get("artist")
		album := q.Get("album")
		if query == "" {
			parts := []string{}
			if artist != "" {
				parts = append(parts, artist)
			}
			if album != "" {
				parts = append(parts, album)
			}
			query = strings.Join(parts, " ")
		}
	}

	if query == "" {
		writeSearchResponse(w, nil, h.BaseURL)
		return
	}

	slog.Info("searching slskd", "query", query, "action", action)

	responses, err := h.SlskdClient.SearchAndWait(r.Context(), query, h.SearchTimeout)
	if err != nil {
		slog.Error("slskd search failed", "error", err)
		writeError(w, 900, "slskd search failed")
		return
	}

	// Collect and filter results
	var items []searchItem
	for _, resp := range responses {
		for _, f := range resp.Files {
			ext := strings.ToLower(path.Ext(f.Filename))

			isVideo := videoExtensions[ext]
			isAudio := audioExtensions[ext]
			if !isVideo && !isAudio {
				continue
			}
			if isVideo && f.Size < minVideoFileSize {
				continue
			}
			if isAudio && f.Size < minAudioFileSize {
				continue
			}

			token := EncodeToken(resp.Username, f.Filename, f.Size)
			// Convert backslashes (Windows paths from Soulseek) to forward slashes
			basename := path.Base(strings.ReplaceAll(f.Filename, "\\", "/"))

			category := "2000"
			switch {
			case action == "music" || isAudio:
				category = "3000"
			case action == "tvsearch":
				category = "5000"
			}

			items = append(items, searchItem{
				Title:    basename,
				Token:    token,
				Size:     f.Size,
				Category: category,
				Username: resp.Username,
			})
		}
	}

	slog.Info("search complete", "query", query, "results", len(items))
	writeSearchResponse(w, items, h.BaseURL)
}

func (h *Handler) handleGet(w http.ResponseWriter, r *http.Request) {
	if !h.checkAPIKey(r) {
		writeError(w, 100, "Incorrect user credentials")
		return
	}

	id := r.URL.Query().Get("id")
	if id == "" {
		writeError(w, 200, "Missing parameter (id)")
		return
	}

	token, err := DecodeToken(id)
	if err != nil {
		writeError(w, 300, "Invalid token")
		return
	}

	basename := path.Base(strings.ReplaceAll(token.Filename, "\\", "/"))

	w.Header().Set("Content-Type", "application/x-nzb")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.nzb"`, basename))
	fmt.Fprintf(w, nzbTemplate, token.Username, token.Filename, token.Size, basename)
}

type searchItem struct {
	Title    string
	Token    string
	Size     int64
	Category string
	Username string
}

func writeSearchResponse(w http.ResponseWriter, items []searchItem, baseURL string) {
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	fmt.Fprint(w, `<?xml version="1.0" encoding="UTF-8"?>`)
	fmt.Fprint(w, "\n")
	fmt.Fprint(w, `<rss version="2.0" xmlns:atom="http://www.w3.org/2005/Atom" xmlns:newznab="http://www.newznab.com/DTD/2010/feeds/attributes/">`)
	fmt.Fprint(w, "\n<channel>")
	fmt.Fprint(w, "\n<title>slskrr</title>")
	fmt.Fprintf(w, "\n<description>slskd Newznab facade</description>")

	for _, item := range items {
		downloadURL := fmt.Sprintf("%s/api?t=get&amp;id=%s", baseURL, item.Token)
		pubDate := time.Now().UTC().Format(time.RFC1123Z)

		fmt.Fprint(w, "\n<item>")
		fmt.Fprintf(w, "\n  <title>%s</title>", xmlEscape(item.Title))
		fmt.Fprintf(w, "\n  <guid>%s</guid>", item.Token)
		fmt.Fprintf(w, "\n  <link>%s</link>", downloadURL)
		fmt.Fprintf(w, "\n  <pubDate>%s</pubDate>", pubDate)
		fmt.Fprintf(w, "\n  <enclosure url=\"%s\" length=\"%d\" type=\"application/x-nzb\" />", downloadURL, item.Size)
		fmt.Fprintf(w, "\n  <newznab:attr name=\"size\" value=\"%d\" />", item.Size)
		fmt.Fprintf(w, "\n  <newznab:attr name=\"category\" value=\"%s\" />", item.Category)
		fmt.Fprintf(w, "\n  <newznab:attr name=\"grabs\" value=\"0\" />")
		fmt.Fprint(w, "\n</item>")
	}

	fmt.Fprint(w, "\n</channel>")
	fmt.Fprint(w, "\n</rss>\n")
}

func writeError(w http.ResponseWriter, code int, description string) {
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.WriteHeader(http.StatusOK) // Newznab errors are returned as 200 with error XML
	fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?>
<error code="%d" description="%s" />`, code, xmlEscape(description))
}

func xmlEscape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, "\"", "&quot;")
	return s
}

func zeroPad(s string) string {
	if len(s) == 1 {
		return "0" + s
	}
	return s
}

const capsXML = `<?xml version="1.0" encoding="UTF-8"?>
<caps>
  <server version="1.0" title="slskrr" strapline="Soulseek via slskd" />
  <limits max="100" default="100" />
  <searching>
    <search available="yes" supportedParams="q" />
    <tv-search available="yes" supportedParams="q,season,ep" />
    <movie-search available="yes" supportedParams="q,imdbid" />
    <music-search available="yes" supportedParams="q,artist,album" />
  </searching>
  <categories>
    <category id="2000" name="Movies">
      <subcat id="2010" name="Foreign" />
      <subcat id="2020" name="Other" />
      <subcat id="2030" name="SD" />
      <subcat id="2040" name="HD" />
      <subcat id="2045" name="UHD" />
      <subcat id="2050" name="BluRay" />
      <subcat id="2060" name="3D" />
    </category>
    <category id="3000" name="Audio">
      <subcat id="3010" name="MP3" />
      <subcat id="3020" name="Video" />
      <subcat id="3030" name="Audiobook" />
      <subcat id="3040" name="Lossless" />
      <subcat id="3050" name="Podcast" />
      <subcat id="3060" name="Other" />
    </category>
    <category id="5000" name="TV">
      <subcat id="5020" name="Foreign" />
      <subcat id="5030" name="SD" />
      <subcat id="5040" name="HD" />
      <subcat id="5045" name="UHD" />
      <subcat id="5050" name="Other" />
      <subcat id="5060" name="Sport" />
      <subcat id="5070" name="Anime" />
      <subcat id="5080" name="Documentary" />
    </category>
  </categories>
</caps>`

const nzbTemplate = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE nzb PUBLIC "-//newzBin//DTD NZB 1.1//EN" "http://www.newzbin.com/DTD/nzb/nzb-1.1.dtd">
<nzb xmlns="http://www.newzbin.com/DTD/2003/nzb">
  <head>
    <meta type="username">%s</meta>
    <meta type="filename">%s</meta>
    <meta type="size">%d</meta>
    <meta type="name">%s</meta>
  </head>
  <file poster="slskrr" date="0" subject="slskd download">
    <groups><group>alt.binaries.slskd</group></groups>
    <segments><segment bytes="0" number="1">placeholder@slskrr</segment></segments>
  </file>
</nzb>`
