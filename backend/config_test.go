package main

import (
	"strings"
	"testing"
)

func TestLoadConfigNamesMissingVariables(t *testing.T) {
	_, err := loadConfig(testEnv(map[string]string{"DOWNLOADS_DIR": "", "JWT_SECRET": "", "PUBLIC_URL": ""}))
	if err == nil {
		t.Fatal("missing variables were accepted")
	}
	if want := "missing environment variables: JWT_SECRET, PUBLIC_URL, DOWNLOADS_DIR"; err.Error() != want {
		t.Fatalf("error %q, want %q", err, want)
	}
	_, err = loadConfig(testEnv(map[string]string{"PUBLIC_URL": "not a url", "STEAM_API_KEY": "secret-key-value"}))
	if err == nil || strings.Contains(err.Error(), "not a url") || strings.Contains(err.Error(), "secret-key-value") {
		t.Fatalf("error %v must name the variable, not its value", err)
	}
}

func TestLoadConfigDefaultsAndDerivedURLs(t *testing.T) {
	cfg, err := loadConfig(testEnv(map[string]string{"NODE_ENV": "development"}))
	if err != nil {
		t.Fatal(err)
	}
	checks := map[string][2]string{
		"addr":              {cfg.addr, "127.0.0.1:3001"},
		"frontend fragment": {cfg.frontendURL("success"), "https://buckshot-roulette-macos.hloth.dev/#success"},
		"steam callback":    {cfg.steamCallbackURL, "https://buckshot-roulette-macos.hloth.dev/api/auth/steam/callback"},
		"itch.io callback":  {cfg.itchCallbackURL, "https://buckshot-roulette-macos.hloth.dev/api/auth/itch.io/callback"},
		"logout":            {cfg.logoutURL, "https://buckshot-roulette-macos.hloth.dev/api/logout"},
		"steam origin":      {cfg.origins.steamCommunity, "https://steamcommunity.com"},
	}
	for name, check := range checks {
		if check[0] != check[1] {
			t.Errorf("%s: %q, want %q", name, check[0], check[1])
		}
	}

	cfg, err = loadConfig(testEnv(map[string]string{"HOST": "::1", "PORT": "3007", "PUBLIC_URL": "https://example.com/app/#old"}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.addr != "[::1]:3007" || cfg.frontendURL("auth") != "https://example.com/app/#auth" {
		t.Fatalf("addr %q, frontend %q", cfg.addr, cfg.frontendURL("auth"))
	}
	if _, err := loadConfig(testEnv(map[string]string{"PORT": "70000"})); err == nil {
		t.Fatal("an invalid PORT was accepted")
	}
}
