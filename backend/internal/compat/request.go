package compat

import (
	"errors"
	"mime"
	"net/http"
	"strings"
)

// RoutePath resolves dot segments like the WHATWG URL parser in front of Elysia did, never
// percent-decodes, and drops one trailing slash because Elysia matched both spellings.
func RoutePath(path string) string {
	segments := strings.Split(strings.TrimPrefix(path, "/"), "/")
	resolved := make([]string, 0, len(segments))
	for i, segment := range segments {
		last := i == len(segments)-1
		switch strings.ToLower(segment) {
		case "..", ".%2e", "%2e.", "%2e%2e":
			if len(resolved) > 0 {
				resolved = resolved[:len(resolved)-1]
			}
			if last {
				resolved = append(resolved, "")
			}
		case ".", "%2e":
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

// Bun hands Elysia every Cookie header joined with "; ", and Elysia keeps the first pair with a name.
func Cookie(r *http.Request, name string) (string, bool) {
	header := strings.Join(r.Header.Values("Cookie"), "; ")
	for pair := range strings.SplitSeq(header, ";") {
		key, value, ok := strings.Cut(pair, "=")
		if ok && strings.Trim(key, " \t") == name {
			return strings.Trim(value, " \t"), true
		}
	}
	return "", false
}

// Elysia looked at the start of the raw header only, so a broken parameter or a list after the
// type did not stop it.
func MediaType(header string) (string, map[string]string) {
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
