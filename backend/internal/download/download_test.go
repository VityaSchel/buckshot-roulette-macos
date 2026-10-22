package download

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestParseRange(t *testing.T) {
	const size = 70001
	partial := func(start, end int64) byteRange { return byteRange{kind: partialFile, start: start, end: end} }
	whole := byteRange{kind: wholeFile}
	outside := byteRange{kind: unsatisfiable}
	cases := map[string]byteRange{
		"":                             whole,
		"bytes=1000-":                  partial(1000, 70000),
		"bytes=10-19":                  partial(10, 19),
		"bytes=0-0":                    partial(0, 0),
		"bytes=70000-":                 partial(70000, 70000),
		"bytes=-100":                   partial(69901, 70000),
		"bytes=-70001":                 partial(0, 70000),
		"bytes=-999999999":             partial(0, 70000),
		"bytes=-0":                     outside,
		"bytes=70001-":                 outside,
		"bytes=70001-70011":            outside,
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
		"bytes=99999999999999999999-":  outside,
		"bytes=-99999999999999999999":  partial(0, 70000),
		"bytes=--5":                    whole,
	}
	for header, want := range cases {
		if got := parseRange(header, size); got != want {
			t.Errorf("parseRange(%q) = %+v, want %+v", header, got, want)
		}
	}
}

func zipDir(t *testing.T, content []byte) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ZipName), content, 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func serve(t *testing.T, dir, method string, headers ...string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, "/download", nil)
	for i := 0; i+1 < len(headers); i += 2 {
		request.Header.Add(headers[i], headers[i+1])
	}
	recorder := httptest.NewRecorder()
	if err := Serve(recorder, request, dir, time.Minute); err != nil {
		t.Fatal(err)
	}
	return recorder
}

func expectHeaders(t *testing.T, header http.Header, want map[string]string) {
	t.Helper()
	for name, value := range want {
		if got := header.Get(name); got != value {
			t.Errorf("%s: %q, want %q", name, got, value)
		}
	}
}

func TestServe(t *testing.T) {
	content := bytes.Repeat([]byte("0123456789"), 10)
	dir := zipDir(t, content)

	full := serve(t, dir, http.MethodGet)
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

	part := serve(t, dir, http.MethodGet, "Range", "bytes=10-19", "If-Range", `"stale"`)
	if part.Code != http.StatusPartialContent || part.Body.String() != "0123456789" {
		t.Fatalf("range: %d %q", part.Code, part.Body.String())
	}
	expectHeaders(t, part.Header(), map[string]string{"Content-Range": "bytes 10-19/100", "Content-Length": "10"})

	repeated := serve(t, dir, http.MethodGet, "Range", "bytes=10-19", "Range", "bytes=20-29")
	if repeated.Code != http.StatusOK || repeated.Body.Len() != 100 {
		t.Fatalf("repeated Range headers: %d, %d bytes", repeated.Code, repeated.Body.Len())
	}

	outside := serve(t, dir, http.MethodGet, "Range", "bytes=100-")
	if outside.Code != http.StatusRequestedRangeNotSatisfiable || outside.Body.Len() != 0 {
		t.Fatalf("unsatisfiable: %d", outside.Code)
	}
	expectHeaders(t, outside.Header(), map[string]string{"Content-Range": "bytes */100", "Content-Length": "0", "Content-Type": "", "Accept-Ranges": "bytes"})

	head := serve(t, dir, http.MethodHead)
	expectHeaders(t, head.Header(), map[string]string{"Content-Length": "100", "Content-Type": "application/zip", "Content-Disposition": ""})
	if head.Body.Len() != 0 {
		t.Fatalf("HEAD sent %d bytes", head.Body.Len())
	}

	conditional := serve(t, dir, http.MethodGet, "If-None-Match", "*", "If-Modified-Since", "Fri, 01 Jan 2100 00:00:00 GMT")
	if conditional.Code != http.StatusOK {
		t.Fatalf("conditional headers must be ignored, got %d", conditional.Code)
	}
}

func TestServeFailsBeforeWritingWithoutTheZip(t *testing.T) {
	notRegular := t.TempDir()
	if err := os.Mkdir(filepath.Join(notRegular, ZipName), 0o755); err != nil {
		t.Fatal(err)
	}
	cases := map[string]string{
		"the zip cannot be opened":      t.TempDir(),
		"the zip is not a regular file": notRegular,
	}
	for want, dir := range cases {
		recorder := httptest.NewRecorder()
		err := Serve(recorder, httptest.NewRequest(http.MethodGet, "/download", nil), dir, time.Minute)
		if err == nil || err.Error() != want {
			t.Errorf("error %v, want %q", err, want)
		}
		if len(recorder.Header()) != 0 || recorder.Body.Len() != 0 {
			t.Errorf("%s: wrote %v %q", want, recorder.Header(), recorder.Body.String())
		}
	}
}
