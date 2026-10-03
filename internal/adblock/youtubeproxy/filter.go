// Package youtubeproxy provides opt-in server-side filtering for trusted clients.
package youtubeproxy

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"golang.org/x/net/html"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

const maxBody = 8 << 20

var playerAPI = regexp.MustCompile(`^/youtubei/v[0-9]+/(player|next)$`)
var initialPlayer = regexp.MustCompile(`(?:var\s+)?ytInitialPlayerResponse\s*=\s*`)
var adKeys = []string{"adPlacements", "playerAds", "adSlots", "adBreakHeartbeatParams"}

func interceptedHost(host string) bool {
	switch strings.ToLower(host) {
	case "youtube.com", "www.youtube.com", "m.youtube.com":
		return true
	}
	return false
}
func cleanPlayer(value any, depth int) bool {
	if depth > 8 {
		return false
	}
	obj, ok := value.(map[string]any)
	if !ok {
		return false
	}
	changed := false
	for _, key := range adKeys {
		if _, ok := obj[key]; ok {
			delete(obj, key)
			changed = true
		}
	}
	if nested, ok := obj["playerResponse"]; ok {
		changed = cleanPlayer(nested, depth+1) || changed
	}
	return changed
}
func cleanJSON(raw []byte) ([]byte, bool) {
	if !utf8.Valid(raw) {
		return raw, false
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if decoder.Decode(&value) != nil {
		return raw, false
	}
	var trailing any
	if decoder.Decode(&trailing) != io.EOF {
		return raw, false
	}
	if !cleanPlayer(value, 0) {
		return raw, false
	}
	filtered, err := json.Marshal(value)
	if err != nil {
		return raw, false
	}
	return filtered, true
}

// jsonEnd scans one object without treating braces inside JSON strings as structure.
func jsonEnd(raw []byte, start int) int {
	if start >= len(raw) || raw[start] != '{' {
		return -1
	}
	depth := 0
	quoted := false
	escape := false
	for i := start; i < len(raw); i++ {
		c := raw[i]
		if quoted {
			if escape {
				escape = false
			} else if c == '\\' {
				escape = true
			} else if c == '"' {
				quoted = false
			}
			continue
		}
		switch c {
		case '"':
			quoted = true
		case '{', '[':
			depth++
		case '}', ']':
			depth--
			if depth == 0 {
				return i + 1
			}
		}
	}
	return -1
}
func cleanScript(raw []byte) ([]byte, bool) {
	// Only rewrite a syntactically valid initial player object. Unknown layouts,
	// JSON errors and non-player content remain byte-for-byte unchanged.
	matches := initialPlayer.FindAllIndex(raw, -1)
	if len(matches) == 0 {
		return raw, false
	}
	var out bytes.Buffer
	cursor := 0
	changed := false
	for _, match := range matches {
		start := match[1]
		if start < cursor {
			continue
		}
		end := jsonEnd(raw, start)
		if end < 0 {
			continue
		}
		cleaned, ok := cleanJSON(raw[start:end])
		if !ok {
			continue
		}
		out.Write(raw[cursor:start])
		out.Write(cleaned)
		cursor = end
		changed = true
	}
	if !changed {
		return raw, false
	}
	out.Write(raw[cursor:])
	return out.Bytes(), true
}

type joinedBody struct {
	io.Reader
	io.Closer
}

func filterResponse(response *http.Response, path string) bool {
	return filterResponseLimit(response, path, maxBody)
}
func filterResponseLimit(response *http.Response, path string, limit int) bool {
	changed, _ := filterResponseReason(response, path, limit)
	return changed
}
func filterResponseReason(response *http.Response, path string, limit int) (bool, string) {
	if response.StatusCode != http.StatusOK || response.Header.Get("Content-Range") != "" {
		return false, "unchanged"
	}
	contentType := strings.ToLower(response.Header.Get("Content-Type"))
	jsonResponse := playerAPI.MatchString(path) && strings.Contains(contentType, "json")
	htmlResponse := (path == "/watch" || path == "/" || strings.HasPrefix(path, "/shorts/")) && strings.Contains(contentType, "text/html")
	if !jsonResponse && !htmlResponse {
		return false, "unchanged"
	}
	encoding := strings.ToLower(strings.TrimSpace(response.Header.Get("Content-Encoding")))
	if encoding != "" && encoding != "identity" && encoding != "gzip" {
		return false, "unsupported_encoding"
	}
	original := response.Body
	raw, err := io.ReadAll(io.LimitReader(original, int64(limit)+1))
	response.Body = joinedBody{io.MultiReader(bytes.NewReader(raw), original), original}
	if err != nil {
		return false, "read_error"
	}
	if len(raw) > limit {
		return false, "oversized"
	}
	decoded := raw
	if encoding == "gzip" {
		reader, err := gzip.NewReader(bytes.NewReader(raw))
		if err != nil {
			return false, "malformed"
		}
		decoded, err = io.ReadAll(io.LimitReader(reader, int64(limit)+1))
		closeErr := reader.Close()
		if err != nil || closeErr != nil {
			return false, "malformed"
		}
		if len(decoded) > limit {
			return false, "oversized"
		}
	}
	var filtered []byte
	var changed bool
	if jsonResponse {
		if !utf8.Valid(decoded) || !json.Valid(decoded) {
			return false, "malformed"
		}
		filtered, changed = cleanJSON(decoded)
	} else {
		filtered, changed = cleanHTML(decoded)
	}
	if !changed {
		return false, "unchanged"
	}
	_ = original.Close()
	response.Body = io.NopCloser(bytes.NewReader(filtered))
	response.ContentLength = int64(len(filtered))
	response.TransferEncoding = nil
	response.Header.Set("Content-Length", strconv.Itoa(len(filtered)))
	for _, name := range []string{"Content-Encoding", "ETag", "Content-MD5", "Digest", "Content-Digest", "Repr-Digest", "Accept-Ranges"} {
		response.Header.Del(name)
	}
	response.Header.Set("Cache-Control", "no-store")
	return true, "filtered"
}

func cleanHTML(raw []byte) ([]byte, bool) {
	tokenizer := html.NewTokenizer(bytes.NewReader(raw))
	var output bytes.Buffer
	inScript := false
	changed := false
	for {
		kind := tokenizer.Next()
		if kind == html.ErrorToken {
			if tokenizer.Err() != io.EOF {
				return raw, false
			}
			break
		}
		piece := append([]byte(nil), tokenizer.Raw()...)
		if kind == html.StartTagToken || kind == html.EndTagToken {
			name, _ := tokenizer.TagName()
			if bytes.EqualFold(name, []byte("script")) {
				inScript = kind == html.StartTagToken
			}
		}
		if kind == html.TextToken && inScript {
			if filtered, ok := cleanScript(piece); ok {
				piece = filtered
				changed = true
			}
		}
		output.Write(piece)
	}
	if !changed {
		return raw, false
	}
	return output.Bytes(), true
}
