// Package compat reproduces the parsing and encoding rules of Bun, Elysia and the WHATWG URL
// standard that the old backend's answers depended on, odd corners included.
package compat

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

const alphanumeric = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"

type Pair struct {
	Key   string
	Value string
}

// ParseQuery is Elysia's parser: '+' is a space even when escaped later, and a malformed escape keeps its raw text.
func ParseQuery(query string) []Pair {
	var pairs []Pair
	for segment := range strings.SplitSeq(query, "&") {
		key, value, hasValue := strings.Cut(segment, "=")
		if !hasValue && key == "" {
			continue
		}
		pairs = append(pairs, Pair{decodeQueryComponent(key), decodeQueryComponent(value)})
	}
	return pairs
}

func LastValues(pairs []Pair) map[string]string {
	values := make(map[string]string, len(pairs))
	for _, pair := range pairs {
		values[pair.Key] = pair.Value
	}
	return values
}

func decodeQueryComponent(component string) string {
	component = strings.ReplaceAll(component, "+", " ")
	if decoded, ok := DecodeURIComponent(component); ok {
		return decoded
	}
	return component
}

// Each run of escapes must decode to valid UTF-8 on its own, as in JavaScript.
func DecodeURIComponent(encoded string) (string, bool) {
	if !strings.Contains(encoded, "%") {
		return encoded, true
	}
	var decoded, escaped []byte
	for i := 0; i < len(encoded); i++ {
		if encoded[i] != '%' {
			if !utf8.Valid(escaped) {
				return "", false
			}
			decoded = append(append(decoded, escaped...), encoded[i])
			escaped = escaped[:0]
			continue
		}
		if i+2 >= len(encoded) {
			return "", false
		}
		octet, err := strconv.ParseUint(encoded[i+1:i+3], 16, 8)
		if err != nil {
			return "", false
		}
		escaped = append(escaped, byte(octet))
		i += 2
	}
	if !utf8.Valid(escaped) {
		return "", false
	}
	return string(append(decoded, escaped...)), true
}

func EncodeURIComponent(text string) string {
	return percentEncode(text, alphanumeric+"-_.!~*'()", false)
}

// Same bytes as URLSearchParams.toString(); url.Values.Encode would sort the keys.
func EncodeForm(keysAndValues ...string) string {
	var form strings.Builder
	for i, text := range keysAndValues {
		switch {
		case i%2 == 1:
			form.WriteByte('=')
		case i > 0:
			form.WriteByte('&')
		}
		form.WriteString(percentEncode(text, alphanumeric+"*-._", true))
	}
	return form.String()
}

func percentEncode(text, unescaped string, spaceAsPlus bool) string {
	var encoded strings.Builder
	for i := range len(text) {
		switch c := text[i]; {
		case strings.IndexByte(unescaped, c) >= 0:
			encoded.WriteByte(c)
		case c == ' ' && spaceAsPlus:
			encoded.WriteByte('+')
		default:
			fmt.Fprintf(&encoded, "%%%02X", c)
		}
	}
	return encoded.String()
}
