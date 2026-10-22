package server

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"git.hloth.dev/hloth/buckshot-roulette-macos/backend/internal/download"
	"git.hloth.dev/hloth/buckshot-roulette-macos/backend/internal/upstream/upstreamtest"
)

func downloadApp(t *testing.T, content []byte) *App {
	t.Helper()
	a, _ := newTestApp(t, upstreamtest.Unused(t))
	a.cfg.DownloadsDir = t.TempDir()
	if content != nil {
		if err := os.WriteFile(filepath.Join(a.cfg.DownloadsDir, download.ZipName), content, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return a
}

func TestDownloadNeedsAnActiveLicense(t *testing.T) {
	a := downloadApp(t, []byte("zip"))
	assertRedirect(t, serve(a, http.MethodGet, "/download"), testPublicURL+"/#auth")
	assertRedirect(t, serve(a, http.MethodGet, "/download", "Cookie", licenseHeader(`{"expiresAt":1000000000000}`)), testPublicURL+"/#auth")
	assertRedirect(t, serve(a, http.MethodGet, "/download", "Cookie", "license=tampered.x"), testAPIURL+"logout")
	assertJSONError(t, serve(a, http.MethodGet, "/download", "Cookie", licenseHeader(`{}`)), http.StatusInternalServerError, "Internal server error")

	granted := serve(a, http.MethodGet, "/download/", "Cookie", licenseHeader(validCookieRaw), "Range", "bytes=1-")
	if granted.Code != http.StatusPartialContent || granted.Body.String() != "ip" {
		t.Fatalf("licensed download: %d %q", granted.Code, granted.Body.String())
	}
}

func TestMissingDownloadIsAServerError(t *testing.T) {
	a := downloadApp(t, nil)
	for _, rangeHeader := range []string{"", "bytes=0-1", "bytes=-1"} {
		recorder := serve(a, http.MethodGet, "/download", "Cookie", licenseHeader(validCookieRaw), "Range", rangeHeader)
		assertJSONError(t, recorder, http.StatusInternalServerError, "Internal server error")
		expectHeaders(t, recorder.Header(), map[string]string{"Accept-Ranges": ""})
	}
}
