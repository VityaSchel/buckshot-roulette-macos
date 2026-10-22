package steam

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"slices"
	"time"

	"git.hloth.dev/hloth/buckshot-roulette-macos/backend/internal/compat"
	"git.hloth.dev/hloth/buckshot-roulette-macos/backend/internal/config"
	"git.hloth.dev/hloth/buckshot-roulette-macos/backend/internal/license"
	"git.hloth.dev/hloth/buckshot-roulette-macos/backend/internal/upstream"
)

const (
	testID        = "76561198943790498"
	buckshotAppID = 2835570
)

type Failure struct {
	Status  int
	Message string
}

func (f *Failure) Error() string {
	return f.Message
}

var (
	ErrCallback    = &Failure{http.StatusBadRequest, "Invalid Steam authorization callback, try again in 10-30 minutes"}
	ErrState       = &Failure{http.StatusUnauthorized, "Invalid Steam authorization state"}
	ErrID          = &Failure{http.StatusBadRequest, "Failed to extract Steam ID from authorization callback, try again in 10-30 minutes"}
	ErrUnavailable = &Failure{http.StatusBadGateway, "Steam API server error, try again in 10-30 minutes"}
)

type Client struct {
	cfg      config.Config
	http     *http.Client
	licenses *license.Cache[string]
}

func New(cfg config.Config, client *http.Client, now func() time.Time) *Client {
	return &Client{cfg: cfg, http: client, licenses: license.NewCache[string](now)}
}

func (c *Client) LoginURL() string {
	return openIDEndpoint +
		"?openid.ns=" + openIDNamespace +
		"&openid.mode=checkid_setup" +
		"&openid.return_to=" + compat.EncodeURIComponent(c.cfg.SteamCallbackURL) +
		"&openid.realm=" + c.cfg.PublicAPIURL +
		"&openid.identity=http://specs.openid.net/auth/2.0/identifier_select" +
		"&openid.claimed_id=http://specs.openid.net/auth/2.0/identifier_select"
}

func (c *Client) Verify(ctx context.Context, rawQuery string) (string, error) {
	assertion, err := parseAssertion(rawQuery)
	if err != nil {
		return "", err
	}
	steamID, err := assertion.steamID(c.cfg.SteamCallbackURL)
	if err != nil {
		log.Printf("steam callback: %v", err)
		return "", err
	}
	if err := c.checkAuthentication(ctx, assertion); err != nil {
		return "", err
	}
	return steamID, nil
}

func (c *Client) checkAuthentication(ctx context.Context, assertion assertion) error {
	_, err := upstream.Fetch(ctx, c.http, upstream.Request{
		Name:    "steam check_authentication",
		Method:  http.MethodPost,
		URL:     c.cfg.Origins.SteamCommunity + "/openid/login",
		Form:    assertion.checkAuthenticationForm(),
		OKOnly:  true,
		Refusal: "assertion not confirmed",
	}, func(body []byte) (struct{}, bool) { return struct{}{}, confirmsAssertion(body) })
	switch {
	case errors.Is(err, upstream.ErrRefused):
		return ErrState
	case err != nil:
		return ErrUnavailable
	}
	return nil
}

func (c *Client) License(ctx context.Context, steamID string) license.Result {
	if steamID == testID {
		return license.Owned
	}
	return c.licenses.Check(ctx, steamID, func() license.Result {
		return c.fetchLicense(ctx, steamID)
	})
}

type ownedGamesInput struct {
	SteamID                string `json:"steamid"`
	AppIDsFilter           []int  `json:"appids_filter"`
	IncludeAppInfo         bool   `json:"include_appinfo"`
	IncludePlayedFreeGames bool   `json:"include_played_free_games"`
}

func (c *Client) fetchLicense(ctx context.Context, steamID string) license.Result {
	input, err := json.Marshal(ownedGamesInput{SteamID: steamID, AppIDsFilter: []int{buckshotAppID}})
	if err != nil {
		return license.Unknown
	}
	result, err := upstream.Fetch(ctx, c.http, upstream.Request{
		Name:   "steam GetOwnedGames",
		Method: http.MethodGet,
		URL:    c.cfg.Origins.SteamAPI + "/IPlayerService/GetOwnedGames/v1/?" + compat.EncodeForm("key", c.cfg.SteamAPIKey, "input_json", string(input)),
	}, ownership)
	if err != nil {
		return license.Unknown
	}
	return result
}

// ownership reads GetOwnedGames like the old Zod schema: a valid games list decides,
// otherwise only game_count 0 means "not owned". The HTTP status was never looked at.
func ownership(body []byte) (license.Result, bool) {
	document, err := compat.DecodeJSON(body)
	if err != nil {
		return license.Unknown, false
	}
	response := compat.Field(document, "response")
	if appIDs, ok := compat.Each(compat.Field(response, "games"), "appid", compat.FiniteNumber); ok {
		if slices.Contains(appIDs, buckshotAppID) {
			return license.Owned, true
		}
		return license.NotOwned, true
	}
	if count, ok := compat.FiniteNumber(compat.Field(response, "game_count")); ok && count == 0 {
		return license.NotOwned, true
	}
	return license.Unknown, false
}
