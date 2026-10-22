# Buckshot Roulette macOS Website Backend

Built with Go and its standard library, nothing else.

## Build

Needs Go 1.27:

```sh
CGO_ENABLED=0 go build -trimpath -buildvcs=false -ldflags='-s -w' -o buckshot-roulette-macos.hloth.dev-backend .
```

The result is one static binary with no runtime dependencies.

## Run

```sh
STEAM_API_KEY=… JWT_SECRET=… \
PUBLIC_URL=https://buckshot-roulette-macos.hloth.dev \
PUBLIC_API_URL=https://buckshot-roulette-macos.hloth.dev/api/ \
DOWNLOADS_DIR=/srv/buckshot-roulette-macos.hloth.dev \
./buckshot-roulette-macos.hloth.dev-backend
```

| Variable | Default | Purpose |
| --- | --- | --- |
| `STEAM_API_KEY` | required | Steam Web API key, for `GetOwnedGames` |
| `JWT_SECRET` | required | Signs the `license` cookie. Changing it signs everyone out. |
| `PUBLIC_URL` | required | The website. Logins end on it with `#success`, `#error-…`, `#auth` or `#try-again`. |
| `PUBLIC_API_URL` | required | This backend as the browser sees it, with a trailing slash. The Steam and itch.io callbacks are built from it. |
| `DOWNLOADS_DIR` | required | Folder that holds `buckshot-roulette-macos-v1.2.zip`. It may be missing at startup. |
| `HOST` | `127.0.0.1` | Listen address |
| `PORT` | `3001` | Listen port |

The backend exits with status 1 and names the missing variables. It stops gracefully on SIGTERM. It expects a reverse proxy in front that strips `/api`, as Caddy does in production.

## Routes

| Route | What it does |
| --- | --- |
| `GET /auth/steam` | Redirects to Steam's OpenID login |
| `GET /auth/steam/callback` | Verifies the OpenID answer with Steam, checks the license, sets the cookie |
| `GET /auth/itch.io` | Redirects to itch.io's OAuth page |
| `GET /auth/itch.io/callback` | A page that posts the itch.io token back |
| `POST /auth/itch.io/callback` | Checks the token's owned keys, sets the cookie |
| `GET /download` | Sends the zip to a holder of a valid license, with single-range resume |
| `GET /logout` | Clears the license cookie |

## Layout

`main.go` loads the config and runs the server until SIGTERM. Everything else is in `internal/`:

| Package | Owns |
| --- | --- |
| `config` | Environment variables, the URLs built from them, the upstream origins |
| `server` | Routes, handlers, turning a license check into redirects and the cookie, graceful shutdown |
| `steam` | OpenID login URL and assertion checks, `check_authentication`, `GetOwnedGames` |
| `itchio` | OAuth login URL, the callback page, reading the token from the POST body, the owned-keys scan |
| `license` | The signed `license` cookie and the cache of ownership answers |
| `download` | The zip, with single-range resume and a per-chunk write deadline |
| `upstream` | The HTTP client for Steam and itch.io, with its timeouts, redirect policy, size cap and logging |
| `httpx` | Redirects and JSON errors without extras, panic recovery, the request-body deadline |
| `compat` | The Bun, Elysia and WHATWG parsing rules the old backend's answers depended on |

## Tests

```sh
go test ./...
```

Unit tests sit next to each package. `internal/server` holds the end-to-end tests; some run the server on a real socket and listen on a free port in `127.0.0.1:39560-39599`.

`go build -tags parity` makes a binary for the parity harness. It reads `TEST_STEAM_COMMUNITY_ORIGIN`, `TEST_STEAM_API_ORIGIN` and `TEST_ITCH_API_ORIGIN` so a mock can stand in for Steam and itch.io. Builds without the tag have no such hook.

## License

[MIT](./LICENSE)

## Donate

[hloth.dev/donate](https://hloth.dev/donate)
