package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	licenseCookieName = "license"
	licenseLifetime   = 7 * 24 * time.Hour
	// The site is HTTPS only, and Lax is what browsers already assumed.
	cookieSecurityAttributes = "; Secure; SameSite=Lax"
	clearLicenseCookie       = licenseCookieName + "=; Max-Age=0; Path=/; Expires=Thu, 01 Jan 1970 00:00:00 GMT" + cookieSecurityAttributes
)

var (
	errLicenseSignature = errors.New("license cookie signature is invalid")
	errLicensePayload   = errors.New("license cookie payload is invalid")
)

type licenseCookie struct {
	present   bool
	expiresAt float64
}

func (l licenseCookie) activeAt(now time.Time) bool {
	return l.present && l.expiresAt > float64(now.UnixMilli())
}

// Bun hands Elysia every Cookie header joined with "; ", and Elysia keeps the first pair with a name.
func requestCookie(r *http.Request, name string) (string, bool) {
	header := strings.Join(r.Header.Values("Cookie"), "; ")
	for pair := range strings.SplitSeq(header, ";") {
		key, value, ok := strings.Cut(pair, "=")
		if ok && trimCookieSpace(key) == name {
			return trimCookieSpace(value), true
		}
	}
	return "", false
}

func trimCookieSpace(text string) string {
	return strings.Trim(text, " \t")
}

// Elysia decodes a cookie twice: the cookie package keeps the raw text when decoding fails,
// the second pass rejects it, which the license cookie reports as a bad signature.
func readLicenseCookie(r *http.Request, secret []byte) (licenseCookie, error) {
	raw, found := requestCookie(r, licenseCookieName)
	if !found {
		return licenseCookie{}, nil
	}
	firstPass, ok := decodeURIComponent(raw)
	if !ok {
		firstPass = raw
	}
	signed, ok := decodeURIComponent(firstPass)
	if !ok {
		return licenseCookie{}, errLicenseSignature
	}
	value, ok := unsignCookieValue(signed, secret)
	if !ok {
		return licenseCookie{}, errLicenseSignature
	}
	expiresAt, err := parseLicensePayload(value)
	if err != nil {
		return licenseCookie{}, err
	}
	return licenseCookie{present: true, expiresAt: expiresAt}, nil
}

func signCookieValue(value string, secret []byte) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(value))
	return value + "." + base64.RawStdEncoding.EncodeToString(mac.Sum(nil))
}

// The whole re-signed string is compared: decoding the presented base64 would also accept
// non-canonical spellings of the same bytes, which Elysia rejects.
func unsignCookieValue(signed string, secret []byte) (string, bool) {
	dot := strings.LastIndexByte(signed, '.')
	if dot < 0 {
		return "", false
	}
	value := signed[:dot]
	return value, hmac.Equal([]byte(signCookieValue(value, secret)), []byte(signed))
}

func issueLicenseCookie(now time.Time, secret []byte) string {
	expiresAt := now.Add(licenseLifetime).UnixMilli()
	payload := `{"expiresAt":` + strconv.FormatInt(expiresAt, 10) + `}`
	maxAge := strconv.Itoa(int(licenseLifetime / time.Second))
	return licenseCookieName + "=" + encodeURIComponent(signCookieValue(payload, secret)) +
		"; Max-Age=" + maxAge + "; Path=/; HttpOnly" + cookieSecurityAttributes
}

var decimalNumberPattern = regexp.MustCompile(`^[+-]?(\d+\.?\d*|\.\d+)([eE][+-]?\d+)?$`)

// Elysia's schema took expiresAt as a finite number or a numeric string; it only ever issued integers.
func parseLicensePayload(value string) (float64, error) {
	if !strings.HasPrefix(value, "{") || !strings.HasSuffix(value, "}") {
		return 0, errLicensePayload
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(value), &fields); err != nil {
		return 0, errLicensePayload
	}
	number, ok := numericText(fields["expiresAt"])
	if !ok {
		return 0, errLicensePayload
	}
	expiresAt, err := strconv.ParseFloat(number, 64)
	if err != nil {
		return 0, errLicensePayload
	}
	return expiresAt, nil
}

func numericText(raw json.RawMessage) (string, bool) {
	if len(raw) == 0 {
		return "", false
	}
	if raw[0] == '-' || '0' <= raw[0] && raw[0] <= '9' {
		return string(raw), true
	}
	var text string
	if raw[0] != '"' || json.Unmarshal(raw, &text) != nil {
		return "", false
	}
	text = strings.TrimSpace(text)
	return text, decimalNumberPattern.MatchString(text)
}
