package main

import (
	"bytes"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func TestParseByteRange(t *testing.T) {
	const size = 70001
	partial := func(start, end int64) byteRange { return byteRange{kind: partialFile, start: start, end: end} }
	whole := byteRange{kind: wholeFile}
	unsatisfiable := byteRange{kind: unsatisfiableRange}
	cases := map[string]byteRange{
		"":                             whole,
		"bytes=1000-":                  partial(1000, 70000),
		"bytes=10-19":                  partial(10, 19),
		"bytes=0-0":                    partial(0, 0),
		"bytes=70000-":                 partial(70000, 70000),
		"bytes=-100":                   partial(69901, 70000),
		"bytes=-70001":                 partial(0, 70000),
		"bytes=-999999999":             partial(0, 70000),
		"bytes=-0":                     unsatisfiable,
		"bytes=70001-":                 unsatisfiable,
		"bytes=70001-70011":            unsatisfiable,
		"bytes=69996-70101":            partial(69996, 70000),
		"bytes=0-70001":                partial(0, 70000),
		"bytes=0-1,5-6":                whole,
		"bytes=0-1, bytes=5-6":         whole,
		"bytes=9-3":                    whole,
		"bytes=-":                      whole,
		"lines=1-2":                    whole,
		"Bytes=0-1":                    whole,
		"bytes= 0-1":                   whole,
		"bytes=0010-0019":              partial(10, 19),
		"bytes=0-99999999999999999999": partial(0, 70000),
		"bytes=99999999999999999999-":  unsatisfiable,
		"bytes=-99999999999999999999":  partial(0, 70000),
		"bytes=--5":                    whole,
	}
	for header, want := range cases {
		if got := parseByteRange(header, size); got != want {
			t.Errorf("parseByteRange(%q) = %+v, want %+v", header, got, want)
		}
	}
}

func downloadApp(t *testing.T, content []byte) *app {
	t.Helper()
	a, _ := newTestApp(t, noUpstream(t))
	a.cfg.downloadsDir = t.TempDir()
	if content != nil {
		if err := os.WriteFile(filepath.Join(a.cfg.downloadsDir, downloadName), content, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return a
}

func TestDownloadAnswers(t *testing.T) {
	content := bytes.Repeat([]byte("0123456789"), 10)
	a := downloadApp(t, content)
	valid := licenseHeader(validCookieRaw)

	full := serve(a, http.MethodGet, "/download", "Cookie", valid)
	if full.Code != http.StatusOK || !bytes.Equal(full.Body.Bytes(), content) {
		t.Fatalf("full download: %d, %d bytes", full.Code, full.Body.Len())
	}
	expectHeaders(t, full.Header(), map[string]string{
		"Accept-Ranges":       "bytes",
		"Content-Type":        "application/zip",
		"Content-Disposition": `filename="buckshot-roulette-macos-v1.2.zip"`,
		"Content-Length":      "100",
		"Content-Range":       "",
		"Last-Modified":       "",
		"ETag":                "",
	})

	part := serve(a, http.MethodGet, "/download/", "Cookie", valid, "Range", "bytes=10-19", "If-Range", `"stale"`)
	if part.Code != http.StatusPartialContent || part.Body.String() != "0123456789" {
		t.Fatalf("range: %d %q", part.Code, part.Body.String())
	}
	expectHeaders(t, part.Header(), map[string]string{"Content-Range": "bytes 10-19/100", "Content-Length": "10"})

	outside := serve(a, http.MethodGet, "/download", "Cookie", valid, "Range", "bytes=100-")
	if outside.Code != http.StatusRequestedRangeNotSatisfiable || outside.Body.Len() != 0 {
		t.Fatalf("unsatisfiable: %d", outside.Code)
	}
	expectHeaders(t, outside.Header(), map[string]string{"Content-Range": "bytes */100", "Content-Length": "0", "Content-Type": "", "Accept-Ranges": "bytes"})

	head := serve(a, http.MethodHead, "/download", "Cookie", valid)
	expectHeaders(t, head.Header(), map[string]string{"Content-Length": "100", "Content-Type": "application/zip", "Content-Disposition": ""})

	conditional := serve(a, http.MethodGet, "/download", "Cookie", valid, "If-None-Match", "*", "If-Modified-Since", "Fri, 01 Jan 2100 00:00:00 GMT")
	if conditional.Code != http.StatusOK {
		t.Fatalf("conditional headers must be ignored, got %d", conditional.Code)
	}
}

func TestDownloadNeedsAnActiveLicense(t *testing.T) {
	a := downloadApp(t, []byte("zip"))
	assertRedirect(t, serve(a, http.MethodGet, "/download"), testPublicURL+"/#auth")
	assertRedirect(t, serve(a, http.MethodGet, "/download", "Cookie", licenseHeader(`{"expiresAt":1000000000000}`)), testPublicURL+"/#auth")
	assertRedirect(t, serve(a, http.MethodGet, "/download", "Cookie", "license=tampered.x"), testAPIURL+"logout")
	assertJSONError(t, serve(a, http.MethodGet, "/download", "Cookie", licenseHeader(`{}`)), http.StatusInternalServerError, "Internal server error")
}

func TestMissingDownloadIsAServerError(t *testing.T) {
	a := downloadApp(t, nil)
	for _, rangeHeader := range []string{"", "bytes=0-1", "bytes=-1"} {
		recorder := serve(a, http.MethodGet, "/download", "Cookie", licenseHeader(validCookieRaw), "Range", rangeHeader)
		assertJSONError(t, recorder, http.StatusInternalServerError, "Internal server error")
	}
}

func expectHeaders(t *testing.T, header http.Header, want map[string]string) {
	t.Helper()
	for name, value := range want {
		if got := header.Get(name); got != value {
			t.Errorf("%s: %q, want %q", name, got, value)
		}
	}
}
