package downloader

import (
	"bufio"
	"context"
	"io"
	"mime"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/olivier-w/climp/internal/media"
)

// URLRouteKind describes which playback path a URL should take.
type URLRouteKind int

const (
	RouteFiniteDownload URLRouteKind = iota
	RouteLiveStream
	RouteRemotePlaylist
)

// URLRouteResult is the classification outcome for an input URL.
type URLRouteResult struct {
	Kind     URLRouteKind
	FinalURL string
	Playlist []media.PlaylistEntry
}

const (
	routeProbeTimeout   = 4 * time.Second
	routeProbeBodyLimit = 128 * 1024
)

var (
	routeHTTPClient = &http.Client{
		Timeout: routeProbeTimeout,
	}

	liveURLCacheMu sync.RWMutex
	liveURLCache   = make(map[string]struct{})
)

type probeResult struct {
	originalURL   string
	finalURL      string
	contentType   string
	contentLength int64
	headers       http.Header
	body          string
	chunked       bool
}

// IsLiveURL reports whether a URL should use the live playback path.
// It returns true for explicit live suffixes and for URLs previously
// classified as live via ResolveURLRoute.
func IsLiveURL(rawURL string) bool {
	if IsLiveBySuffix(rawURL) {
		return true
	}
	key, ok := normalizeURLKey(rawURL)
	if !ok {
		return false
	}
	liveURLCacheMu.RLock()
	_, found := liveURLCache[key]
	liveURLCacheMu.RUnlock()
	return found
}

// ResolveURLRoute probes a URL and classifies it as finite media download,
// live stream, or remote playlist wrapper.
func ResolveURLRoute(rawURL string) (URLRouteResult, error) {
	normalizedURL, err := normalizeAndValidateURL(rawURL)
	if err != nil {
		return URLRouteResult{}, err
	}

	result := URLRouteResult{
		Kind:     RouteFiniteDownload,
		FinalURL: normalizedURL,
	}

	probe, err := probeURL(normalizedURL)
	if err != nil {
		return result, err
	}
	if probe.finalURL != "" {
		result.FinalURL = probe.finalURL
	}

	if hasHLSBodyMarker(probe.body) {
		result.Kind = RouteLiveStream
		cacheLiveURL(normalizedURL)
		cacheLiveURL(result.FinalURL)
		return result, nil
	}

	if isRemotePlaylist(probe) {
		entries := parseRemotePlaylistBody(probe.body, result.FinalURL)
		if len(entries) > 0 {
			result.Kind = RouteRemotePlaylist
			result.Playlist = entries
			return result, nil
		}
	}

	if isLiveProbe(probe) {
		result.Kind = RouteLiveStream
		cacheLiveURL(normalizedURL)
		cacheLiveURL(result.FinalURL)
		return result, nil
	}

	return result, nil
}

func probeURL(rawURL string) (probeResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), routeProbeTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return probeResult{}, err
	}
	req.Header.Set("Range", "bytes=0-65535")
	req.Header.Set("Icy-MetaData", "1")
	req.Header.Set("User-Agent", "climp")

	resp, err := routeHTTPClient.Do(req)
	if err != nil {
		return probeResult{}, err
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, routeProbeBodyLimit))
	contentType := strings.TrimSpace(resp.Header.Get("Content-Type"))
	if contentType != "" {
		if mediaType, _, parseErr := mime.ParseMediaType(contentType); parseErr == nil {
			contentType = strings.ToLower(strings.TrimSpace(mediaType))
		} else {
			contentType = strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
		}
	}

	finalURL := rawURL
	if resp.Request != nil && resp.Request.URL != nil {
		finalURL = resp.Request.URL.String()
	}

	p := probeResult{
		originalURL:   rawURL,
		finalURL:      finalURL,
		contentType:   contentType,
		contentLength: resp.ContentLength,
		headers:       resp.Header,
		body:          string(bodyBytes),
	}
	for _, enc := range resp.TransferEncoding {
		if strings.EqualFold(strings.TrimSpace(enc), "chunked") {
			p.chunked = true
			break
		}
	}

	return p, nil
}

func isRemotePlaylist(p probeResult) bool {
	if hasHLSBodyMarker(p.body) {
		return false
	}
	if hasPlaylistExt(p.originalURL) || hasPlaylistExt(p.finalURL) {
		return true
	}
	if isPlaylistContentType(p.contentType) {
		return true
	}
	return hasPlaylistBodyMarker(p.body)
}

func isLiveProbe(p probeResult) bool {
	if hasICYHeaders(p.headers) {
		return true
	}
	if isHLSContentType(p.contentType) {
		return true
	}
	if IsLiveBySuffix(p.finalURL) || IsLiveBySuffix(p.originalURL) {
		return true
	}
	if isAudioLikeContentType(p.contentType) && (p.contentLength <= 0 || p.chunked) {
		return true
	}
	return false
}

func hasPlaylistExt(rawURL string) bool {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return false
	}
	path := strings.ToLower(parsed.Path)
	return strings.HasSuffix(path, ".pls") ||
		strings.HasSuffix(path, ".m3u") ||
		strings.HasSuffix(path, ".m3u8")
}

func isPlaylistContentType(contentType string) bool {
	switch strings.ToLower(strings.TrimSpace(contentType)) {
	case "audio/x-mpegurl",
		"application/x-mpegurl",
		"application/vnd.apple.mpegurl",
		"audio/mpegurl",
		"audio/x-scpls",
		"application/pls+xml":
		return true
	default:
		return false
	}
}

func isHLSContentType(contentType string) bool {
	switch strings.ToLower(strings.TrimSpace(contentType)) {
	case "application/vnd.apple.mpegurl",
		"application/x-mpegurl":
		return true
	default:
		return false
	}
}

func isAudioLikeContentType(contentType string) bool {
	contentType = strings.ToLower(strings.TrimSpace(contentType))
	return strings.HasPrefix(contentType, "audio/") ||
		contentType == "application/ogg" ||
		contentType == "application/aacp"
}

func hasICYHeaders(headers http.Header) bool {
	for key := range headers {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(key)), "icy-") {
			return true
		}
	}
	return false
}

func hasPlaylistBodyMarker(body string) bool {
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(strings.TrimPrefix(line, "\uFEFF"))
		if trimmed == "" {
			continue
		}
		lower := strings.ToLower(trimmed)
		return strings.HasPrefix(lower, "#extm3u") || lower == "[playlist]"
	}
	return false
}

func hasHLSBodyMarker(body string) bool {
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.ToUpper(strings.TrimSpace(strings.TrimPrefix(line, "\uFEFF")))
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "#EXT-X-") {
			return true
		}
	}
	return false
}

func parseRemotePlaylistBody(body, baseURL string) []media.PlaylistEntry {
	body = strings.TrimSpace(strings.TrimPrefix(body, "\uFEFF"))
	if body == "" {
		return nil
	}
	if looksLikePLS(body) {
		return parseRemotePLS(body, baseURL)
	}
	return parseRemoteM3U(body, baseURL)
}

func looksLikePLS(body string) bool {
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(line, "\uFEFF")))
		if trimmed == "" {
			continue
		}
		if trimmed == "[playlist]" {
			return true
		}
		break
	}
	return strings.Contains(strings.ToLower(body), "\nfile1=")
}

func parseRemoteM3U(body, baseURL string) []media.PlaylistEntry {
	scanner := bufio.NewScanner(strings.NewReader(body))
	entries := make([]media.PlaylistEntry, 0)
	pendingTitle := ""
	for scanner.Scan() {
		line := normalizeRemotePlaylistValue(scanner.Text())
		if line == "" {
			continue
		}
		lower := strings.ToLower(line)
		if strings.HasPrefix(lower, "#extinf:") {
			if comma := strings.Index(line, ","); comma >= 0 && comma+1 < len(line) {
				pendingTitle = strings.TrimSpace(line[comma+1:])
			}
			continue
		}
		if strings.HasPrefix(line, "#") {
			continue
		}
		resolvedURL, ok := resolveRemoteURL(line, baseURL)
		if !ok {
			pendingTitle = ""
			continue
		}
		title := pendingTitle
		if title == "" {
			title = resolvedURL
		}
		entries = append(entries, media.PlaylistEntry{Title: title, URL: resolvedURL})
		pendingTitle = ""
	}
	return entries
}

func parseRemotePLS(body, baseURL string) []media.PlaylistEntry {
	scanner := bufio.NewScanner(strings.NewReader(body))
	files := make(map[int]string)
	titles := make(map[int]string)
	for scanner.Scan() {
		line := strings.TrimSpace(strings.TrimPrefix(scanner.Text(), "\uFEFF"))
		if line == "" {
			continue
		}
		eq := strings.Index(line, "=")
		if eq <= 0 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(line[:eq]))
		val := normalizeRemotePlaylistValue(line[eq+1:])
		if val == "" {
			continue
		}
		if idx, ok := parsePLSIndex(key, "file"); ok {
			files[idx] = val
			continue
		}
		if idx, ok := parsePLSIndex(key, "title"); ok {
			titles[idx] = val
		}
	}

	if len(files) == 0 {
		return nil
	}

	indices := make([]int, 0, len(files))
	for idx := range files {
		indices = append(indices, idx)
	}
	sort.Ints(indices)

	entries := make([]media.PlaylistEntry, 0, len(indices))
	for _, idx := range indices {
		resolvedURL, ok := resolveRemoteURL(files[idx], baseURL)
		if !ok {
			continue
		}
		title := strings.TrimSpace(titles[idx])
		if title == "" {
			title = resolvedURL
		}
		entries = append(entries, media.PlaylistEntry{Title: title, URL: resolvedURL})
	}
	return entries
}

func parsePLSIndex(key, prefix string) (int, bool) {
	if !strings.HasPrefix(key, prefix) {
		return 0, false
	}
	rest := strings.TrimSpace(key[len(prefix):])
	if rest == "" {
		return 0, false
	}
	idx, err := strconv.Atoi(rest)
	if err != nil || idx <= 0 {
		return 0, false
	}
	return idx, true
}

func normalizeRemotePlaylistValue(raw string) string {
	s := strings.TrimSpace(strings.TrimPrefix(raw, "\uFEFF"))
	if len(s) >= 2 {
		first := s[0]
		last := s[len(s)-1]
		if (first == '"' || first == '\'') && first == last {
			s = strings.TrimSpace(s[1 : len(s)-1])
		}
	}
	s = strings.TrimSpace(s)
	if strings.HasSuffix(s, ";") {
		s = strings.TrimSpace(strings.TrimSuffix(s, ";"))
	}
	return s
}

func resolveRemoteURL(raw, baseURL string) (string, bool) {
	candidate := normalizeRemotePlaylistValue(raw)
	if candidate == "" {
		return "", false
	}
	parsed, err := url.Parse(candidate)
	if err != nil {
		return "", false
	}

	base, err := url.Parse(baseURL)
	if err != nil {
		return "", false
	}
	if !parsed.IsAbs() {
		parsed = base.ResolveReference(parsed)
	}

	scheme := strings.ToLower(strings.TrimSpace(parsed.Scheme))
	if scheme != "http" && scheme != "https" {
		return "", false
	}
	if parsed.Host == "" {
		return "", false
	}

	return parsed.String(), true
}

func cacheLiveURL(rawURL string) {
	key, ok := normalizeURLKey(rawURL)
	if !ok {
		return
	}
	liveURLCacheMu.Lock()
	liveURLCache[key] = struct{}{}
	liveURLCacheMu.Unlock()
}

func normalizeURLKey(rawURL string) (string, bool) {
	normalized, err := normalizeAndValidateURL(rawURL)
	if err != nil {
		return "", false
	}
	parsed, err := url.Parse(normalized)
	if err != nil {
		return "", false
	}
	parsed.Fragment = ""
	return parsed.String(), true
}
