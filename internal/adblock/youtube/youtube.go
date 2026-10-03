// Package youtube distributes the offline Chromium companion extension.
package youtube

import (
	"archive/zip"
	"bytes"
	"embed"
	"io/fs"
	"time"
)

//go:embed assets/*
var files embed.FS

func Archive() ([]byte, error) {
	var output bytes.Buffer
	writer := zip.NewWriter(&output)
	entries, err := fs.ReadDir(files, "assets")
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		data, err := files.ReadFile("assets/" + entry.Name())
		if err != nil {
			return nil, err
		}
		header := &zip.FileHeader{Name: "3x-ui-youtube/" + entry.Name(), Method: zip.Deflate}
		header.SetModTime(time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC))
		file, err := writer.CreateHeader(header)
		if err != nil {
			return nil, err
		}
		if _, err := file.Write(data); err != nil {
			return nil, err
		}
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}
