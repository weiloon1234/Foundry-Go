package authenticating_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"foundry.test/consumer/authenticating"
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/auth/token"
	tokenpg "github.com/weiloon1234/Foundry-Go/auth/token/postgres"
	"github.com/weiloon1234/Foundry-Go/database"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/testkit"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
	"github.com/weiloon1234/Foundry-Go/value"
)

const origin = "https://portal.test"

// browserCall is a same-origin fetch from the SPA; the browser attaches only
// the cookie named for this portal.
func browserCall(t *testing.T, router http.Handler, path, cookieName, cookieValue, bearer string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest("POST", origin+path, nil)
	r.Header.Set("Origin", origin)
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	if cookieValue != "" {
		r.AddCookie(&http.Cookie{Name: cookieName, Value: cookieValue})
	}
	if bearer != "" {
		r.Header.Set("Authorization", "Bearer "+bearer)
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, r)
	return w
}

func refreshCookieOf(t *testing.T, w *httptest.ResponseRecorder, name string) *http.Cookie {
	t.Helper()
	for _, cookie := range w.Result().Cookies() {
		if cookie.Name == name {
			return cookie
		}
	}
	return nil
}

func accessToken(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Tokens map[string]json.RawMessage `json:"tokens"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if _, present := body.Tokens["refresh_token"]; present {
		t.Fatal("browser JSON contains a refresh token")
	}
	var access string
	if err := json.Unmarshal(body.Tokens["access_token"], &access); err != nil || access == "" {
		t.Fatal("browser JSON has no access token", err)
	}
	return access
}

func TestBrowserRefreshCookieRotatesRevokesOnReplayAndClearsOnLogout(t *testing.T) {
	db := pgtest.Open(t)
	schema := pgtest.Namespace(t, db)
	if err := db.Transaction(t.Context(), func(tx *database.Tx) error {
		if _, err := tx.Exec(t.Context(), `SET LOCAL search_path TO "`+schema+`"`); err != nil {
			return err
		}
		for _, definition := range tokenpg.Migrations() {
			for _, statement := range definition.SQL {
				if _, err := tx.Exec(t.Context(), statement); err != nil {
					return err
				}
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	id, err := model.NewID[models.User]()
	if err != nil {
		t.Fatal(err)
	}
	user := models.User{ID: id, Status: models.StatusActive}
	provider := auth.DefineProvider("users", (models.User{}).FoundryReference(), func(_ context.Context, key model.ID[models.User]) (value.Optional[models.User], error) {
		if key != id {
			return value.Optional[models.User]{}, nil
		}
		return value.Set(user), nil
	}, func(context.Context, models.User) (bool, error) { return true, nil })
	now := testkit.NewClock(time.Date(2026, 10, 1, 1, 0, 0, 0, time.UTC))
	persistence := tokenpg.DefaultConfig()
	persistence.Schema, persistence.Clock = schema, now
	config := token.DefaultConfig(keyspace.Namespace{Application: "browser-consumer", Environment: "test"})
	users, err := authenticating.APITokens(db, provider, persistence, config)
	if err != nil {
		t.Fatal(err)
	}
	// A second guard on the same origin uses its own cookie.
	backend, err := tokenpg.New(db, persistence)
	if err != nil {
		t.Fatal(err)
	}
	store, err := token.NewStore(backend, config)
	if err != nil {
		t.Fatal(err)
	}
	scopes, err := authenticating.OrderReadScopes()
	if err != nil {
		t.Fatal(err)
	}
	// Admin families expire sooner than the store's user lifetimes.
	adminLifetime := token.Lifetime{Access: 5 * time.Minute, RefreshIdle: 12 * time.Hour, Absolute: 24 * time.Hour}
	admins, err := token.New(store, "users.admin", provider, "admin.bearer", scopes, token.WithLifetimes(token.Lifetimes{Renewable: adminLifetime}))
	if err != nil {
		t.Fatal(err)
	}
	proof, err := auth.NewProof(user.FoundryReference(), auth.Authenticated)
	if err != nil {
		t.Fatal(err)
	}
	verify := func(context.Context) (auth.Proof[models.User, model.ID[models.User]], error) { return proof, nil }
	csrf := foundryhttp.CSRFConfig{}
	userCookie, adminCookie := foundryhttp.DefineRefreshCookie("__Host-refresh-user", csrf), foundryhttp.DefineRefreshCookie("__Host-refresh-admin", csrf)
	userRoutes, err := authenticating.BrowserTokenRoutes(users, now, verify, userCookie, "web")
	if err != nil {
		t.Fatal(err)
	}
	adminRoutes, err := authenticating.BrowserTokenRoutes(admins, now, verify, adminCookie, "admin")
	if err != nil {
		t.Fatal(err)
	}

	login := browserCall(t, userRoutes, "/web/login", "", "", "")
	first := refreshCookieOf(t, login, "__Host-refresh-user")
	if login.Code != 200 || first == nil || !first.HttpOnly || !first.Secure || first.SameSite != http.SameSiteStrictMode || first.Path != "/" || first.MaxAge <= 0 {
		t.Fatal("web login did not set the refresh cookie", login.Code, first)
	}
	accessToken(t, login)
	adminLogin := browserCall(t, adminRoutes, "/admin/login", "", "", "")
	adminSession := refreshCookieOf(t, adminLogin, "__Host-refresh-admin")
	if adminLogin.Code != 200 || adminSession == nil {
		t.Fatal("admin login did not set its own cookie", adminLogin.Code)
	}
	if adminSession.MaxAge != int(adminLifetime.RefreshIdle/time.Second) || first.MaxAge != int(config.Renewable.RefreshIdle/time.Second) {
		t.Fatal("refresh cookies do not follow each guard's lifetime", adminSession.MaxAge, first.MaxAge)
	}

	now.Advance(time.Minute)
	rotated := browserCall(t, userRoutes, "/web/refresh", first.Name, first.Value, "")
	second := refreshCookieOf(t, rotated, "__Host-refresh-user")
	if rotated.Code != 200 || second == nil || second.Value == first.Value {
		t.Fatal("refresh did not rotate the cookie", rotated.Code)
	}
	accessToken(t, rotated)
	// The admin cookie never reaches the user refresh endpoint, and vice versa.
	if w := browserCall(t, userRoutes, "/web/refresh", adminSession.Name, adminSession.Value, ""); w.Code != 401 {
		t.Fatal("admin cookie refreshed a user family", w.Code)
	}

	for name, request := range map[string]func(*http.Request){
		"cross-origin": func(r *http.Request) {
			r.Header.Set("Origin", "https://attacker.test")
			r.Header.Set("Sec-Fetch-Site", "cross-site")
		},
		"origin-less": func(r *http.Request) { r.Header.Del("Origin"); r.Header.Del("Sec-Fetch-Site") },
	} {
		r := httptest.NewRequest("POST", origin+"/web/refresh", nil)
		r.Header.Set("Origin", origin)
		r.AddCookie(&http.Cookie{Name: second.Name, Value: second.Value})
		request(r)
		w := httptest.NewRecorder()
		userRoutes.ServeHTTP(w, r)
		if w.Code != 403 || len(w.Header().Values("Set-Cookie")) != 0 {
			t.Fatalf("%s refresh was not rejected: %d", name, w.Code)
		}
	}

	// Replaying the consumed cookie revokes the family and clears the cookie;
	// the successor stops working too.
	replay := browserCall(t, userRoutes, "/web/refresh", first.Name, first.Value, "")
	if cleared := refreshCookieOf(t, replay, "__Host-refresh-user"); replay.Code != 401 || cleared == nil || cleared.MaxAge >= 0 {
		t.Fatal("replayed cookie did not revoke and clear", replay.Code)
	}
	if w := browserCall(t, userRoutes, "/web/refresh", second.Name, second.Value, ""); w.Code != 401 {
		t.Fatal("successor survived refresh-token replay", w.Code)
	}
	// Logging out with the revoked family's cookie still succeeds and clears it.
	if w := browserCall(t, userRoutes, "/web/logout", second.Name, second.Value, ""); w.Code != 204 || refreshCookieOf(t, w, "__Host-refresh-user") == nil {
		t.Fatal("logout of a revoked family failed", w.Code)
	}

	// A user family's secret presented to the admin guard revokes nothing there.
	relogin := browserCall(t, userRoutes, "/web/login", "", "", "")
	third := refreshCookieOf(t, relogin, "__Host-refresh-user")
	if relogin.Code != 200 || third == nil {
		t.Fatal("web login after logout", relogin.Code)
	}
	if w := browserCall(t, adminRoutes, "/admin/logout", adminSession.Name, third.Value, ""); w.Code != 204 {
		t.Fatal("unknown admin refresh secret failed logout", w.Code)
	}
	if w := browserCall(t, userRoutes, "/web/refresh", third.Name, third.Value, ""); w.Code != 200 {
		t.Fatal("another guard's logout revoked the user family", w.Code)
	}

	// After the admin access token expired, the cookie alone logs out and clears
	// only the admin cookie.
	now.Advance(config.Renewable.Access)
	logout := browserCall(t, adminRoutes, "/admin/logout", adminSession.Name, adminSession.Value, "")
	if cleared := refreshCookieOf(t, logout, "__Host-refresh-admin"); logout.Code != 204 || cleared == nil || cleared.MaxAge >= 0 || refreshCookieOf(t, logout, "__Host-refresh-user") != nil {
		t.Fatal("logout did not clear the admin cookie", logout.Code)
	}
	if w := browserCall(t, adminRoutes, "/admin/refresh", adminSession.Name, adminSession.Value, ""); w.Code != 401 {
		t.Fatal("refresh succeeded after logout", w.Code)
	}
	if w := browserCall(t, adminRoutes, "/admin/logout", "", "", ""); w.Code != 204 || refreshCookieOf(t, w, "__Host-refresh-admin") == nil {
		t.Fatal("logout without a cookie was not idempotent", w.Code)
	}
	if !strings.HasPrefix(adminSession.Name, "__Host-") {
		t.Fatal("cookie lost its prefix")
	}
}
