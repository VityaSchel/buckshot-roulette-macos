package main

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
)

type config struct {
	addr         string
	steamAPIKey  string
	jwtSecret    []byte
	downloadsDir string
	origins      upstreamOrigins

	publicAPIURL     string
	frontendPage     string
	steamCallbackURL string
	itchCallbackURL  string
	logoutURL        string
}

type environment struct {
	getenv  func(string) string
	missing []string
}

func (e *environment) require(name string) string {
	value := e.getenv(name)
	if value == "" {
		e.missing = append(e.missing, name)
	}
	return value
}

func (e *environment) withDefault(name, fallback string) string {
	if value := e.getenv(name); value != "" {
		return value
	}
	return fallback
}

func loadConfig(getenv func(string) string) (config, error) {
	env := environment{getenv: getenv}
	steamAPIKey := env.require("STEAM_API_KEY")
	jwtSecret := env.require("JWT_SECRET")
	publicURL := env.require("PUBLIC_URL")
	publicAPIURL := env.require("PUBLIC_API_URL")
	downloadsDir := env.require("DOWNLOADS_DIR")
	if len(env.missing) > 0 {
		return config{}, fmt.Errorf("missing environment variables: %s", strings.Join(env.missing, ", "))
	}

	port := env.withDefault("PORT", "3001")
	if _, err := strconv.ParseUint(port, 10, 16); err != nil {
		return config{}, errors.New("PORT is not a TCP port number")
	}
	frontend, err := parseAbsoluteURL("PUBLIC_URL", publicURL)
	if err != nil {
		return config{}, err
	}
	api, err := parseAbsoluteURL("PUBLIC_API_URL", publicAPIURL)
	if err != nil {
		return config{}, err
	}
	return config{
		addr:             net.JoinHostPort(env.withDefault("HOST", "127.0.0.1"), port),
		steamAPIKey:      steamAPIKey,
		jwtSecret:        []byte(jwtSecret),
		downloadsDir:     downloadsDir,
		origins:          loadOrigins(getenv),
		publicAPIURL:     publicAPIURL,
		frontendPage:     pageForFragments(frontend),
		steamCallbackURL: resolveURL(api, "auth/steam/callback"),
		itchCallbackURL:  resolveURL(api, "auth/itch.io/callback"),
		logoutURL:        resolveURL(api, "logout"),
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

func (c config) frontendURL(fragment string) string {
	return c.frontendPage + "#" + fragment
}
