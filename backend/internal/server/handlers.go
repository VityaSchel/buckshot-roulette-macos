package server

import (
	"errors"
	"log"
	"net/http"

	"git.hloth.dev/hloth/buckshot-roulette-macos/backend/internal/download"
	"git.hloth.dev/hloth/buckshot-roulette-macos/backend/internal/httpx"
	"git.hloth.dev/hloth/buckshot-roulette-macos/backend/internal/itchio"
	"git.hloth.dev/hloth/buckshot-roulette-macos/backend/internal/license"
	"git.hloth.dev/hloth/buckshot-roulette-macos/backend/internal/steam"
)

func redirectTo(location string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		httpx.Redirect(w, location)
	}
}

func (a *App) redirectToFrontend(w http.ResponseWriter, fragment string) {
	httpx.Redirect(w, a.cfg.FrontendURL(fragment))
}

func (a *App) steamCallback(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.licenseCookie(w, r); !ok {
		return
	}
	steamID, err := a.steam.Verify(r.Context(), r.URL.RawQuery)
	if err != nil {
		failure := steam.ErrUnavailable
		errors.As(err, &failure)
		httpx.Error(w, failure.Status, failure.Message)
		return
	}
	a.finishLicenseCheck(w, "steam", a.steam.License(r.Context(), steamID))
}

func (a *App) itchCallback(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.licenseCookie(w, r); !ok {
		return
	}
	token, err := itchio.ReadToken(w, r)
	if err != nil {
		log.Printf("itch.io callback: %v", err)
		httpx.ServerError(w)
		return
	}
	a.finishLicenseCheck(w, "itch.io", a.itch.License(r.Context(), token))
}

func (a *App) download(w http.ResponseWriter, r *http.Request) {
	cookie, ok := a.licenseCookie(w, r)
	if !ok {
		return
	}
	if !cookie.ActiveAt(a.now()) {
		a.redirectToFrontend(w, "auth")
		return
	}
	if err := download.Serve(w, r, a.cfg.DownloadsDir, a.stallTimeout); err != nil {
		log.Printf("download: %v", err)
		httpx.ServerError(w)
	}
}

// The old /logout never checked the signature: any license cookie, even an empty one, is cleared.
func (a *App) logout(w http.ResponseWriter, r *http.Request) {
	if license.Sent(r) {
		w.Header().Add("Set-Cookie", license.ClearCookie)
	}
	a.redirectToFrontend(w, "try-again")
}

// licenseCookie answers the request itself when the license cookie is unusable, as Elysia's
// cookie validation did before any handler ran.
func (a *App) licenseCookie(w http.ResponseWriter, r *http.Request) (license.Cookie, bool) {
	cookie, err := license.Read(r, a.cfg.JWTSecret)
	switch {
	case errors.Is(err, license.ErrSignature):
		httpx.Redirect(w, a.badLicenseTarget)
		return license.Cookie{}, false
	case err != nil:
		log.Print(err)
		httpx.ServerError(w)
		return license.Cookie{}, false
	}
	return cookie, true
}

// finishLicenseCheck is Elysia's checkLicense macro: every result becomes a redirect to the frontend.
func (a *App) finishLicenseCheck(w http.ResponseWriter, service string, result license.Result) {
	switch result {
	case license.Owned:
		w.Header().Add("Set-Cookie", license.Issue(a.now(), a.cfg.JWTSecret))
		a.redirectToFrontend(w, "success")
	case license.NotOwned:
		a.redirectToFrontend(w, "error-"+service+"-no-license")
	default:
		a.redirectToFrontend(w, "error-"+service+"-license-unknown")
	}
}
