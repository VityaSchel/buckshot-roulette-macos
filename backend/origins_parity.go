//go:build parity

package main

// The parity harness points the server-to-server calls at its mock; browser redirects keep the real hosts.
func loadOrigins(getenv func(string) string) upstreamOrigins {
	origin := func(name, production string) string {
		if value := getenv(name); value != "" {
			return value
		}
		return production
	}
	return upstreamOrigins{
		steamCommunity: origin("TEST_STEAM_COMMUNITY_ORIGIN", productionOrigins.steamCommunity),
		steamAPI:       origin("TEST_STEAM_API_ORIGIN", productionOrigins.steamAPI),
		itchAPI:        origin("TEST_ITCH_API_ORIGIN", productionOrigins.itchAPI),
	}
}
