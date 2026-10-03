package youtubeproxy

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func response(body, kind string) *http.Response {
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {kind}, "ETag": {"old"}}, Body: io.NopCloser(strings.NewReader(body)), ContentLength: int64(len(body))}
}
func TestFilterPlayerPreservesMediaNumbersAndCaptions(t *testing.T) {
	raw := `{"adPlacements":[1],"playerAds":[2],"adSlots":[3],"streamingData":{"url":"https://media.example/x","n":9007199254740993},"captions":{"tracks":[1]},"playabilityStatus":{"status":"OK"}}`
	r := response(raw, "application/json")
	if !filterResponse(r, "/youtubei/v1/player") {
		t.Fatal("not filtered")
	}
	body, _ := io.ReadAll(r.Body)
	if bytes.Contains(body, []byte("adSlots")) || !bytes.Contains(body, []byte("9007199254740993")) || !bytes.Contains(body, []byte("captions")) {
		t.Fatalf("damaged content: %s", body)
	}
	if r.Header.Get("ETag") != "" || r.ContentLength != int64(len(body)) || r.Header.Get("Cache-Control") != "no-store" {
		t.Fatal("stale metadata")
	}
	var parsed map[string]any
	if json.Unmarshal(body, &parsed) != nil {
		t.Fatal("invalid JSON")
	}
}
func TestHTMLInitialPlayerAndEscapedStrings(t *testing.T) {
	raw := `<html><body>ytInitialPlayerResponse = {"adSlots":[1]}<script nonce="x">var ytInitialPlayerResponse = {"adSlots":[1],"streamingData":{"url":"x}\\\"y"},"captions":{"tracks":[1]}}; var other=42;</script></body></html>`
	out, ok := cleanHTML([]byte(raw))
	if !ok {
		t.Fatal("initial data not filtered")
	}
	if !bytes.Contains(out, []byte(`nonce="x"`)) || !bytes.Contains(out, []byte(`var other=42;`)) || !bytes.Contains(out, []byte(`body>ytInitialPlayerResponse = {"adSlots":[1]}`)) {
		t.Fatalf("unrelated HTML changed: %s", out)
	}
	if strings.Count(string(out), "adSlots") != 1 {
		t.Fatalf("script ad data survived: %s", out)
	}
}
func TestFilterFailOpenAndStreaming(t *testing.T) {
	for _, tc := range []struct{ body, path, kind string }{{`broken {`, "/youtubei/v1/player", "application/json"}, {`{"adSlots":[1]}`, "/youtubei/v1/browse", "application/json"}, {`{"adSlots":[1]}`, "/videoplayback", "video/mp4"}, {`{"streamingData":{}}`, "/youtubei/v1/player", "application/json"}, {strings.Repeat("x", maxBody+1), "/youtubei/v1/player", "application/json"}} {
		r := response(tc.body, tc.kind)
		if filterResponse(r, tc.path) {
			t.Fatal("unexpected rewrite")
		}
		body, _ := io.ReadAll(r.Body)
		_ = r.Body.Close()
		if string(body) != tc.body {
			t.Fatal("fallback lost body bytes")
		}
	}
}
func TestGzipAndNestedPlayer(t *testing.T) {
	var buf bytes.Buffer
	writer := gzip.NewWriter(&buf)
	_, _ = writer.Write([]byte(`{"playerResponse":{"adSlots":[1],"streamingData":{"url":"x"}},"other":{"adSlots":[42]}}`))
	_ = writer.Close()
	r := response(buf.String(), "application/json")
	r.Header.Set("Content-Encoding", "gzip")
	if !filterResponse(r, "/youtubei/v2/next") {
		t.Fatal("gzip not filtered")
	}
	body, _ := io.ReadAll(r.Body)
	if r.Header.Get("Content-Encoding") != "" || bytes.Contains(body, []byte(`"adSlots":[1]`)) || !bytes.Contains(body, []byte(`"adSlots":[42]`)) {
		t.Fatalf("nested rewrite: %s", body)
	}
}
