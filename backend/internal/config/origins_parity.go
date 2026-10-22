//go:build parity

package config

import "cmp"

// The parity harness points the server-to-server calls at its mock; browser redirects keep the real hosts.
func loadOrigins(getenv func(string) string) Origins {
	return Origins{
		SteamCommunity: cmp.Or(getenv("TEST_STEAM_COMMUNITY_ORIGIN"), steamCommunityOrigin),
		SteamAPI:       cmp.Or(getenv("TEST_STEAM_API_ORIGIN"), steamAPIOrigin),
		ItchAPI:        cmp.Or(getenv("TEST_ITCH_API_ORIGIN"), itchAPIOrigin),
	}
}
