package main

import (
	"strings"
	"unicode/utf8"
)

const upperHex = "0123456789ABCDEF"

type queryPair struct {
	key   string
	value string
}

// Elysia's parser: '+' is a space even when escaped later, and a malformed escape keeps its raw text.
func parseQuery(query string) []queryPair {
	var pairs []queryPair
	for segment := range strings.SplitSeq(query, "&") {
		key, value, hasValue := strings.Cut(segment, "=")
		if !hasValue && key == "" {
			continue
		}
		pairs = append(pairs, queryPair{decodeQueryComponent(key), decodeQueryComponent(value)})
	}
	return pairs
}

func lastValues(pairs []queryPair) map[string]string {
	values := make(map[string]string, len(pairs))
	for _, pair := range pairs {
		values[pair.key] = pair.value
	}
	return values
}

func decodeQueryComponent(component string) string {
	component = strings.ReplaceAll(component, "+", " ")
	if decoded, ok := decodeURIComponent(component); ok {
		return decoded
	}
	return component
}

func decodeURIComponent(encoded string) (string, bool) {
	if !strings.Contains(encoded, "%") {
		return encoded, true
	}
	var decoded strings.Builder
	var run []byte
	flush := func() bool {
		valid := utf8.Valid(run)
		decoded.Write(run)
		run = run[:0]
		return valid
	}
	for i := 0; i < len(encoded); {
		if encoded[i] != '%' {
			if !flush() {
				return "", false
			}
			decoded.WriteByte(encoded[i])
			i++
			continue
		}
		if i+2 >= len(encoded) {
			return "", false
		}
		high, highOK := hexValue(encoded[i+1])
		low, lowOK := hexValue(encoded[i+2])
		if !highOK || !lowOK {
			return "", false
		}
		run = append(run, high<<4|low)
		i += 3
	}
	if !flush() {
		return "", false
	}
	return decoded.String(), true
}

func hexValue(c byte) (byte, bool) {
	switch {
	case '0' <= c && c <= '9':
		return c - '0', true
	case 'a' <= c && c <= 'f':
		return c - 'a' + 10, true
	case 'A' <= c && c <= 'F':
		return c - 'A' + 10, true
	}
	return 0, false
}

func encodeURIComponent(text string) string {
	return percentEncode(text, func(c byte) bool {
		return isASCIIAlphanumeric(c) || strings.IndexByte("-_.!~*'()", c) >= 0
	}, false)
}

// Same bytes as URLSearchParams.toString(); url.Values.Encode would sort the keys.
func encodeForm(pairs ...queryPair) string {
	keep := func(c byte) bool {
		return isASCIIAlphanumeric(c) || strings.IndexByte("*-._", c) >= 0
	}
	parts := make([]string, len(pairs))
	for i, pair := range pairs {
		parts[i] = percentEncode(pair.key, keep, true) + "=" + percentEncode(pair.value, keep, true)
	}
	return strings.Join(parts, "&")
}

func percentEncode(text string, keep func(byte) bool, spaceAsPlus bool) string {
	var encoded strings.Builder
	for i := 0; i < len(text); i++ {
		c := text[i]
		switch {
		case keep(c):
			encoded.WriteByte(c)
		case c == ' ' && spaceAsPlus:
			encoded.WriteByte('+')
		default:
			encoded.WriteByte('%')
			encoded.WriteByte(upperHex[c>>4])
			encoded.WriteByte(upperHex[c&0x0F])
		}
	}
	return encoded.String()
}

func isASCIIAlphanumeric(c byte) bool {
	return 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' || '0' <= c && c <= '9'
}
