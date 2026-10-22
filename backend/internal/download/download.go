package download

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	ZipName      = "buckshot-roulette-macos-v1.2.zip"
	StallTimeout = time.Minute
	chunkSize    = 128 << 10
)

type rangeKind int

const (
	wholeFile rangeKind = iota
	partialFile
	unsatisfiable
)

type byteRange struct {
	kind  rangeKind
	start int64
	end   int64
}

var singleRangePattern = regexp.MustCompile(`^bytes=(\d*)-(\d*)$`)

// parseRange keeps the old backend's rules: one range lets a browser resume a download,
// anything else it does not understand gets the whole file.
func parseRange(header string, size int64) byteRange {
	match := singleRangePattern.FindStringSubmatch(header)
	if match == nil || match[1] == "" && match[2] == "" {
		return byteRange{kind: wholeFile}
	}
	if match[1] == "" {
		suffix := digitsValue(match[2])
		if suffix == 0 {
			return byteRange{kind: unsatisfiable}
		}
		return byteRange{kind: partialFile, start: int64(max(0, float64(size)-suffix)), end: size - 1}
	}
	start := digitsValue(match[1])
	end := float64(size - 1)
	if match[2] != "" {
		end = min(digitsValue(match[2]), end)
	}
	if start >= float64(size) {
		return byteRange{kind: unsatisfiable}
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

// Serve fails only when the zip cannot be opened, and then before writing anything.
func Serve(w http.ResponseWriter, r *http.Request, dir string, stallTimeout time.Duration) error {
	file, size, err := open(dir)
	if err != nil {
		return err
	}
	defer file.Close()

	header := w.Header()
	header.Set("Accept-Ranges", "bytes")
	// Bun repeats the Range header values joined with ", ", which no single range matches.
	requested := parseRange(strings.Join(r.Header.Values("Range"), ", "), size)
	if requested.kind == unsatisfiable {
		header.Set("Content-Range", "bytes */"+strconv.FormatInt(size, 10))
		header.Set("Content-Length", "0")
		w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
		return nil
	}

	header.Set("Content-Type", "application/zip")
	// Bun leaves the disposition out of HEAD answers.
	if r.Method != http.MethodHead {
		header.Set("Content-Disposition", `filename="`+ZipName+`"`)
	}
	status, start, length := http.StatusOK, int64(0), size
	if requested.kind == partialFile {
		status, start, length = http.StatusPartialContent, requested.start, requested.end-requested.start+1
		header.Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", requested.start, requested.end, size))
	}
	header.Set("Content-Length", strconv.FormatInt(length, 10))
	w.WriteHeader(status)
	if r.Method != http.MethodHead {
		if _, err := file.Seek(start, io.SeekStart); err == nil {
			send(w, file, length, stallTimeout)
		}
	}
	return nil
}

// The server has no WriteTimeout, which a large download would outlast, so each chunk gets its own
// deadline and a client that stopped reading is dropped.
func send(w http.ResponseWriter, file io.Reader, length int64, stallTimeout time.Duration) {
	controller := http.NewResponseController(w)
	for remaining := length; remaining > 0; {
		controller.SetWriteDeadline(time.Now().Add(stallTimeout))
		written, err := io.CopyN(w, file, min(remaining, chunkSize))
		if err != nil {
			return
		}
		remaining -= written
	}
}

func open(dir string) (*os.File, int64, error) {
	file, err := os.Open(filepath.Join(dir, ZipName))
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
