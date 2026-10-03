package youtube

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"testing"
)

func TestArchiveIsInstallableAndDeterministic(t *testing.T) {
	first, err := Archive()
	if err != nil {
		t.Fatal(err)
	}
	second, err := Archive()
	if err != nil || !bytes.Equal(first, second) {
		t.Fatal("non-deterministic archive")
	}
	reader, err := zip.NewReader(bytes.NewReader(first), int64(len(first)))
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, file := range reader.File {
		names[file.Name] = true
		if file.Name == "3x-ui-youtube/manifest.json" {
			stream, err := file.Open()
			if err != nil {
				t.Fatal(err)
			}
			data, err := io.ReadAll(stream)
			_ = stream.Close()
			if err != nil {
				t.Fatal(err)
			}
			var manifest map[string]any
			if err := json.Unmarshal(data, &manifest); err != nil {
				t.Fatal(err)
			}
			if manifest["manifest_version"] != float64(3) {
				t.Fatal("unsupported manifest")
			}
		}
	}
	for _, name := range []string{"manifest.json", "main.js", "content.js", "content.css", "popup.html", "popup.js", "popup.css", "README.txt"} {
		if !names["3x-ui-youtube/"+name] {
			t.Fatalf("missing extension asset %s", name)
		}
	}
}
