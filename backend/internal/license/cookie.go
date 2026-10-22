package license

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

	"git.hloth.dev/hloth/buckshot-roulette-macos/backend/internal/compat"
)

const (
	cookieName = "license"
	lifetime   = 7 * 24 * time.Hour
	// The site is HTTPS only, and Lax is what browsers already assumed.
	securityAttributes = "; Secure; SameSite=Lax"
	ClearCookie        = cookieName + "=; Max-Age=0; Path=/; Expires=Thu, 01 Jan 1970 00:00:00 GMT" + securityAttributes
)

var (
	ErrSignature = errors.New("license cookie signature is invalid")
	ErrPayload   = errors.New("license cookie payload is invalid")
)

var decimalNumberPattern = regexp.MustCompile(`^[+-]?(\d+\.?\d*|\.\d+)([eE][+-]?\d+)?$`)

type Cookie struct {
	present   bool
	expiresAt float64
}

func (c Cookie) ActiveAt(now time.Time) bool {
	return c.present && c.expiresAt > float64(now.UnixMilli())
}

func Sent(r *http.Request) bool {
	_, found := compat.Cookie(r, cookieName)
	return found
}

// Elysia decodes a cookie twice: the cookie package keeps the raw text when decoding fails,
// the second pass rejects it, which the license cookie reports as a bad signature.
func Read(r *http.Request, secret []byte) (Cookie, error) {
	raw, found := compat.Cookie(r, cookieName)
	if !found {
		return Cookie{}, nil
	}
	firstPass, ok := compat.DecodeURIComponent(raw)
	if !ok {
		firstPass = raw
	}
	signed, decoded := compat.DecodeURIComponent(firstPass)
	value, verified := unsign(signed, secret)
	if !decoded || !verified {
		return Cookie{}, ErrSignature
	}
	expiresAt, err := parsePayload(value)
	if err != nil {
		return Cookie{}, err
	}
	return Cookie{present: true, expiresAt: expiresAt}, nil
}

func Issue(now time.Time, secret []byte) string {
	expiresAt := now.Add(lifetime).UnixMilli()
	payload := `{"expiresAt":` + strconv.FormatInt(expiresAt, 10) + `}`
	maxAge := strconv.Itoa(int(lifetime / time.Second))
	return cookieName + "=" + compat.EncodeURIComponent(sign(payload, secret)) +
		"; Max-Age=" + maxAge + "; Path=/; HttpOnly" + securityAttributes
}

func sign(value string, secret []byte) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(value))
	return value + "." + base64.RawStdEncoding.EncodeToString(mac.Sum(nil))
}

// The whole re-signed string is compared: decoding the presented base64 would also accept
// non-canonical spellings of the same bytes, which Elysia rejects.
func unsign(signed string, secret []byte) (string, bool) {
	dot := strings.LastIndexByte(signed, '.')
	if dot < 0 {
		return "", false
	}
	value := signed[:dot]
	return value, hmac.Equal([]byte(sign(value, secret)), []byte(signed))
}

// Elysia's schema took expiresAt as a finite number or a numeric string; it only ever issued integers.
func parsePayload(value string) (float64, error) {
	if !strings.HasPrefix(value, "{") || !strings.HasSuffix(value, "}") {
		return 0, ErrPayload
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(value), &fields); err != nil {
		return 0, ErrPayload
	}
	number, ok := numericText(fields["expiresAt"])
	if !ok {
		return 0, ErrPayload
	}
	expiresAt, err := strconv.ParseFloat(number, 64)
	if err != nil {
		return 0, ErrPayload
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
