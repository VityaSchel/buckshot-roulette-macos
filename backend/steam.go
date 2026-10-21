package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"slices"
	"strings"
)

const (
	openIDNamespace     = "http://specs.openid.net/auth/2.0"
	steamOpenIDEndpoint = "https://steamcommunity.com/openid/login"
	steamClaimedIDBase  = "https://steamcommunity.com/openid/id/"
	steamIDLength       = 17
	steamTestID         = "76561198943790498"
	buckshotSteamAppID  = 2835570
)

type steamFailure struct {
	status  int
	message string
}

func (f *steamFailure) Error() string {
	return f.message
}

var (
	errSteamCallback    = &steamFailure{http.StatusBadRequest, "Invalid Steam authorization callback, try again in 10-30 minutes"}
	errSteamState       = &steamFailure{http.StatusUnauthorized, "Invalid Steam authorization state"}
	errSteamID          = &steamFailure{http.StatusBadRequest, "Failed to extract Steam ID from authorization callback, try again in 10-30 minutes"}
	errSteamUnavailable = &steamFailure{http.StatusBadGateway, "Steam API server error, try again in 10-30 minutes"}
)

var requiredSignedFields = []string{"op_endpoint", "claimed_id", "identity", "return_to", "response_nonce", "assoc_handle"}

type steamAssertion struct {
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

func steamLoginURL(cfg config) string {
	return steamOpenIDEndpoint +
		"?openid.ns=" + openIDNamespace +
		"&openid.mode=checkid_setup" +
		"&openid.return_to=" + encodeURIComponent(cfg.steamCallbackURL) +
		"&openid.realm=" + cfg.publicAPIURL +
		"&openid.identity=http://specs.openid.net/auth/2.0/identifier_select" +
		"&openid.claimed_id=http://specs.openid.net/auth/2.0/identifier_select"
}

func (a *app) steamLogin(w http.ResponseWriter, r *http.Request) {
	redirect(w, a.steamLoginURL)
}

func (a *app) steamCallback(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.licenseFromRequest(w, r); !ok {
		return
	}
	steamID, err := a.verifySteamLogin(r.Context(), r.URL.RawQuery)
	if err != nil {
		failure := errSteamUnavailable
		errors.As(err, &failure)
		writeError(w, failure.status, failure.message)
		return
	}
	a.finishLicenseCheck(w, "steam", a.steamLicense(r.Context(), steamID))
}

func (a *app) verifySteamLogin(ctx context.Context, rawQuery string) (string, error) {
	assertion, err := parseSteamAssertion(rawQuery)
	if err != nil {
		return "", err
	}
	steamID, err := assertion.steamID(a.cfg.steamCallbackURL)
	if err != nil {
		log.Printf("steam callback: %v", err)
		return "", err
	}
	if err := a.checkAuthentication(ctx, assertion); err != nil {
		return "", err
	}
	return steamID, nil
}

func parseSteamAssertion(rawQuery string) (steamAssertion, error) {
	query := lastValues(parseQuery(rawQuery))
	missing := false
	field := func(name string) string {
		value, ok := query["openid."+name]
		missing = missing || !ok
		return value
	}
	assertion := steamAssertion{
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
		return steamAssertion{}, errSteamCallback
	}
	return assertion, nil
}

// Steam's check_authentication only proves the signature, so the assertion must also be a
// positive answer from Steam, for our callback, about a Steam account.
func (s steamAssertion) steamID(callbackURL string) (string, error) {
	if reason := s.refusal(callbackURL); reason != "" {
		return "", fmt.Errorf("%w: %s", errSteamState, reason)
	}
	id := strings.TrimPrefix(s.claimedID, steamClaimedIDBase)
	if !isSteamID(id) {
		return "", fmt.Errorf("%w: the account id is not 17 digits", errSteamID)
	}
	return id, nil
}

func (s steamAssertion) refusal(callbackURL string) string {
	switch {
	case s.namespace != openIDNamespace:
		return "openid.ns is not OpenID 2.0"
	case s.mode != "id_res":
		return "openid.mode is not id_res"
	case s.opEndpoint != steamOpenIDEndpoint:
		return "openid.op_endpoint is not Steam"
	case !isOurReturnTo(s.returnTo, callbackURL):
		return "openid.return_to is not this backend's callback"
	case !signsRequiredFields(s.signed):
		return "openid.signed does not cover the assertion"
	case s.claimedID != s.identity:
		return "openid.identity differs from openid.claimed_id"
	case !strings.HasPrefix(s.claimedID, steamClaimedIDBase):
		return "openid.claimed_id is not a Steam account"
	}
	return ""
}

// Steam may append a query string to return_to, nothing else.
func isOurReturnTo(returnTo, callbackURL string) bool {
	return returnTo == callbackURL || strings.HasPrefix(returnTo, callbackURL+"?")
}

func signsRequiredFields(signed string) bool {
	fields := strings.Split(signed, ",")
	for _, required := range requiredSignedFields {
		if !slices.Contains(fields, required) {
			return false
		}
	}
	return true
}

func isSteamID(id string) bool {
	if len(id) != steamIDLength {
		return false
	}
	for i := range len(id) {
		if id[i] < '0' || id[i] > '9' {
			return false
		}
	}
	return true
}

func (s steamAssertion) checkAuthenticationForm() string {
	return encodeForm(
		queryPair{"openid.ns", s.namespace},
		queryPair{"openid.mode", "check_authentication"},
		queryPair{"openid.op_endpoint", s.opEndpoint},
		queryPair{"openid.claimed_id", s.claimedID},
		queryPair{"openid.identity", s.identity},
		queryPair{"openid.return_to", s.returnTo},
		queryPair{"openid.response_nonce", s.responseNonce},
		queryPair{"openid.assoc_handle", s.assocHandle},
		queryPair{"openid.signed", s.signed},
		queryPair{"openid.sig", s.signature},
	)
}

func (a *app) checkAuthentication(ctx context.Context, assertion steamAssertion) error {
	const call = "steam check_authentication"
	form := strings.NewReader(assertion.checkAuthenticationForm())
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, a.cfg.origins.steamCommunity+"/openid/login", form)
	if err != nil {
		logUpstreamError(call, err)
		return errSteamUnavailable
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := fetch(a.client, request)
	if err != nil {
		logUpstreamError(call, err)
		return errSteamUnavailable
	}
	if response.status != http.StatusOK || !confirmsAssertion(response.body) {
		logUpstreamAnswer(call, "assertion not confirmed", response.status)
		return errSteamState
	}
	return nil
}

// The answer is OpenID key-value form: one "key:value" per line, and is_valid must appear exactly once.
// A CRLF line end is tolerated: the old substring check accepted it, and it changes no verdict.
func confirmsAssertion(body []byte) bool {
	verdicts := 0
	valid := false
	for line := range strings.SplitSeq(string(withoutBOM(body)), "\n") {
		key, value, ok := strings.Cut(strings.TrimSuffix(line, "\r"), ":")
		if ok && key == "is_valid" {
			verdicts++
			valid = value == "true"
		}
	}
	return verdicts == 1 && valid
}

func (a *app) steamLicense(ctx context.Context, steamID string) licenseResult {
	if steamID == steamTestID {
		return licenseOwned
	}
	return a.steamLicenses.check(ctx, steamID, func() licenseResult {
		return a.fetchSteamLicense(ctx, steamID)
	})
}

func (a *app) fetchSteamLicense(ctx context.Context, steamID string) licenseResult {
	const call = "steam GetOwnedGames"
	input, err := json.Marshal(ownedGamesInput{SteamID: steamID, AppIDsFilter: []int{buckshotSteamAppID}})
	if err != nil {
		return licenseUnknown
	}
	query := encodeForm(queryPair{"key", a.cfg.steamAPIKey}, queryPair{"input_json", string(input)})
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, a.cfg.origins.steamAPI+"/IPlayerService/GetOwnedGames/v1/?"+query, nil)
	if err != nil {
		return licenseUnknown
	}
	response, err := fetch(a.client, request)
	if err != nil {
		logUpstreamError(call, err)
		return licenseUnknown
	}
	result := steamOwnership(response.body)
	if result == licenseUnknown {
		logUpstreamAnswer(call, "unexpected answer", response.status)
	}
	return result
}

type ownedGamesInput struct {
	SteamID                string `json:"steamid"`
	AppIDsFilter           []int  `json:"appids_filter"`
	IncludeAppInfo         bool   `json:"include_appinfo"`
	IncludePlayedFreeGames bool   `json:"include_played_free_games"`
}

// steamOwnership reads GetOwnedGames like the old Zod schema: a valid games list decides,
// otherwise only game_count 0 means "not owned". The HTTP status was never looked at.
func steamOwnership(body []byte) licenseResult {
	document, err := decodeJSON(body)
	if err != nil {
		return licenseUnknown
	}
	root, _ := jsonObject(document)
	response, ok := jsonObject(root["response"])
	if !ok {
		return licenseUnknown
	}
	if appIDs, ok := gameAppIDs(response["games"]); ok {
		if slices.Contains(appIDs, buckshotSteamAppID) {
			return licenseOwned
		}
		return licenseNotOwned
	}
	if count, ok := finiteNumber(response["game_count"]); ok && count == 0 {
		return licenseNotOwned
	}
	return licenseUnknown
}

func gameAppIDs(value any) ([]float64, bool) {
	games, ok := value.([]any)
	if !ok {
		return nil, false
	}
	appIDs := make([]float64, 0, len(games))
	for _, entry := range games {
		game, ok := jsonObject(entry)
		if !ok {
			return nil, false
		}
		appID, ok := finiteNumber(game["appid"])
		if !ok {
			return nil, false
		}
		appIDs = append(appIDs, appID)
	}
	return appIDs, true
}
