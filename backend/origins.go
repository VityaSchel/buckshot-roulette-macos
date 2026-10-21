package main

type upstreamOrigins struct {
	steamCommunity string
	steamAPI       string
	itchAPI        string
}

var productionOrigins = upstreamOrigins{
	steamCommunity: "https://steamcommunity.com",
	steamAPI:       "https://api.steampowered.com",
	itchAPI:        "https://api.itch.io",
}
