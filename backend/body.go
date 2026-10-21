package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"time"
)

const (
	maxRequestBody     = 64 << 10
	requestBodyTimeout = 30 * time.Second
	itchTokenField     = "token"
)

var (
	errBodyType      = errors.New("unsupported content type")
	errBodyTooLarge  = errors.New("body too large")
	errBodyRead      = errors.New("body could not be read")
	errBodyMalformed = errors.New("malformed body")
	errNoToken       = errors.New("no single non-empty token")
)

var utf8BOM = []byte("\xEF\xBB\xBF")

// net/http drains an unread body before and after the answer under the connection's read deadline,
// which is otherwise unset, so a body that never arrives would hold the connection for good.
func guardRequestBody(w http.ResponseWriter, timeout time.Duration) {
	w.Header().Set("Connection", "close")
	http.NewResponseController(w).SetReadDeadline(time.Now().Add(timeout))
}

// The deadline is for the body only; the license check that follows may take longer.
func releaseRequestBody(w http.ResponseWriter) {
	w.Header().Del("Connection")
	http.NewResponseController(w).SetReadDeadline(time.Time{})
}

func dropUnreadBody(w http.ResponseWriter) {
	if w.Header().Get("Connection") == "close" {
		http.NewResponseController(w).SetReadDeadline(time.Now())
	}
}

func readItchToken(w http.ResponseWriter, r *http.Request) (string, error) {
	mediaType, params := requestMediaType(r.Header.Get("Content-Type"))
	var parseToken func([]byte) (string, error)
	switch mediaType {
	case "application/x-www-form-urlencoded":
		parseToken = formToken
	case "application/json":
		parseToken = jsonToken
	case "multipart/form-data":
		parseToken = func(body []byte) (string, error) { return multipartToken(body, params["boundary"]) }
	default:
		return "", errBodyType
	}
	body, err := readRequestBody(w, r)
	if err != nil {
		return "", err
	}
	token, err := parseToken(body)
	if err == nil && token == "" {
		err = errNoToken
	}
	return token, err
}

// Elysia looked at the start of the raw header only, so a broken parameter or a list after the
// type did not stop it.
func requestMediaType(header string) (string, map[string]string) {
	mediaType, params, err := mime.ParseMediaType(header)
	if err == nil {
		return mediaType, params
	}
	if errors.Is(err, mime.ErrInvalidMediaParameter) {
		return mediaType, lenientParams(header)
	}
	essence := strings.TrimLeft(header, " \t")
	if end := strings.IndexAny(essence, " \t;,"); end >= 0 {
		essence = essence[:end]
	}
	return strings.ToLower(essence), lenientParams(header)
}

func lenientParams(header string) map[string]string {
	params := map[string]string{}
	segments := strings.Split(header, ";")
	for _, segment := range segments[1:] {
		_, single, err := mime.ParseMediaType("x/x;" + segment)
		if err != nil {
			continue
		}
		for name, value := range single {
			if _, seen := params[name]; !seen {
				params[name] = value
			}
		}
	}
	return params
}

func readRequestBody(w http.ResponseWriter, r *http.Request) ([]byte, error) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxRequestBody))
	var tooLarge *http.MaxBytesError
	switch {
	case errors.As(err, &tooLarge):
		return nil, errBodyTooLarge
	case err != nil:
		return nil, errBodyRead
	}
	releaseRequestBody(w)
	return body, nil
}

// Bun's text() and json() drop one leading byte order mark, as the WHATWG UTF-8 decoder does.
func withoutBOM(data []byte) []byte {
	return bytes.TrimPrefix(data, utf8BOM)
}

func formToken(body []byte) (string, error) {
	var tokens []string
	for _, pair := range parseQuery(string(withoutBOM(body))) {
		if pair.key == itchTokenField {
			tokens = append(tokens, pair.value)
		}
	}
	return singleToken(tokens)
}

func jsonToken(body []byte) (string, error) {
	document, err := decodeJSON(body)
	if err != nil {
		return "", errBodyMalformed
	}
	object, _ := jsonObject(document)
	token, ok := object[itchTokenField].(string)
	if !ok {
		return "", errNoToken
	}
	return token, nil
}

func multipartToken(body []byte, boundary string) (string, error) {
	reader := multipart.NewReader(bytes.NewReader(body), boundary)
	var tokens []string
	for {
		part, err := reader.NextRawPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", errBodyMalformed
		}
		name := part.FormName()
		if overwritesToken(name) {
			return "", errNoToken
		}
		if name != itchTokenField {
			continue
		}
		if isFilePart(part) {
			return "", errNoToken
		}
		value, err := io.ReadAll(part)
		if err != nil {
			return "", errBodyMalformed
		}
		tokens = append(tokens, string(value))
	}
	token, err := singleToken(tokens)
	if err == nil && isJSONStructure(token) {
		return "", errNoToken
	}
	return token, err
}

// Bun's FormData holds a part with a filename parameter, even an empty one, as a File.
func isFilePart(part *multipart.Part) bool {
	_, params, err := mime.ParseMediaType(part.Header.Get("Content-Disposition"))
	_, hasFilename := params["filename"]
	return err == nil && hasFilename
}

var arrayFieldPattern = regexp.MustCompile(`^(.+)\[(\d+)\]$`)

// Elysia's form-to-object step turned body.token into an object for "token.x" and into an array for
// "token[0]", but skipped a name with a segment like "__proto__".
func overwritesToken(name string) bool {
	if !strings.ContainsAny(name, ".[") {
		return false
	}
	segments := strings.Split(name, ".")
	if slices.ContainsFunc(segments, isDangerousFieldSegment) {
		return false
	}
	return segments[0] == itchTokenField || arrayFieldName(segments[0]) == itchTokenField
}

func arrayFieldName(segment string) string {
	if match := arrayFieldPattern.FindStringSubmatch(segment); match != nil {
		return match[1]
	}
	return ""
}

func isDangerousFieldSegment(segment string) bool {
	dangerous := []string{"__proto__", "constructor", "prototype"}
	return slices.Contains(dangerous, segment) || slices.Contains(dangerous, arrayFieldName(segment))
}

func singleToken(tokens []string) (string, error) {
	if len(tokens) != 1 {
		return "", errNoToken
	}
	return tokens[0], nil
}

// Elysia turned a multipart field holding a JSON object or array into that value, which then failed the string schema.
func isJSONStructure(value string) bool {
	return (strings.HasPrefix(value, "{") || strings.HasPrefix(value, "[")) && json.Valid([]byte(value))
}
