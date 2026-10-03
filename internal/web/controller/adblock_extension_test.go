package controller

import (
	"archive/zip"
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestAdBlockExtensionDownload(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	a := &AdBlockController{}
	router.GET("/download", a.youtubeExtension)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/download", nil))
	if recorder.Code != http.StatusOK || recorder.Header().Get("Content-Type") != "application/zip" || recorder.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("bad download: %d %v", recorder.Code, recorder.Header())
	}
	if _, err := zip.NewReader(bytes.NewReader(recorder.Body.Bytes()), int64(recorder.Body.Len())); err != nil {
		t.Fatal(err)
	}
}

func TestAdBlockServerRoutingDownload(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	a := &AdBlockController{}
	router.GET("/routing/:core", a.youtubeServerRouting)
	for _, core := range []string{"xray", "singbox"} {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/routing/"+core, nil))
		if recorder.Code != 200 || recorder.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("preset failed: %d", recorder.Code)
		}
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/routing/unknown", nil))
	if recorder.Code != 400 {
		t.Fatal("unknown core accepted")
	}
}
