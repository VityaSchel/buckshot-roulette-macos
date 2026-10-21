package main

import (
	"context"
	_ "embed"
	"errors"
	"log"
	"net/http"
	"slices"
	"strconv"
	"strings"
)

const (
	itchClientID         = "3ebb49fdae8199285348d6bbbbf77af1"
	buckshotItchGameID   = 2443623
	maxOwnedKeyPages     = 50
	itchCallbackPageType = "text/html;charset=utf-8"
)

var errItchAnswer = errors.New("unexpected itch.io answer")

//go:embed static/itch.io-callback.html
var itchCallbackPageHTML []byte

func itchLoginURL(cfg config) string {
	return "https://itch.io/user/oauth?client_id=" + itchClientID +
		"&scope=profile:owned%20profile:me&response_type=token&redirect_uri=" + encodeURIComponent(cfg.itchCallbackURL)
}

func (a *app) itchLogin(w http.ResponseWriter, r *http.Request) {
	redirect(w, a.itchLoginURL)
}

func (a *app) itchCallbackPage(w http.ResponseWriter, r *http.Request) {
	writeBody(w, http.StatusOK, itchCallbackPageType, itchCallbackPageHTML)
}

func (a *app) itchCallback(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.licenseFromRequest(w, r); !ok {
		return
	}
	token, err := readItchToken(w, r)
	if err != nil {
		log.Printf("itch.io callback: %v", err)
		writeError(w, http.StatusInternalServerError, "Internal server error")
		return
	}
	a.finishLicenseCheck(w, "itch.io", a.itchLicense(r.Context(), token))
}

func (a *app) itchLicense(ctx context.Context, token string) licenseResult {
	authorization, ok := bearerAuthorization(token)
	if !ok {
		log.Print("itch.io: the token cannot be sent in a header")
		return licenseUnknown
	}
	userID, err := a.itchUserID(ctx, authorization)
	if err != nil {
		return licenseUnknown
	}
	return a.itchLicenses.check(ctx, userID, func() licenseResult {
		return a.fetchItchLicense(ctx, authorization)
	})
}

// fetch() turned header values into Latin-1 bytes after trimming HTTP whitespace, and refused
// anything it could not send; such a token never reached itch.io.
func bearerAuthorization(token string) (string, bool) {
	value := make([]byte, 0, len("Bearer ")+len(token))
	for _, char := range "Bearer " + token {
		if char > 0xFF {
			return "", false
		}
		value = append(value, byte(char))
	}
	trimmed := strings.Trim(string(value), " \t\r\n")
	if strings.ContainsAny(trimmed, "\x00\r\n") {
		return "", false
	}
	return trimmed, true
}

func (a *app) itchUserID(ctx context.Context, authorization string) (int64, error) {
	const call = "itch.io profile"
	response, err := a.itchGet(ctx, "/profile", authorization)
	if err != nil {
		logUpstreamError(call, err)
		return 0, err
	}
	userID, ok := profileUserID(response.body)
	if !ok {
		logUpstreamAnswer(call, "unexpected answer", response.status)
		return 0, errItchAnswer
	}
	return userID, nil
}

func profileUserID(body []byte) (int64, bool) {
	document, err := decodeJSON(body)
	if err != nil {
		return 0, false
	}
	root, _ := jsonObject(document)
	user, ok := jsonObject(root["user"])
	if !ok {
		return 0, false
	}
	return nonNegativeSafeInteger(user["id"])
}

func (a *app) fetchItchLicense(ctx context.Context, authorization string) licenseResult {
	const call = "itch.io owned-keys"
	for page := 1; page <= maxOwnedKeyPages; page++ {
		response, err := a.itchGet(ctx, "/profile/owned-keys?"+encodeForm(queryPair{"page", strconv.Itoa(page)}), authorization)
		if err != nil {
			logUpstreamError(call, err)
			return licenseUnknown
		}
		gameIDs, ok := ownedGameIDs(response.body)
		if !ok {
			logUpstreamAnswer(call, "unexpected answer", response.status)
			return licenseUnknown
		}
		if len(gameIDs) == 0 {
			return licenseNotOwned
		}
		if slices.Contains(gameIDs, buckshotItchGameID) {
			return licenseOwned
		}
	}
	return licenseNotOwned
}

// ownedGameIDs reads a page like the old Zod schema: owned_keys as an object holds nothing usable
// (an empty page), as an array every key needs a game id.
func ownedGameIDs(body []byte) ([]int64, bool) {
	document, err := decodeJSON(body)
	if err != nil {
		return nil, false
	}
	root, _ := jsonObject(document)
	switch keys := root["owned_keys"].(type) {
	case map[string]any:
		return nil, true
	case []any:
		gameIDs := make([]int64, 0, len(keys))
		for _, entry := range keys {
			key, ok := jsonObject(entry)
			if !ok {
				return nil, false
			}
			gameID, ok := nonNegativeSafeInteger(key["game_id"])
			if !ok {
				return nil, false
			}
			gameIDs = append(gameIDs, gameID)
		}
		return gameIDs, true
	}
	return nil, false
}

func (a *app) itchGet(ctx context.Context, path, authorization string) (upstreamResponse, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, a.cfg.origins.itchAPI+path, nil)
	if err != nil {
		return upstreamResponse{}, err
	}
	request.Header.Set("Authorization", authorization)
	return fetch(a.client, request)
}
