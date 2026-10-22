package steam

import (
	"fmt"
	"slices"
	"strings"

	"git.hloth.dev/hloth/buckshot-roulette-macos/backend/internal/compat"
)

const (
	openIDNamespace = "http://specs.openid.net/auth/2.0"
	openIDEndpoint  = "https://steamcommunity.com/openid/login"
	claimedIDBase   = "https://steamcommunity.com/openid/id/"
	idLength        = 17
)

type assertion struct {
	namespace     string
	mode          string
	opEndpoint    string
	claimedID     string
	identity      string
	returnTo      string
	responseNonce string
	assocHandle   string
	signed        string
	signature     string
}

func parseAssertion(rawQuery string) (assertion, error) {
	query := compat.LastValues(compat.ParseQuery(rawQuery))
	missing := false
	field := func(name string) string {
		value, ok := query["openid."+name]
		missing = missing || !ok
		return value
	}
	parsed := assertion{
		namespace:     field("ns"),
		mode:          field("mode"),
		opEndpoint:    field("op_endpoint"),
		claimedID:     field("claimed_id"),
		identity:      field("identity"),
		returnTo:      field("return_to"),
		responseNonce: field("response_nonce"),
		assocHandle:   field("assoc_handle"),
		signed:        field("signed"),
		signature:     field("sig"),
	}
	if missing {
		return assertion{}, ErrCallback
	}
	return parsed, nil
}

// Steam's check_authentication only proves the signature, so the assertion must also be a
// positive answer from Steam, for our callback, about a Steam account.
func (a assertion) steamID(callbackURL string) (string, error) {
	if reason := a.refusal(callbackURL); reason != "" {
		return "", fmt.Errorf("%w: %s", ErrState, reason)
	}
	id := strings.TrimPrefix(a.claimedID, claimedIDBase)
	if len(id) != idLength || strings.Trim(id, "0123456789") != "" {
		return "", fmt.Errorf("%w: the account id is not 17 digits", ErrID)
	}
	return id, nil
}

func (a assertion) refusal(callbackURL string) string {
	switch {
	case a.namespace != openIDNamespace:
		return "openid.ns is not OpenID 2.0"
	case a.mode != "id_res":
		return "openid.mode is not id_res"
	case a.opEndpoint != openIDEndpoint:
		return "openid.op_endpoint is not Steam"
	// Steam may append a query string to return_to, nothing else.
	case a.returnTo != callbackURL && !strings.HasPrefix(a.returnTo, callbackURL+"?"):
		return "openid.return_to is not this backend's callback"
	case !signsRequiredFields(a.signed):
		return "openid.signed does not cover the assertion"
	case a.claimedID != a.identity:
		return "openid.identity differs from openid.claimed_id"
	case !strings.HasPrefix(a.claimedID, claimedIDBase):
		return "openid.claimed_id is not a Steam account"
	}
	return ""
}

func signsRequiredFields(signed string) bool {
	fields := strings.Split(signed, ",")
	for _, required := range []string{"op_endpoint", "claimed_id", "identity", "return_to", "response_nonce", "assoc_handle"} {
		if !slices.Contains(fields, required) {
			return false
		}
	}
	return true
}

func (a assertion) checkAuthenticationForm() string {
	return compat.EncodeForm(
		"openid.ns", a.namespace,
		"openid.mode", "check_authentication",
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

// The answer is OpenID key-value form: one "key:value" per line, and is_valid must appear exactly once.
// A CRLF line end is tolerated: the old substring check accepted it, and it changes no verdict.
func confirmsAssertion(body []byte) bool {
	verdicts := 0
	valid := false
	for line := range strings.SplitSeq(string(compat.WithoutBOM(body)), "\n") {
		key, value, ok := strings.Cut(strings.TrimSuffix(line, "\r"), ":")
		if ok && key == "is_valid" {
			verdicts++
			valid = value == "true"
		}
	}
	return verdicts == 1 && valid
}
