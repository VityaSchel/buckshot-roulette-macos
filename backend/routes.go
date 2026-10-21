package main

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Elysia answered a bad signature with the relative "/logout", which misses Caddy's /api prefix,
// so the cookie was never cleared. Set to false to restore that answer.
const clearBadLicenseThroughAPI = true

type route struct {
	get  http.HandlerFunc
	post http.HandlerFunc
}

type app struct {
	cfg              config
	client           *http.Client
	now              func() time.Time
	steamLicenses    *licenseCache[string]
	itchLicenses     *licenseCache[int64]
	routes           map[string]route
	steamLoginURL    string
	itchLoginURL     string
	badLicenseTarget string
	bodyTimeout      time.Duration
	stallTimeout     time.Duration
}

func newApp(cfg config, client *http.Client, now func() time.Time) *app {
	a := &app{
		cfg:           cfg,
		client:        client,
		now:           now,
		steamLicenses: newLicenseCache[string](maxCachedLicenses, now),
		itchLicenses:  newLicenseCache[int64](maxCachedLicenses, now),
		steamLoginURL: steamLoginURL(cfg),
		itchLoginURL:  itchLoginURL(cfg),
		bodyTimeout:   requestBodyTimeout,
		stallTimeout:  downloadStallTimeout,
	}
	a.badLicenseTarget = "/logout"
	if clearBadLicenseThroughAPI {
		a.badLicenseTarget = cfg.logoutURL
	}
	a.routes = map[string]route{
		"/auth/steam":            {get: a.steamLogin},
		"/auth/steam/callback":   {get: a.steamCallback},
		"/auth/itch.io":          {get: a.itchLogin},
		"/auth/itch.io/callback": {get: a.itchCallbackPage, post: a.itchCallback},
		"/download":              {get: a.download},
		"/logout":                {get: a.logout},
	}
	return a
}

func (a *app) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer recoverAsServerError(w)
	if r.ContentLength != 0 {
		guardRequestBody(w, a.bodyTimeout)
		defer dropUnreadBody(w)
	}
	handler := a.handlerFor(r)
	if handler == nil {
		writeError(w, http.StatusNotFound, "Not found")
		return
	}
	handler(w, r)
}

func (a *app) handlerFor(r *http.Request) http.HandlerFunc {
	route := a.routes[routePath(r.URL.EscapedPath())]
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		return route.get
	case http.MethodPost:
		return route.post
	}
	return nil
}

// routePath resolves dot segments like the WHATWG URL parser in front of Elysia did, never
// percent-decodes, and drops one trailing slash because Elysia matched both spellings.
func routePath(path string) string {
	segments := strings.Split(strings.TrimPrefix(path, "/"), "/")
	resolved := make([]string, 0, len(segments))
	for i, segment := range segments {
		last := i == len(segments)-1
		switch {
		case isDoubleDotSegment(segment):
			if len(resolved) > 0 {
				resolved = resolved[:len(resolved)-1]
			}
			if last {
				resolved = append(resolved, "")
			}
		case isSingleDotSegment(segment):
			if last {
				resolved = append(resolved, "")
			}
		default:
			resolved = append(resolved, segment)
		}
	}
	normalized := "/" + strings.Join(resolved, "/")
	if len(normalized) > 1 {
		normalized = strings.TrimSuffix(normalized, "/")
	}
	return normalized
}

func isSingleDotSegment(segment string) bool {
	return segment == "." || strings.EqualFold(segment, "%2e")
}

func isDoubleDotSegment(segment string) bool {
	switch strings.ToLower(segment) {
	case "..", ".%2e", "%2e.", "%2e%2e":
		return true
	}
	return false
}

// licenseFromRequest answers the request itself when the license cookie is unusable, as Elysia's
// cookie validation did before any handler ran.
func (a *app) licenseFromRequest(w http.ResponseWriter, r *http.Request) (licenseCookie, bool) {
	license, err := readLicenseCookie(r, a.cfg.jwtSecret)
	switch {
	case errors.Is(err, errLicenseSignature):
		redirect(w, a.badLicenseTarget)
		return licenseCookie{}, false
	case err != nil:
		log.Print(err)
		writeError(w, http.StatusInternalServerError, "Internal server error")
		return licenseCookie{}, false
	}
	return license, true
}

// Not http.Redirect: it adds a Content-Type and an HTML body.
func redirect(w http.ResponseWriter, location string) {
	header := w.Header()
	header.Set("Location", location)
	header.Set("Content-Length", "0")
	w.WriteHeader(http.StatusFound)
}

type errorBody struct {
	OK    bool   `json:"ok"`
	Error string `json:"error"`
}

func writeError(w http.ResponseWriter, status int, message string) {
	body, _ := json.Marshal(errorBody{OK: false, Error: message})
	writeBody(w, status, "application/json", body)
}

func writeBody(w http.ResponseWriter, status int, contentType string, body []byte) {
	header := w.Header()
	header.Set("Content-Type", contentType)
	header.Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(status)
	w.Write(body)
}

func recoverAsServerError(w http.ResponseWriter) {
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
	writeError(w, http.StatusInternalServerError, "Internal server error")
}
