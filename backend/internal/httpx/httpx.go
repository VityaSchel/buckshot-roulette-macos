package httpx

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"runtime"
	"strconv"
	"time"
)

// Not http.Redirect: it adds a Content-Type and an HTML body.
func Redirect(w http.ResponseWriter, location string) {
	header := w.Header()
	header.Set("Location", location)
	header.Set("Content-Length", "0")
	w.WriteHeader(http.StatusFound)
}

func Error(w http.ResponseWriter, status int, message string) {
	body, _ := json.Marshal(struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}{false, message})
	WriteBody(w, status, "application/json", body)
}

func ServerError(w http.ResponseWriter) {
	Error(w, http.StatusInternalServerError, "Internal server error")
}

func WriteBody(w http.ResponseWriter, status int, contentType string, body []byte) {
	header := w.Header()
	header.Set("Content-Type", contentType)
	header.Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(status)
	w.Write(body)
}

func Recover(w http.ResponseWriter) {
	recovered := recover()
	if recovered == nil {
		return
	}
	if recovered == http.ErrAbortHandler {
		panic(recovered)
	}
	if runtimeErr, ok := recovered.(runtime.Error); ok {
		log.Printf("handler panic: %v", runtimeErr)
	} else {
		log.Print("handler panic")
	}
	ServerError(w)
}

var (
	ErrBodyTooLarge = errors.New("body too large")
	ErrBodyRead     = errors.New("body could not be read")
)

// net/http drains an unread body before and after the answer under the connection's read deadline,
// which is otherwise unset, so a body that never arrives would hold the connection for good.
func GuardBody(w http.ResponseWriter, timeout time.Duration) {
	w.Header().Set("Connection", "close")
	http.NewResponseController(w).SetReadDeadline(time.Now().Add(timeout))
}

func DropUnreadBody(w http.ResponseWriter) {
	if w.Header().Get("Connection") == "close" {
		http.NewResponseController(w).SetReadDeadline(time.Now())
	}
}

func ReadBody(w http.ResponseWriter, r *http.Request, limit int64) ([]byte, error) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, limit))
	var tooLarge *http.MaxBytesError
	switch {
	case errors.As(err, &tooLarge):
		return nil, ErrBodyTooLarge
	case err != nil:
		return nil, ErrBodyRead
	}
	// The deadline is for the body only; the license check that follows may take longer.
	w.Header().Del("Connection")
	http.NewResponseController(w).SetReadDeadline(time.Time{})
	return body, nil
}
