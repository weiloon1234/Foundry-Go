package http_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/auth/token"
	"github.com/weiloon1234/Foundry-Go/clock"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/testkit"
	"github.com/weiloon1234/Foundry-Go/value"
)

type tokenWireIssued = token.Issued[authAccount, int64]
type tokenWireClock func() time.Time

func (f tokenWireClock) Now() time.Time { return f() }

// This stub only supports issuance. Authentication/replay tests use PostgreSQL.
type tokenWireBackend struct {
	token.Backend
	now clock.Clock
}

func (b tokenWireBackend) Create(_ context.Context, address token.Address, creation token.Creation) (token.Record, error) {
	return creation.At(address, b.now.Now())
}
func tokenWireIssue(t *testing.T, mode token.Mode) (tokenWireIssued, *testkit.Clock) {
	t.Helper()
	now := testkit.NewClock(time.Date(2026, 9, 16, 1, 0, 0, 0, time.UTC))
	store, err := token.NewStore(tokenWireBackend{now: now}, token.DefaultConfig(keyspace.Namespace{Application: "token-http", Environment: "test"}))
	if err != nil {
		t.Fatal(err)
	}
	provider := auth.DefineProvider("accounts", (authAccount{}).reference(), func(_ context.Context, id int64) (value.Optional[authAccount], error) {
		return value.Set(authAccount{ID: id, Enabled: true}), nil
	}, func(_ context.Context, a authAccount) (bool, error) { return a.Enabled, nil })
	tokens, err := token.New(store, "api", provider, "bearer", auth.AccessScopes[authAccount]{})
	if err != nil {
		t.Fatal(err)
	}
	assurance := auth.Authenticated
	if mode == token.Challenge {
		assurance = auth.PendingMFA
	}
	proof, err := auth.NewProof(authAccount{ID: 7}.reference(), assurance)
	if err != nil {
		t.Fatal(err)
	}
	issued, err := tokens.Issue(t.Context(), proof, token.IssueOptions[authAccount]{Refresh: mode == token.Renewable})
	if err != nil {
		t.Fatal(err)
	}
	return issued, now
}
func tokenWireEndpoint(response foundryhttp.Response[tokenWireIssued]) foundryhttp.Endpoint[foundryhttp.NoPath, foundryhttp.NoQuery, foundryhttp.NoBody, tokenWireIssued] {
	return foundryhttp.DefineEndpoint(foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "token.issue", Method: foundryhttp.POST, Access: foundryhttp.Public}, foundryhttp.StaticPath("/token")), foundryhttp.EmptyQuery(), foundryhttp.EmptyBody(), response)
}
func tokenWireServe(h http.Handler, scheme string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", scheme+"://app.test/token", nil))
	return w
}
func TestTokenDeliveryUsesTypedWireContract(t *testing.T) {
	for _, mode := range []token.Mode{token.Personal, token.Renewable, token.Challenge} {
		t.Run(fmt.Sprint(mode), func(t *testing.T) {
			issued, now := tokenWireIssue(t, mode)
			// expires_in is remaining whole seconds, not the original lifetime.
			now.Advance(1500 * time.Millisecond)
			endpoint := tokenWireEndpoint(foundryhttp.TokenResponse[authAccount, int64](201, now))
			router := newAuthRouter(t, endpoint.Handle(func(context.Context, authInput) (tokenWireIssued, error) { return issued, nil }))
			w := tokenWireServe(router, "https")
			if w.Code != 201 || w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("Pragma") != "no-cache" || w.Header().Get("Content-Type") != "application/json" {
				t.Fatal("token response headers", w.Code)
			}
			var wire struct {
				Tokens struct {
					AccessToken  string  `json:"access_token"`
					RefreshToken *string `json:"refresh_token"`
					ExpiresIn    int64   `json:"expires_in"`
					TokenType    string  `json:"token_type"`
				} `json:"tokens"`
				MFARequired bool `json:"mfa_required"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &wire); err != nil {
				t.Fatal(err)
			}
			if wire.Tokens.AccessToken != issued.AccessSecret().Reveal() || wire.Tokens.TokenType != "Bearer" || wire.MFARequired != (mode == token.Challenge) || wire.Tokens.ExpiresIn != int64(issued.Info().AccessExpiresAt().UTC().Sub(now.Now())/time.Second) {
				t.Fatal("wire contract did not preserve issuance")
			}
			refresh, present := issued.RefreshSecret().Get()
			if (wire.Tokens.RefreshToken != nil) != present || present && *wire.Tokens.RefreshToken != refresh.Reveal() {
				t.Fatal("refresh field")
			}
			raw, err := json.Marshal(issued)
			if err != nil || strings.Contains(string(raw), wire.Tokens.AccessToken) || strings.Contains(fmt.Sprintf("%#v", issued), wire.Tokens.AccessToken) {
				t.Fatal("ordinary serialization disclosed credential")
			}
			metadata, err := endpoint.Description()
			if err != nil || metadata.Response == nil || !reflect.DeepEqual(metadata, router.Endpoints()[0]) {
				t.Fatal("response metadata", err)
			}
			if metadata.Response.Schema.Root == "" || len(metadata.Response.Schema.Types) < 4 {
				t.Fatal("missing nested token wire schema")
			}
		})
	}
}
func TestTokenDeliveryRequiresSecurePOSTBeforeHandler(t *testing.T) {
	issued, now := tokenWireIssue(t, token.Personal)
	response := foundryhttp.TokenResponse[authAccount, int64](200, now)
	for _, method := range []foundryhttp.Method{foundryhttp.GET, foundryhttp.HEAD, foundryhttp.PUT, foundryhttp.DELETE} {
		endpoint := foundryhttp.DefineEndpoint(foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "unsafe", Method: method, Access: foundryhttp.Public}, foundryhttp.StaticPath("/")), foundryhttp.EmptyQuery(), foundryhttp.EmptyBody(), response)
		if endpoint.Validate() == nil {
			t.Fatal("accepted token delivery method", method)
		}
	}
	calls := 0
	handler := func(context.Context, authInput) (tokenWireIssued, error) { calls++; return issued, nil }
	endpoint := tokenWireEndpoint(response)
	for _, trusted := range []bool{false, true} {
		route := endpoint
		if trusted {
			route = route.WithMiddleware(foundryhttp.TrustedProxy(foundryhttp.TrustedProxyConfig{
				Proxies: []netip.Prefix{netip.MustParsePrefix("192.0.2.0/24")}, OriginHeaders: []foundryhttp.ProxyOriginHeader{foundryhttp.ProxySchemeHeader("X-Forwarded-Proto")},
			}))
		}
		router := newAuthRouter(t, route.Handle(handler))
		request := httptest.NewRequest("POST", "http://app.test/token", nil)
		request.RemoteAddr = "192.0.2.1:1234"
		request.Header.Set("X-Forwarded-Proto", "https")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, request)
		expected := 400
		if trusted {
			expected = 200
		}
		if w.Code != expected || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("TLS trust", trusted, w.Code)
		}
	}
	if calls != 1 {
		t.Fatal("untrusted plaintext invoked handler")
	}
	var missing *testkit.Clock
	if foundryhttp.TokenResponse[authAccount, int64](200, missing).Validate() == nil || foundryhttp.TokenResponse[authAccount, int64](204, now).Validate() == nil {
		t.Fatal("invalid descriptor accepted")
	}
}
func TestTokenDeliveryDoesNotDiscloseOnFailure(t *testing.T) {
	for _, mode := range []string{"zero", "expired", "future", "limit", "cancel", "clock-panic", "clock-goexit", "handler-error"} {
		t.Run(mode, func(t *testing.T) {
			issued, now := tokenWireIssue(t, token.Renewable)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			var source clock.Clock = now
			if mode == "clock-panic" {
				source = tokenWireClock(func() time.Time { panic("private-clock-detail") })
			}
			if mode == "clock-goexit" {
				source = tokenWireClock(func() time.Time { runtime.Goexit(); return time.Time{} })
			}
			endpoint := tokenWireEndpoint(foundryhttp.TokenResponse[authAccount, int64](200, source))
			if mode == "limit" {
				limits := foundryhttp.DefaultEndpointLimits()
				limits.Response.Bytes = 8
				endpoint = endpoint.WithLimits(limits)
			}
			router := newAuthRouter(t, endpoint.Handle(func(context.Context, authInput) (tokenWireIssued, error) {
				switch mode {
				case "zero":
					return tokenWireIssued{}, nil
				case "expired":
					now.Set(issued.Info().AccessExpiresAt().UTC())
				case "future":
					now.Advance(-time.Second)
				case "cancel":
					cancel()
				case "handler-error":
					return issued, auth.Unauthenticated
				}
				return issued, nil
			}))
			w := httptest.NewRecorder()
			router.ServeHTTP(w, httptest.NewRequestWithContext(ctx, "POST", "https://app.test/token", nil))
			want := 500
			if mode == "cancel" {
				want = 408
			}
			if mode == "handler-error" {
				want = 401
			}
			if w.Code != want || w.Header().Get("Cache-Control") != "no-store" || strings.Contains(w.Body.String(), issued.AccessSecret().Reveal()) || strings.Contains(w.Body.String(), "private-clock-detail") {
				t.Fatal("failed response disclosed credential", mode, w.Code)
			}
		})
	}
}
func TestRefreshRequestIsBoundedValidatedAndRedacted(t *testing.T) {
	issued, now := tokenWireIssue(t, token.Renewable)
	refresh, _ := issued.RefreshSecret().Get()
	endpoint := foundryhttp.DefineEndpoint(foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "refresh", Method: foundryhttp.POST, Access: foundryhttp.Public}, foundryhttp.StaticPath("/refresh")), foundryhttp.EmptyQuery(), foundryhttp.RefreshTokenBody(), foundryhttp.TokenResponse[authAccount, int64](200, now))
	calls := 0
	router := newAuthRouter(t, endpoint.Handle(func(_ context.Context, in foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, foundryhttp.RefreshTokenRequest]) (tokenWireIssued, error) {
		calls++
		if in.Body.RefreshToken.Secret().Reveal() != refresh.Reveal() {
			t.Error("decoded credential lost")
		}
		raw, _ := json.Marshal(in.Body)
		if strings.Contains(string(raw), refresh.Reveal()) || strings.Contains(fmt.Sprintf("%#v", in.Body), refresh.Reveal()) {
			t.Error("request logging disclosed credential")
		}
		return issued, nil
	}))
	valid := `{"refresh_token":"` + refresh.Reveal() + `"}`
	for _, data := range []string{valid, `{}`, `{"refresh_token":null}`, `{"refresh_token":42}`, `{"refresh_token":"bad"}`, `{"refresh_token":"` + refresh.Reveal() + `","extra":1}`, `{"refresh_token":"` + refresh.Reveal() + `","refresh_token":"` + refresh.Reveal() + `"}`} {
		r := httptest.NewRequest("POST", "https://app.test/refresh", strings.NewReader(data))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		want := 400
		if data == valid {
			want = 200
		}
		if w.Code != want {
			t.Fatal("refresh body validation", w.Code)
		}
		if want == 400 && strings.Contains(w.Body.String(), refresh.Reveal()) {
			t.Fatal("invalid body error disclosed credential")
		}
	}
	if calls != 1 {
		t.Fatal("invalid body reached refresh handler")
	}
	r := httptest.NewRequest("POST", "https://app.test/refresh?refresh_token="+refresh.Reveal(), strings.NewReader(valid))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, r)
	if w.Code != 400 || calls != 1 {
		t.Fatal("credential in query accepted")
	}
}
