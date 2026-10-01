package http_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/auth/token"
	"github.com/weiloon1234/Foundry-Go/fault"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/testkit"
)

const refreshCookieName = "__Host-refresh-user"

type refreshCookieInput = foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, foundryhttp.RefreshTokenRequest]
type refreshLogoutInput = foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, foundryhttp.RefreshCookieLogoutRequest]

func refreshCookie(t *testing.T) foundryhttp.RefreshCookie {
	t.Helper()
	cookie := foundryhttp.DefineRefreshCookie(refreshCookieName, foundryhttp.CSRFConfig{})
	if err := cookie.Validate(); err != nil {
		t.Fatal(err)
	}
	return cookie
}

func refreshRoute(id foundryhttp.RouteID, path string) foundryhttp.Route[foundryhttp.NoPath] {
	return foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: id, Method: foundryhttp.POST, Access: foundryhttp.Public}, foundryhttp.StaticPath(path))
}

// browserRequest is a same-origin HTTPS fetch, optionally carrying the cookie.
func browserRequest(path, cookie string, body string) *http.Request {
	request := httptest.NewRequest("POST", "https://app.test"+path, strings.NewReader(body))
	request.Header.Set("Origin", "https://app.test")
	request.Header.Set("Sec-Fetch-Site", "same-origin")
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	if cookie != "" {
		request.AddCookie(&http.Cookie{Name: refreshCookieName, Value: cookie})
	}
	return request
}

func serveBrowser(h http.Handler, request *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	h.ServeHTTP(w, request)
	return w
}

// cleared reports whether the response expires the refresh cookie.
func cleared(w *httptest.ResponseRecorder) bool {
	cookies := w.Result().Cookies()
	return len(cookies) == 1 && cookies[0].Name == refreshCookieName && cookies[0].MaxAge < 0 && cookies[0].HttpOnly && cookies[0].Secure && cookies[0].Path == "/"
}

func setCookie(t *testing.T, w *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	cookies := w.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != refreshCookieName {
		t.Fatalf("expected one refresh cookie, got %v", w.Header().Values("Set-Cookie"))
	}
	return cookies[0]
}

func TestTokenCookieResponseSetsAnHttpOnlyRefreshCookieAndOmitsItFromJSON(t *testing.T) {
	issued, now := tokenWireIssue(t, token.Renewable)
	cookie := refreshCookie(t)
	calls := 0
	login := foundryhttp.DefineEndpoint(refreshRoute("web.login", "/login"), foundryhttp.EmptyQuery(), foundryhttp.EmptyBody(), foundryhttp.TokenCookieResponse[authAccount, int64](cookie, 200, now))
	router := newAuthRouter(t, login.Handle(func(context.Context, authInput) (tokenWireIssued, error) { calls++; return issued, nil }))
	now.Advance(1500 * time.Millisecond)
	w := serveBrowser(router, browserRequest("/login", "", ""))
	if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("cookie login response", w.Code, w.Body.String())
	}
	refresh, _ := issued.RefreshSecret().Get()
	if strings.Contains(w.Body.String(), "refresh_token") || strings.Contains(w.Body.String(), refresh.Reveal()) {
		t.Fatal("refresh credential reached the JSON body")
	}
	var wire struct {
		Tokens struct {
			AccessToken string `json:"access_token"`
			ExpiresIn   int64  `json:"expires_in"`
			TokenType   string `json:"token_type"`
		} `json:"tokens"`
		MFARequired bool `json:"mfa_required"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &wire); err != nil || wire.Tokens.AccessToken != issued.AccessSecret().Reveal() || wire.Tokens.TokenType != "Bearer" || wire.MFARequired {
		t.Fatal("access token wire contract", w.Body.String(), err)
	}
	set := setCookie(t, w)
	deadline, _ := issued.Info().RefreshExpiresAt().Get()
	want := int(deadline.UTC().Sub(now.Now()).Truncate(time.Second) / time.Second)
	if set.Value != refresh.Reveal() || !set.HttpOnly || !set.Secure || set.SameSite != http.SameSiteStrictMode || set.Path != "/" || set.Domain != "" || set.MaxAge != want {
		t.Fatalf("refresh cookie attributes: %+v want max-age %d", set, want)
	}
	// Login CSRF: a cross-site or origin-less request never reaches the handler.
	for _, origin := range []string{"https://attacker.test", ""} {
		request := browserRequest("/login", "", "")
		request.Header.Del("Sec-Fetch-Site")
		request.Header.Del("Origin")
		if origin != "" {
			request.Header.Set("Origin", origin)
		}
		if w := serveBrowser(router, request); w.Code != 403 || len(w.Header().Values("Set-Cookie")) != 0 {
			t.Fatal("cross-origin login was not rejected", origin, w.Code)
		}
	}
	plain := browserRequest("/login", "", "")
	plain.URL.Scheme, plain.TLS = "http", nil
	if w := serveBrowser(router, plain); w.Code != 400 {
		t.Fatal("cookie delivery without TLS", w.Code)
	}
	if calls != 1 {
		t.Fatal("rejected requests reached the handler", calls)
	}
	personal, now := tokenWireIssue(t, token.Personal)
	personalLogin := foundryhttp.DefineEndpoint(refreshRoute("web.personal", "/personal"), foundryhttp.EmptyQuery(), foundryhttp.EmptyBody(), foundryhttp.TokenCookieResponse[authAccount, int64](cookie, 200, now))
	router = newAuthRouter(t, personalLogin.Handle(func(context.Context, authInput) (tokenWireIssued, error) { return personal, nil }))
	if w := serveBrowser(router, browserRequest("/personal", "", "")); w.Code != 200 || len(w.Header().Values("Set-Cookie")) != 0 {
		t.Fatal("a personal token set a refresh cookie", w.Code)
	}
}

func TestRefreshTokenCookieReadsOnlyTheCookieAndClearsItOnAuthenticationFailure(t *testing.T) {
	first, now := tokenWireIssue(t, token.Renewable)
	rotated, _ := tokenWireIssue(t, token.Renewable)
	cookie := refreshCookie(t)
	original, _ := first.RefreshSecret().Get()
	var failure error
	var received string
	refresh := foundryhttp.DefineEndpoint(refreshRoute("web.refresh", "/refresh"), foundryhttp.EmptyQuery(), foundryhttp.RefreshTokenCookie(cookie), foundryhttp.TokenCookieResponse[authAccount, int64](cookie, 200, now))
	router := newAuthRouter(t, refresh.Handle(func(_ context.Context, input refreshCookieInput) (tokenWireIssued, error) {
		received = input.Body.RefreshToken.Secret().Reveal()
		return rotated, failure
	}))
	w := serveBrowser(router, browserRequest("/refresh", original.Reveal(), ""))
	next, _ := rotated.RefreshSecret().Get()
	if w.Code != 200 || received != original.Reveal() || setCookie(t, w).Value != next.Reveal() {
		t.Fatal("cookie refresh did not rotate the cookie", w.Code, w.Body.String())
	}
	// A missing cookie or a revoked/replayed family clears it in the 401.
	if w := serveBrowser(router, browserRequest("/refresh", "", "")); w.Code != 401 || !cleared(w) {
		t.Fatal("missing cookie was not cleared", w.Code)
	}
	if w := serveBrowser(router, browserRequest("/refresh", "not-a-token", "")); w.Code != 401 || !cleared(w) {
		t.Fatal("malformed cookie was not cleared", w.Code)
	}
	failure = auth.Unauthenticated
	if w := serveBrowser(router, browserRequest("/refresh", original.Reveal(), "")); w.Code != 401 || !cleared(w) {
		t.Fatal("replayed family was not cleared", w.Code)
	}
	// Transient failures, bodies, query strings and cross-site requests keep it.
	failure = fault.New(fault.Overloaded, "busy")
	if w := serveBrowser(router, browserRequest("/refresh", original.Reveal(), "")); w.Code == 200 || w.Code == 401 || len(w.Header().Values("Set-Cookie")) != 0 {
		t.Fatal("transient failure cleared the cookie", w.Code)
	}
	failure = nil
	if w := serveBrowser(router, browserRequest("/refresh", original.Reveal(), `{"refresh_token":"x"}`)); w.Code == 200 || len(w.Header().Values("Set-Cookie")) != 0 {
		t.Fatal("refresh accepted a body", w.Code)
	}
	query := browserRequest("/refresh?refresh_token="+original.Reveal(), original.Reveal(), "")
	if w := serveBrowser(router, query); w.Code != 400 || len(w.Header().Values("Set-Cookie")) != 0 {
		t.Fatal("refresh accepted a query string", w.Code)
	}
	cross := browserRequest("/refresh", original.Reveal(), "")
	cross.Header.Set("Origin", "https://attacker.test")
	cross.Header.Set("Sec-Fetch-Site", "cross-site")
	if w := serveBrowser(router, cross); w.Code != 403 || len(w.Header().Values("Set-Cookie")) != 0 {
		t.Fatal("cross-site refresh was not rejected without clearing", w.Code)
	}
	description, err := refresh.Description()
	if err != nil || description.Body != nil || description.RefreshCookie == nil || *description.RefreshCookie != (foundryhttp.RefreshCookieInfo{Name: refreshCookieName, Reads: true, Sets: true}) {
		t.Fatal("refresh cookie metadata", description.RefreshCookie, err)
	}
	if strings.Contains(string(mustJSON(t, description.Response.Schema)), "refresh_token") {
		t.Fatal("cookie response schema exposes refresh_token")
	}
}

func TestClearRefreshCookieOnLogoutAndRejectsMisuse(t *testing.T) {
	cookie := refreshCookie(t)
	logout := foundryhttp.DefineEndpoint(refreshRoute("web.logout", "/logout"), foundryhttp.EmptyQuery(), foundryhttp.EmptyBody(), foundryhttp.ClearRefreshCookie(cookie, foundryhttp.EmptyResponse(204)))
	router := newAuthRouter(t, logout.Handle(func(context.Context, authInput) (foundryhttp.NoContent, error) { return foundryhttp.NoContent{}, nil }))
	w := serveBrowser(router, browserRequest("/logout", "", ""))
	if w.Code != 204 || setCookie(t, w).MaxAge >= 0 {
		t.Fatal("logout did not clear the refresh cookie", w.Code)
	}
	for name, err := range map[string]error{
		"plain name":  foundryhttp.DefineRefreshCookie("refresh", foundryhttp.CSRFConfig{}).Validate(),
		"bad origin":  foundryhttp.DefineRefreshCookie(refreshCookieName, foundryhttp.CSRFConfig{TrustedOrigins: []foundryhttp.Origin{"null"}}).Validate(),
		"two uses":    foundryhttp.ClearRefreshCookie(cookie, foundryhttp.TokenCookieResponse[authAccount, int64](cookie, 200, testkitClock())).Validate(),
		"get":         foundryhttp.DefineEndpoint(foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "web.get", Method: foundryhttp.GET, Access: foundryhttp.Public}, foundryhttp.StaticPath("/")), foundryhttp.EmptyQuery(), foundryhttp.EmptyBody(), foundryhttp.ClearRefreshCookie(cookie, foundryhttp.EmptyResponse(204))).Validate(),
		"two cookies": foundryhttp.DefineEndpoint(refreshRoute("web.mixed", "/mixed"), foundryhttp.EmptyQuery(), foundryhttp.RefreshTokenCookie(cookie), foundryhttp.TokenCookieResponse[authAccount, int64](foundryhttp.DefineRefreshCookie("__Host-refresh-admin", foundryhttp.CSRFConfig{}), 200, testkitClock())).Validate(),
		// The consumed cookie must be replaced or cleared, never left for a replay.
		"read without set": foundryhttp.DefineEndpoint(refreshRoute("web.json", "/json"), foundryhttp.EmptyQuery(), foundryhttp.RefreshTokenCookie(cookie), foundryhttp.TokenResponse[authAccount, int64](200, testkitClock())).Validate(),
	} {
		if !errors.Is(err, fault.Invalid) {
			t.Fatalf("%s accepted: %v", name, err)
		}
	}
}

// The refresh cookie authenticates a browser logout by itself, so an expired
// access token cannot strand a live family. Without a usable cookie there is
// nothing to revoke and logout still succeeds; a transient failure keeps it.
func TestRefreshTokenCookieLogoutIsIdempotentAndKeepsTheCookieOnFailure(t *testing.T) {
	issued, _ := tokenWireIssue(t, token.Renewable)
	presented, _ := issued.RefreshSecret().Get()
	cookie := refreshCookie(t)
	var failure error
	var received []string
	logout := foundryhttp.DefineEndpoint(refreshRoute("web.logout", "/logout"), foundryhttp.EmptyQuery(), foundryhttp.RefreshTokenCookieLogout(cookie), foundryhttp.ClearRefreshCookie(cookie, foundryhttp.EmptyResponse(204)))
	router := newAuthRouter(t, logout.Handle(func(_ context.Context, input refreshLogoutInput) (foundryhttp.NoContent, error) {
		refresh, present := input.Body.RefreshToken.Get()
		if !present {
			received = append(received, "")
		} else {
			received = append(received, refresh.Secret().Reveal())
		}
		return foundryhttp.NoContent{}, failure
	}))
	if w := serveBrowser(router, browserRequest("/logout", presented.Reveal(), "")); w.Code != 204 || !cleared(w) {
		t.Fatal("cookie logout did not clear the cookie", w.Code)
	}
	for _, value := range []string{"", "not-a-token"} {
		if w := serveBrowser(router, browserRequest("/logout", value, "")); w.Code != 204 || !cleared(w) {
			t.Fatal("logout without a usable cookie failed", value, w.Code)
		}
	}
	if len(received) != 3 || received[0] != presented.Reveal() || received[1] != "" || received[2] != "" {
		t.Fatal("logout handler received", len(received))
	}
	// A rejected credential is cleared like a refresh's; a transient failure keeps it.
	failure = auth.Unauthenticated
	if w := serveBrowser(router, browserRequest("/logout", presented.Reveal(), "")); w.Code != 401 || !cleared(w) {
		t.Fatal("rejected logout credential was not cleared", w.Code)
	}
	failure = fault.New(fault.Overloaded, "busy")
	if w := serveBrowser(router, browserRequest("/logout", presented.Reveal(), "")); w.Code < 500 || len(w.Header().Values("Set-Cookie")) != 0 {
		t.Fatal("transient logout failure cleared the cookie", w.Code)
	}
	failure = nil
	// Bodies and cross-site requests never reach the handler or touch the cookie.
	if w := serveBrowser(router, browserRequest("/logout", presented.Reveal(), `{"refresh_token":"x"}`)); w.Code == 204 || len(w.Header().Values("Set-Cookie")) != 0 {
		t.Fatal("logout accepted a body", w.Code)
	}
	cross := browserRequest("/logout", presented.Reveal(), "")
	cross.Header.Set("Origin", "https://attacker.test")
	cross.Header.Set("Sec-Fetch-Site", "cross-site")
	if w := serveBrowser(router, cross); w.Code != 403 || len(w.Header().Values("Set-Cookie")) != 0 {
		t.Fatal("cross-site logout was not rejected without clearing", w.Code)
	}
	if len(received) != 5 {
		t.Fatal("rejected requests reached the handler", len(received))
	}
	description, err := logout.Description()
	if err != nil || description.Body != nil || description.RefreshCookie == nil || *description.RefreshCookie != (foundryhttp.RefreshCookieInfo{Name: refreshCookieName, Reads: true, Clears: true, Optional: true}) {
		t.Fatal("cookie logout metadata", description.RefreshCookie, err)
	}
	for name, err := range map[string]error{
		"sets":         foundryhttp.DefineEndpoint(refreshRoute("web.logout.sets", "/sets"), foundryhttp.EmptyQuery(), foundryhttp.RefreshTokenCookieLogout(cookie), foundryhttp.TokenCookieResponse[authAccount, int64](cookie, 200, testkitClock())).Validate(),
		"keeps":        foundryhttp.DefineEndpoint(refreshRoute("web.logout.keeps", "/keeps"), foundryhttp.EmptyQuery(), foundryhttp.RefreshTokenCookieLogout(cookie), foundryhttp.EmptyResponse(204)).Validate(),
		"optional set": foundryhttp.RefreshCookieInfo{Name: refreshCookieName, Reads: true, Sets: true, Optional: true}.Validate(),
		"optional off": foundryhttp.RefreshCookieInfo{Name: refreshCookieName, Clears: true, Optional: true}.Validate(),
	} {
		if !errors.Is(err, fault.Invalid) {
			t.Fatalf("%s accepted: %v", name, err)
		}
	}
}

func testkitClock() *testkit.Clock {
	return testkit.NewClock(time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC))
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
