package itchio

import (
	"context"
	_ "embed"
	"log"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"git.hloth.dev/hloth/buckshot-roulette-macos/backend/internal/compat"
	"git.hloth.dev/hloth/buckshot-roulette-macos/backend/internal/config"
	"git.hloth.dev/hloth/buckshot-roulette-macos/backend/internal/httpx"
	"git.hloth.dev/hloth/buckshot-roulette-macos/backend/internal/license"
	"git.hloth.dev/hloth/buckshot-roulette-macos/backend/internal/upstream"
)

const (
	clientID         = "3ebb49fdae8199285348d6bbbbf77af1"
	buckshotGameID   = 2443623
	maxOwnedKeyPages = 50
)

//go:embed itch.io-callback.html
var callbackPage []byte

func ServeCallbackPage(w http.ResponseWriter, _ *http.Request) {
	httpx.WriteBody(w, http.StatusOK, "text/html;charset=utf-8", callbackPage)
}

type Client struct {
	cfg      config.Config
	http     *http.Client
	licenses *license.Cache[int64]
}

func New(cfg config.Config, client *http.Client, now func() time.Time) *Client {
	return &Client{cfg: cfg, http: client, licenses: license.NewCache[int64](now)}
}

func (c *Client) LoginURL() string {
	return "https://itch.io/user/oauth?client_id=" + clientID +
		"&scope=profile:owned%20profile:me&response_type=token&redirect_uri=" + compat.EncodeURIComponent(c.cfg.ItchCallbackURL)
}

func (c *Client) License(ctx context.Context, token string) license.Result {
	authorization, ok := bearerAuthorization(token)
	if !ok {
		log.Print("itch.io: the token cannot be sent in a header")
		return license.Unknown
	}
	userID, err := upstream.Fetch(ctx, c.http, c.get("itch.io profile", "/profile", authorization), profileUserID)
	if err != nil {
		return license.Unknown
	}
	return c.licenses.Check(ctx, userID, func() license.Result {
		return c.fetchLicense(ctx, authorization)
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

func (c *Client) get(name, path, authorization string) upstream.Request {
	return upstream.Request{Name: name, Method: http.MethodGet, URL: c.cfg.Origins.ItchAPI + path, Authorization: authorization}
}

func profileUserID(body []byte) (int64, bool) {
	document, err := compat.DecodeJSON(body)
	if err != nil {
		return 0, false
	}
	return compat.NonNegativeSafeInteger(compat.Field(document, "user", "id"))
}

func (c *Client) fetchLicense(ctx context.Context, authorization string) license.Result {
	for page := 1; page <= maxOwnedKeyPages; page++ {
		path := "/profile/owned-keys?" + compat.EncodeForm("page", strconv.Itoa(page))
		gameIDs, err := upstream.Fetch(ctx, c.http, c.get("itch.io owned-keys", path, authorization), ownedGameIDs)
		switch {
		case err != nil:
			return license.Unknown
		case len(gameIDs) == 0:
			return license.NotOwned
		case slices.Contains(gameIDs, buckshotGameID):
			return license.Owned
		}
	}
	return license.NotOwned
}

// ownedGameIDs reads a page like the old Zod schema: owned_keys as an object holds nothing usable
// (an empty page), as an array every key needs a game id.
func ownedGameIDs(body []byte) ([]int64, bool) {
	document, err := compat.DecodeJSON(body)
	if err != nil {
		return nil, false
	}
	keys := compat.Field(document, "owned_keys")
	if _, isObject := keys.(map[string]any); isObject {
		return nil, true
	}
	return compat.Each(keys, "game_id", compat.NonNegativeSafeInteger)
}
