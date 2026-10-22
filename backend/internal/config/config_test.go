package config

import (
	"maps"
	"strings"
	"testing"
)

func testEnv(overrides map[string]string) func(string) string {
	values := map[string]string{
		"STEAM_API_KEY":  "test-steam-key",
		"JWT_SECRET":     "test-secret-for-parity",
		"PUBLIC_URL":     "https://buckshot-roulette-macos.hloth.dev",
		"PUBLIC_API_URL": "https://buckshot-roulette-macos.hloth.dev/api/",
		"DOWNLOADS_DIR":  "/nonexistent",
	}
	maps.Copy(values, overrides)
	return func(name string) string { return values[name] }
}

func TestLoadNamesMissingVariables(t *testing.T) {
	_, err := Load(testEnv(map[string]string{"DOWNLOADS_DIR": "", "JWT_SECRET": "", "PUBLIC_URL": ""}))
	if err == nil {
		t.Fatal("missing variables were accepted")
	}
	if want := "missing environment variables: JWT_SECRET, PUBLIC_URL, DOWNLOADS_DIR"; err.Error() != want {
		t.Fatalf("error %q, want %q", err, want)
	}
	for _, name := range []string{"PUBLIC_URL", "PUBLIC_API_URL"} {
		_, err = Load(testEnv(map[string]string{name: "not a url", "STEAM_API_KEY": "secret-key-value"}))
		if err == nil || !strings.Contains(err.Error(), name) || strings.Contains(err.Error(), "not a url") || strings.Contains(err.Error(), "secret-key-value") {
			t.Fatalf("error %v must name %s, not its value", err, name)
		}
	}
}

func TestLoadDefaultsAndDerivedURLs(t *testing.T) {
	cfg, err := Load(testEnv(map[string]string{"NODE_ENV": "development"}))
	if err != nil {
		t.Fatal(err)
	}
	checks := map[string][2]string{
		"addr":              {cfg.Addr, "127.0.0.1:3001"},
		"frontend fragment": {cfg.FrontendURL("success"), "https://buckshot-roulette-macos.hloth.dev/#success"},
		"steam callback":    {cfg.SteamCallbackURL, "https://buckshot-roulette-macos.hloth.dev/api/auth/steam/callback"},
		"itch.io callback":  {cfg.ItchCallbackURL, "https://buckshot-roulette-macos.hloth.dev/api/auth/itch.io/callback"},
		"logout":            {cfg.LogoutURL, "https://buckshot-roulette-macos.hloth.dev/api/logout"},
		"steam origin":      {cfg.Origins.SteamCommunity, "https://steamcommunity.com"},
		"steam API origin":  {cfg.Origins.SteamAPI, "https://api.steampowered.com"},
		"itch.io origin":    {cfg.Origins.ItchAPI, "https://api.itch.io"},
	}
	for name, check := range checks {
		if check[0] != check[1] {
			t.Errorf("%s: %q, want %q", name, check[0], check[1])
		}
	}

	cfg, err = Load(testEnv(map[string]string{"HOST": "::1", "PORT": "3007", "PUBLIC_URL": "https://example.com/app/#old"}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Addr != "[::1]:3007" || cfg.FrontendURL("auth") != "https://example.com/app/#auth" {
		t.Fatalf("addr %q, frontend %q", cfg.Addr, cfg.FrontendURL("auth"))
	}
	if _, err := Load(testEnv(map[string]string{"PORT": "70000"})); err == nil {
		t.Fatal("an invalid PORT was accepted")
	}
}
