package steam

import (
	"errors"
	"strings"
	"testing"

	"git.hloth.dev/hloth/buckshot-roulette-macos/backend/internal/compat"
)

const (
	testSteamID       = "76561197960010001"
	testAPIURL        = "https://buckshot-roulette-macos.hloth.dev/api/"
	testCallbackURL   = testAPIURL + "auth/steam/callback"
	validSignedFields = "signed,op_endpoint,claimed_id,identity,return_to,response_nonce,assoc_handle"
	openIDValidAnswer = "ns:http://specs.openid.net/auth/2.0\nis_valid:true\n"
)

func validAssertion() assertion {
	claimedID := claimedIDBase + testSteamID
	return assertion{
		namespace:     openIDNamespace,
		mode:          "id_res",
		opEndpoint:    openIDEndpoint,
		claimedID:     claimedID,
		identity:      claimedID,
		returnTo:      testCallbackURL,
		responseNonce: "2026-10-20T12:00:00Zparity",
		assocHandle:   "1234567890",
		signed:        validSignedFields,
		signature:     "Kq3+/parity=",
	}
}

func (a assertion) query() string {
	return compat.EncodeForm(
		"openid.ns", a.namespace,
		"openid.mode", a.mode,
		"openid.op_endpoint", a.opEndpoint,
		"openid.claimed_id", a.claimedID,
		"openid.identity", a.identity,
		"openid.return_to", a.returnTo,
		"openid.response_nonce", a.responseNonce,
		"openid.assoc_handle", a.assocHandle,
		"openid.signed", a.signed,
		"openid.sig", a.signature,
	)
}

func TestAssertionValidation(t *testing.T) {
	cases := []struct {
		name   string
		change func(*assertion)
		err    error
	}{
		{"valid", func(*assertion) {}, nil},
		{"return_to with a query Steam appended", func(a *assertion) { a.returnTo += "?steam=1" }, nil},
		{"extra signed fields", func(a *assertion) { a.signed += ",ns,foo" }, nil},
		{"signed fields in another order", func(a *assertion) {
			a.signed = "assoc_handle,response_nonce,return_to,identity,claimed_id,op_endpoint"
		}, nil},
		{"other namespace", func(a *assertion) { a.namespace = "urn:other" }, ErrState},
		{"setup mode", func(a *assertion) { a.mode = "checkid_setup" }, ErrState},
		{"cancel mode", func(a *assertion) { a.mode = "cancel" }, ErrState},
		{"other OP endpoint", func(a *assertion) { a.opEndpoint = "https://evil.example/openid/login" }, ErrState},
		{"return_to on another host", func(a *assertion) { a.returnTo = "https://evil.example/api/auth/steam/callback" }, ErrState},
		{"return_to on another path", func(a *assertion) { a.returnTo = testAPIURL + "download" }, ErrState},
		{"return_to with a longer path", func(a *assertion) { a.returnTo = testCallbackURL + "x" }, ErrState},
		{"return_to with a fragment", func(a *assertion) { a.returnTo = testCallbackURL + "#x" }, ErrState},
		{"claimed_id not signed", func(a *assertion) {
			a.signed = "signed,op_endpoint,identity,return_to,response_nonce,assoc_handle"
		}, ErrState},
		{"return_to not signed", func(a *assertion) {
			a.signed = "signed,op_endpoint,claimed_id,identity,response_nonce,assoc_handle"
		}, ErrState},
		{"signed list with spaces", func(a *assertion) { a.signed = strings.ReplaceAll(validSignedFields, ",", ", ") }, ErrState},
		{"identity differs", func(a *assertion) { a.identity = claimedIDBase + "76561197960030003" }, ErrState},
		{"claimed_id on another host", func(a *assertion) {
			a.claimedID = "https://evil.example/openid/id/" + testSteamID
			a.identity = a.claimedID
		}, ErrState},
		{"claimed_id over http", func(a *assertion) {
			a.claimedID = "http://steamcommunity.com/openid/id/" + testSteamID
			a.identity = a.claimedID
		}, ErrState},
		{"claimed_id with a trailing slash", func(a *assertion) {
			a.claimedID += "/"
			a.identity = a.claimedID
		}, ErrID},
		{"claimed_id with letters", func(a *assertion) {
			a.claimedID = claimedIDBase + "notanumber"
			a.identity = a.claimedID
		}, ErrID},
		{"claimed_id with 17 characters, not all digits", func(a *assertion) {
			a.claimedID = claimedIDBase + "7656119796001000x"
			a.identity = a.claimedID
		}, ErrID},
		{"claimed_id with 17 bytes, not all ASCII", func(a *assertion) {
			a.claimedID = claimedIDBase + "765611979600100é"
			a.identity = a.claimedID
		}, ErrID},
		{"claimed_id too short", func(a *assertion) {
			a.claimedID = claimedIDBase + "123"
			a.identity = a.claimedID
		}, ErrID},
		{"claimed_id too long", func(a *assertion) {
			a.claimedID = claimedIDBase + testSteamID + "0"
			a.identity = a.claimedID
		}, ErrID},
	}
	for _, c := range cases {
		assertion := validAssertion()
		c.change(&assertion)
		id, err := assertion.steamID(testCallbackURL)
		if !errors.Is(err, c.err) {
			t.Errorf("%s: error %v, want %v", c.name, err, c.err)
		}
		if c.err == nil && id != strings.TrimPrefix(assertion.claimedID, claimedIDBase) {
			t.Errorf("%s: steam id %q", c.name, id)
		}
	}
}

func TestParseAssertion(t *testing.T) {
	query := validAssertion().query()
	if _, err := parseAssertion(query); err != nil {
		t.Fatalf("valid query: %v", err)
	}
	for _, field := range []string{"ns", "mode", "op_endpoint", "claimed_id", "identity", "return_to", "response_nonce", "assoc_handle", "signed", "sig"} {
		var kept []string
		for pair := range strings.SplitSeq(query, "&") {
			if !strings.HasPrefix(pair, "openid."+field+"=") {
				kept = append(kept, pair)
			}
		}
		if _, err := parseAssertion(strings.Join(kept, "&")); !errors.Is(err, ErrCallback) {
			t.Errorf("without openid.%s: %v", field, err)
		}
	}
	parsed, err := parseAssertion(query + "&openid.sig=second&openid.sig=")
	if err != nil || parsed.signature != "" {
		t.Errorf("the last value must win, got %q, %v", parsed.signature, err)
	}
	parsed, _ = parseAssertion(strings.Replace(query, "openid.sig=Kq3%2B%2Fparity%3D", "openid.sig=Kq3+/parity=", 1))
	if parsed.signature != "Kq3 /parity=" {
		t.Errorf("a raw '+' is a space: %q", parsed.signature)
	}
}

func TestCheckAuthenticationForm(t *testing.T) {
	want := "openid.ns=http%3A%2F%2Fspecs.openid.net%2Fauth%2F2.0&openid.mode=check_authentication" +
		"&openid.op_endpoint=https%3A%2F%2Fsteamcommunity.com%2Fopenid%2Flogin" +
		"&openid.claimed_id=https%3A%2F%2Fsteamcommunity.com%2Fopenid%2Fid%2F76561197960010001" +
		"&openid.identity=https%3A%2F%2Fsteamcommunity.com%2Fopenid%2Fid%2F76561197960010001" +
		"&openid.return_to=https%3A%2F%2Fbuckshot-roulette-macos.hloth.dev%2Fapi%2Fauth%2Fsteam%2Fcallback" +
		"&openid.response_nonce=2026-10-20T12%3A00%3A00Zparity&openid.assoc_handle=1234567890" +
		"&openid.signed=signed%2Cop_endpoint%2Cclaimed_id%2Cidentity%2Creturn_to%2Cresponse_nonce%2Cassoc_handle" +
		"&openid.sig=Kq3%2B%2Fparity%3D"
	if got := validAssertion().checkAuthenticationForm(); got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
}

func TestConfirmsAssertion(t *testing.T) {
	cases := map[string]bool{
		openIDValidAnswer: true,
		"is_valid:true":   true,
		"ns:http://specs.openid.net/auth/2.0\nis_valid:false\n":                                  false,
		"ns:http://specs.openid.net/auth/2.0\nis_valid:false\ninvalidate_handle:is_valid:true\n": false,
		"ns:http://specs.openid.net/auth/2.0 is_valid:true\n":                                    false,
		"ns:http://specs.openid.net/auth/2.0\nis_valid:false\nis_valid:true\n":                   false,
		"ns:http://specs.openid.net/auth/2.0\nis_valid:true\nis_valid:true\n":                    false,
		"ns:http://specs.openid.net/auth/2.0\nis_valid: true\n":                                  false,
		"ns:http://specs.openid.net/auth/2.0\nis_valid:TRUE\n":                                   false,
		"ns:http://specs.openid.net/auth/2.0\r\nis_valid:true\r\n":                               true,
		"ns:http://specs.openid.net/auth/2.0\r\nis_valid:false\r\n":                              false,
		"ns:http://specs.openid.net/auth/2.0\nis_valid:true\r\r\n":                               false,
		"<html><body>is_valid:true</body></html>":                                                false,
		"\xEF\xBB\xBFis_valid:true\n":                                                            true,
		"":                                                                                       false,
	}
	for body, want := range cases {
		if got := confirmsAssertion([]byte(body)); got != want {
			t.Errorf("confirmsAssertion(%q) = %v, want %v", body, got, want)
		}
	}
}
