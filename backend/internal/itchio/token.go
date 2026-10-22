package itchio

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

	"git.hloth.dev/hloth/buckshot-roulette-macos/backend/internal/compat"
	"git.hloth.dev/hloth/buckshot-roulette-macos/backend/internal/httpx"
)

const (
	tokenField = "token"
	maxBody    = 64 << 10
)

var (
	errBodyType      = errors.New("unsupported content type")
	errBodyMalformed = errors.New("malformed body")
	errNoToken       = errors.New("no single non-empty token")
)

var arrayFieldPattern = regexp.MustCompile(`^(.+)\[(\d+)\]$`)

func ReadToken(w http.ResponseWriter, r *http.Request) (string, error) {
	mediaType, params := compat.MediaType(r.Header.Get("Content-Type"))
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
	body, err := httpx.ReadBody(w, r, maxBody)
	if err != nil {
		return "", err
	}
	token, err := parseToken(body)
	if err == nil && token == "" {
		err = errNoToken
	}
	return token, err
}

func formToken(body []byte) (string, error) {
	var tokens []string
	for _, pair := range compat.ParseQuery(string(compat.WithoutBOM(body))) {
		if pair.Key == tokenField {
			tokens = append(tokens, pair.Value)
		}
	}
	return singleToken(tokens)
}

func jsonToken(body []byte) (string, error) {
	document, err := compat.DecodeJSON(body)
	if err != nil {
		return "", errBodyMalformed
	}
	token, ok := compat.Field(document, tokenField).(string)
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
		if name != tokenField {
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
	return segments[0] == tokenField || arrayFieldName(segments[0]) == tokenField
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
