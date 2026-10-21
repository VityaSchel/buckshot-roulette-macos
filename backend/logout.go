package main

import "net/http"

// The old /logout never checked the signature: any license cookie, even an empty one, is cleared.
func (a *app) logout(w http.ResponseWriter, r *http.Request) {
	if _, found := requestCookie(r, licenseCookieName); found {
		w.Header().Add("Set-Cookie", clearLicenseCookie)
	}
	redirect(w, a.cfg.frontendURL("try-again"))
}
