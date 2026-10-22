//go:build !parity

package config

func loadOrigins(func(string) string) Origins {
	return Origins{SteamCommunity: steamCommunityOrigin, SteamAPI: steamAPIOrigin, ItchAPI: itchAPIOrigin}
}
