//go:build !parity

package main

func loadOrigins(func(string) string) upstreamOrigins {
	return productionOrigins
}
