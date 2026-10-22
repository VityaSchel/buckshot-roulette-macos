package config

import (
	"cmp"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
)

type Config struct {
	Addr         string
	SteamAPIKey  string
	JWTSecret    []byte
	DownloadsDir string
	Origins      Origins

	PublicAPIURL     string
	SteamCallbackURL string
	ItchCallbackURL  string
	LogoutURL        string
	frontendPage     string
}

type Origins struct {
	SteamCommunity string
	SteamAPI       string
	ItchAPI        string
}

const (
	steamCommunityOrigin = "https://steamcommunity.com"
	steamAPIOrigin       = "https://api.steampowered.com"
	itchAPIOrigin        = "https://api.itch.io"
)

func Load(getenv func(string) string) (Config, error) {
	var missing []string
	require := func(name string) string {
		value := getenv(name)
		if value == "" {
			missing = append(missing, name)
		}
		return value
	}
	steamAPIKey := require("STEAM_API_KEY")
	jwtSecret := require("JWT_SECRET")
	publicURL := require("PUBLIC_URL")
	publicAPIURL := require("PUBLIC_API_URL")
	downloadsDir := require("DOWNLOADS_DIR")
	if len(missing) > 0 {
		return Config{}, fmt.Errorf("missing environment variables: %s", strings.Join(missing, ", "))
	}

	port := cmp.Or(getenv("PORT"), "3001")
	if _, err := strconv.ParseUint(port, 10, 16); err != nil {
		return Config{}, errors.New("PORT is not a TCP port number")
	}
	frontend, err := parseAbsoluteURL("PUBLIC_URL", publicURL)
	if err != nil {
		return Config{}, err
	}
	api, err := parseAbsoluteURL("PUBLIC_API_URL", publicAPIURL)
	if err != nil {
		return Config{}, err
	}
	return Config{
		Addr:             net.JoinHostPort(cmp.Or(getenv("HOST"), "127.0.0.1"), port),
		SteamAPIKey:      steamAPIKey,
		JWTSecret:        []byte(jwtSecret),
		DownloadsDir:     downloadsDir,
		Origins:          loadOrigins(getenv),
		PublicAPIURL:     publicAPIURL,
		SteamCallbackURL: resolveURL(api, "auth/steam/callback"),
		ItchCallbackURL:  resolveURL(api, "auth/itch.io/callback"),
		LogoutURL:        resolveURL(api, "logout"),
		frontendPage:     pageForFragments(frontend),
	}, nil
}

func parseAbsoluteURL(name, raw string) (*url.URL, error) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, fmt.Errorf("%s is not an absolute URL", name)
	}
	return parsed, nil
}

func resolveURL(base *url.URL, reference string) string {
	return base.ResolveReference(&url.URL{Path: reference}).String()
}

// pageForFragments mirrors new URL('#x', base): the fragment is replaced and an empty path becomes "/".
func pageForFragments(base *url.URL) string {
	page := *base
	page.Fragment, page.RawFragment = "", ""
	if page.Path == "" {
		page.Path, page.RawPath = "/", ""
	}
	return page.String()
}

func (c Config) FrontendURL(fragment string) string {
	return c.frontendPage + "#" + fragment
}
