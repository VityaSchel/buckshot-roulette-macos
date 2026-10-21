package main

import (
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	downloadName         = "buckshot-roulette-macos-v1.2.zip"
	downloadType         = "application/zip"
	downloadDisposition  = `filename="` + downloadName + `"`
	downloadChunk        = 128 << 10
	downloadStallTimeout = time.Minute
)

type byteRangeKind int

const (
	wholeFile byteRangeKind = iota
	partialFile
	unsatisfiableRange
)

type byteRange struct {
	kind  byteRangeKind
	start int64
	end   int64
}

var singleRangePattern = regexp.MustCompile(`^bytes=(\d*)-(\d*)$`)

// parseByteRange keeps the old backend's rules: one range lets a browser resume a download,
// anything else it does not understand gets the whole file.
func parseByteRange(header string, size int64) byteRange {
	match := singleRangePattern.FindStringSubmatch(header)
	if match == nil || match[1] == "" && match[2] == "" {
		return byteRange{kind: wholeFile}
	}
	if match[1] == "" {
		suffix := digitsValue(match[2])
		if suffix == 0 {
			return byteRange{kind: unsatisfiableRange}
		}
		return byteRange{kind: partialFile, start: int64(max(0, float64(size)-suffix)), end: size - 1}
	}
	start := digitsValue(match[1])
	end := float64(size - 1)
	if match[2] != "" {
		end = min(digitsValue(match[2]), end)
	}
	if start >= float64(size) {
		return byteRange{kind: unsatisfiableRange}
	}
	if end < start {
		return byteRange{kind: wholeFile}
	}
	return byteRange{kind: partialFile, start: int64(start), end: int64(end)}
}

// Number() of a digit string: a huge value rounds instead of failing.
func digitsValue(digits string) float64 {
	value, _ := strconv.ParseFloat(digits, 64)
	return value
}

func (a *app) download(w http.ResponseWriter, r *http.Request) {
	license, ok := a.licenseFromRequest(w, r)
	if !ok {
		return
	}
	if !license.activeAt(a.now()) {
		redirect(w, a.cfg.frontendURL("auth"))
		return
	}
	file, size, err := openDownload(a.cfg.downloadsDir)
	if err != nil {
		log.Printf("download: %v", err)
		writeError(w, http.StatusInternalServerError, "Internal server error")
		return
	}
	defer file.Close()

	header := w.Header()
	header.Set("Accept-Ranges", "bytes")
	// Bun repeats the Range header values joined with ", ", which no single range matches.
	requested := parseByteRange(strings.Join(r.Header.Values("Range"), ", "), size)
	if requested.kind == unsatisfiableRange {
		header.Set("Content-Range", "bytes */"+strconv.FormatInt(size, 10))
		header.Set("Content-Length", "0")
		w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
		return
	}

	header.Set("Content-Type", downloadType)
	// Bun leaves the disposition out of HEAD answers.
	if r.Method != http.MethodHead {
		header.Set("Content-Disposition", downloadDisposition)
	}
	status, start, length := http.StatusOK, int64(0), size
	if requested.kind == partialFile {
		status, start, length = http.StatusPartialContent, requested.start, requested.end-requested.start+1
		header.Set("Content-Range", "bytes "+strconv.FormatInt(requested.start, 10)+"-"+strconv.FormatInt(requested.end, 10)+"/"+strconv.FormatInt(size, 10))
	}
	header.Set("Content-Length", strconv.FormatInt(length, 10))
	w.WriteHeader(status)
	if r.Method == http.MethodHead {
		return
	}
	if _, err := file.Seek(start, io.SeekStart); err != nil {
		return
	}
	sendFile(w, file, length, a.stallTimeout)
}

// The server has no WriteTimeout, which a large download would outlast, so each chunk gets its own
// deadline and a client that stopped reading is dropped.
func sendFile(w http.ResponseWriter, file *os.File, length int64, stallTimeout time.Duration) {
	controller := http.NewResponseController(w)
	for remaining := length; remaining > 0; {
		controller.SetWriteDeadline(time.Now().Add(stallTimeout))
		written, err := io.CopyN(w, file, min(remaining, downloadChunk))
		if err != nil {
			return
		}
		remaining -= written
	}
}

func openDownload(dir string) (*os.File, int64, error) {
	file, err := os.Open(filepath.Join(dir, downloadName))
	if err != nil {
		return nil, 0, errors.New("the zip cannot be opened")
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		file.Close()
		return nil, 0, errors.New("the zip is not a regular file")
	}
	return file, info.Size(), nil
}
