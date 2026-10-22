//go:build !parity

package config

import "testing"

func TestProductionBuildIgnoresTestOrigins(t *testing.T) {
	cfg, err := Load(testEnv(map[string]string{"TEST_STEAM_COMMUNITY_ORIGIN": "http://127.0.0.1:39530"}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Origins.SteamCommunity != steamCommunityOrigin {
		t.Fatalf("origin %q", cfg.Origins.SteamCommunity)
	}
}
