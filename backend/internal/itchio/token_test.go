package itchio

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"git.hloth.dev/hloth/buckshot-roulette-macos/backend/internal/httpx"
)

func multipartBody(parts ...string) string {
	var body strings.Builder
	for _, part := range parts {
		body.WriteString("--b\r\n" + part + "\r\n")
	}
	body.WriteString("--b--\r\n")
	return body.String()
}

func namedField(name string) string {
	return "Content-Disposition: form-data; name=\"" + name + "\"\r\n\r\n"
}

func TestReadToken(t *testing.T) {
	const (
		form      = "application/x-www-form-urlencoded"
		multipart = "multipart/form-data; boundary=b"
		field     = "Content-Disposition: form-data; name=\"token\"\r\n\r\n"
	)
	cases := []struct {
		name        string
		contentType string
		body        string
		token       string
		err         error
	}{
		{"form", form, "token=owner-p1.1", "owner-p1.1", nil},
		{"form with charset", form + "; charset=UTF-8", "token=a", "a", nil},
		{"form with extra fields", form, "token=a&foo=bar", "a", nil},
		{"form plus and escapes", form, "token=x+a%2Bb%20c", "x a+b c", nil},
		{"form single space", form, "token=+", " ", nil},
		{"form repeated token", form, "token=a&token=b", "", errNoToken},
		{"form empty token", form, "token=", "", errNoToken},
		{"form without token", form, "foo=bar", "", errNoToken},
		{"form empty body", form, "", "", errNoToken},
		{"JSON", "application/json", `{"token":"a","extra":1}`, "a", nil},
		{"JSON number token", "application/json", `{"token":1101}`, "", errNoToken},
		{"JSON empty token", "application/json", `{"token":""}`, "", errNoToken},
		{"JSON wrong key case", "application/json", `{"TOKEN":"a"}`, "", errNoToken},
		{"JSON array", "application/json", `["a"]`, "", errNoToken},
		{"JSON malformed", "application/json", `{"token":`, "", errBodyMalformed},
		{"multipart", multipart, multipartBody(field + "a"), "a", nil},
		{"multipart with other fields", multipart, multipartBody("Content-Disposition: form-data; name=\"x\"\r\n\r\n1", field+"a"), "a", nil},
		{"multipart repeated token", multipart, multipartBody(field+"a", field+"b"), "", errNoToken},
		{"multipart file", multipart, multipartBody("Content-Disposition: form-data; name=\"token\"; filename=\"t.txt\"\r\n\r\na"), "", errNoToken},
		{"multipart JSON object", multipart, multipartBody(field + `{"a":1}`), "", errNoToken},
		{"multipart invalid JSON stays a string", multipart, multipartBody(field + `{bad`), "{bad", nil},
		{"multipart without boundary", "multipart/form-data", multipartBody(field + "a"), "", errBodyMalformed},
		{"multipart parameter without a value", multipart + "; charset", multipartBody(field + "a"), "a", nil},
		{"multipart empty filename", multipart, multipartBody("Content-Disposition: form-data; name=\"token\"; filename=\"\"\r\n\r\na"), "", errNoToken},
		{"multipart token then token.x", multipart, multipartBody(field+"a", namedField("token.x")+"1"), "", errNoToken},
		{"multipart token.x then token", multipart, multipartBody(namedField("token.x")+"1", field+"a"), "", errNoToken},
		{"multipart token.", multipart, multipartBody(field+"a", namedField("token.")+"1"), "", errNoToken},
		{"multipart token[0]", multipart, multipartBody(field+"a", namedField("token[0]")+"1"), "", errNoToken},
		{"multipart token[0].x", multipart, multipartBody(field+"a", namedField("token[0].x")+"1"), "", errNoToken},
		{"multipart token.__proto__ is skipped", multipart, multipartBody(field+"a", namedField("token.__proto__")+"1"), "a", nil},
		{"multipart token[0][1] names another field", multipart, multipartBody(field+"a", namedField("token[0][1]")+"1"), "a", nil},
		{"multipart a.token names another field", multipart, multipartBody(field+"a", namedField("a.token")+"1"), "a", nil},
		{"multipart token[x] names another field", multipart, multipartBody(field+"a", namedField("token[x]")+"1"), "a", nil},
		{"form parameter without a value", form + "; charset", "token=a", "a", nil},
		{"JSON parameter without a value", "application/json; charset", `{"token":"a"}`, "a", nil},
		{"JSON in a type list", "application/json, text/plain", `{"token":"a"}`, "a", nil},
		{"JSON type with trailing text", "application/json garbage", `{"token":"a"}`, "a", nil},
		{"uppercase JSON type", "Application/JSON", `{"token":"a"}`, "a", nil},
		{"text in a type list", "text/plain, application/json", `{"token":"a"}`, "", errBodyType},
		{"form with a byte order mark", form, "\xEF\xBB\xBFtoken=a", "a", nil},
		{"JSON with a byte order mark", "application/json", "\xEF\xBB\xBF" + `{"token":"a"}`, "a", nil},
		{"JSON with two byte order marks", "application/json", "\xEF\xBB\xBF\xEF\xBB\xBF" + `{"token":"a"}`, "", errBodyMalformed},
		{"text", "text/plain", "token=a", "", errBodyType},
		{"no content type", "", "token=a", "", errBodyType},
		{"too large", form, "token=" + strings.Repeat("a", maxBody), "", httpx.ErrBodyTooLarge},
	}
	for _, c := range cases {
		request := httptest.NewRequest(http.MethodPost, "/auth/itch.io/callback", strings.NewReader(c.body))
		if c.contentType != "" {
			request.Header.Set("Content-Type", c.contentType)
		}
		token, err := ReadToken(httptest.NewRecorder(), request)
		if token != c.token || !errors.Is(err, c.err) {
			t.Errorf("%s: got %q, %v; want %q, %v", c.name, token, err, c.token, c.err)
		}
	}
}
