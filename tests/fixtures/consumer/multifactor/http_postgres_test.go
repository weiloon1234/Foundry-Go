package multifactor_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
	"time"

	"foundry.test/consumer/multifactor"
	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/auth/mfa"
	"github.com/weiloon1234/Foundry-Go/auth/session"
	"github.com/weiloon1234/Foundry-Go/clock"
	"github.com/weiloon1234/Foundry-Go/database"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/secret"
)

func (s *mfaFixture) browser(t *testing.T) *multifactor.Browser {
	t.Helper()
	registry, err := auth.NewRegistry(auth.DefaultConfig(), s.sessions.Guard().Registration())
	if err != nil {
		t.Fatal(err)
	}
	config := foundryhttp.DefaultBrowserSessionConfig()
	config.Clock = s.clock
	browser, err := foundryhttp.NewBrowserSessions(registry, s.sessions, config)
	if err != nil {
		t.Fatal(err)
	}
	return browser
}
func (s *mfaFixture) routes(t *testing.T) *foundryhttp.Router {
	t.Helper()
	verify := func(ctx context.Context, input multifactor.PasswordCredentials) (multifactor.PasswordResult, error) {
		var result multifactor.PasswordResult
		err := s.within(ctx, func(tx *database.Tx) error {
			login, err := multifactor.NewLogin(tx, s.provider, s.hasher)
			if err != nil {
				return err
			}
			result, err = login.Authenticate(ctx, input.Email, input.Password)
			return err
		})
		return result, err
	}
	router, err := multifactor.MFARoutes(s.factors, s.browser(t), s.tokens, s.clock, verify)
	if err != nil {
		t.Fatal(err)
	}
	return router
}
func mfaRequest(t *testing.T, handler http.Handler, path, body string, cookie secret.String, origin string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest("POST", "https://app.test"+path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	if origin != "" {
		request.Header.Set("Origin", origin)
	}
	if !cookie.IsZero() {
		request.AddCookie(&http.Cookie{Name: "__Host-foundry_session", Value: cookie.Reveal()})
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

const managementCredentials = `"credentials":{"email":"member@example.test","password":"private MFA password"}`

func TestPostgresMFAHTTPEnrollmentConfirmationAndRecoveryContracts(t *testing.T) {
	s := mfaSetup(t)
	router := s.routes(t)
	rejected := mfaRequest(t, router, "/mfa/enroll", "{"+managementCredentials+"}", secret.String{}, "https://foreign.test")
	if rejected.Code != 403 {
		t.Fatal("cross-origin enrollment reached password flow", rejected.Code)
	}
	start := mfaRequest(t, router, "/mfa/enroll", "{"+managementCredentials+"}", secret.String{}, "https://app.test")
	if start.Code != 201 || start.Header().Get("Cache-Control") != "no-store" || start.Header().Get("Pragma") != "no-cache" {
		t.Fatal("enrollment delivery", start.Code)
	}
	var enrollment struct {
		ID      string `json:"enrollment_id"`
		Secret  string `json:"secret"`
		URI     string `json:"provisioning_uri"`
		Expires string `json:"expires_at"`
	}
	if err := json.Unmarshal(start.Body.Bytes(), &enrollment); err != nil {
		t.Fatal(err)
	}
	factor, err := mfa.ParseTOTPSecret(secret.New(enrollment.Secret))
	if err != nil {
		t.Fatal("invalid delivered secret")
	}
	if _, err := mfa.ParseEnrollmentID[multifactor.Account](enrollment.ID); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(enrollment.URI, enrollment.Secret) || enrollment.Expires == "" {
		t.Fatal("missing enrollment wire fields")
	}
	code := authenticatorCode(t, factor, s.clock.Now()).Secret().Reveal()
	body := "{" + managementCredentials + `,"enrollment_id":"` + enrollment.ID + `","code":"` + code + `"}`
	confirm := mfaRequest(t, router, "/mfa/confirm", body, secret.String{}, "https://app.test")
	if confirm.Code != 200 || confirm.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("confirmation delivery", confirm.Code)
	}
	var issued struct {
		Codes []string `json:"recovery_codes"`
	}
	if err := json.Unmarshal(confirm.Body.Bytes(), &issued); err != nil {
		t.Fatal(err)
	}
	if len(issued.Codes) != 10 {
		t.Fatal("missing recovery set")
	}
	for _, raw := range issued.Codes {
		if _, err := mfa.ParseRecoveryCode(secret.New(raw)); err != nil {
			t.Fatal("invalid recovery wire value")
		}
	}
	if !s.current(t).MFAEnabled {
		t.Fatal("HTTP confirmation did not enable factor")
	}
	duplicate := mfaRequest(t, router, "/mfa/confirm", body, secret.String{}, "https://app.test")
	if duplicate.Code != 401 || strings.Contains(duplicate.Body.String(), issued.Codes[0]) {
		t.Fatal("confirmation replay disclosed a result", duplicate.Code)
	}
	s.clock.Advance(30 * time.Second)
	regenerate := "{" + managementCredentials + `,"code":"` + authenticatorCode(t, factor, s.clock.Now()).Secret().Reveal() + `"}`
	changed := mfaRequest(t, router, "/mfa/recovery", regenerate, secret.String{}, "https://app.test")
	if changed.Code != 200 {
		t.Fatal("recovery regeneration delivery", changed.Code)
	}
	var next struct {
		Codes []string `json:"recovery_codes"`
	}
	if err := json.Unmarshal(changed.Body.Bytes(), &next); err != nil || len(next.Codes) != 10 || next.Codes[0] == issued.Codes[0] {
		t.Fatal("recovery set was not replaced", err)
	}
	// Endpoint metadata comes from the same DTOs used for these actual responses.
	for _, endpoint := range router.Endpoints() {
		if endpoint.Route.ID == "mfa.enroll" && endpoint.Response == nil {
			t.Fatal("missing enrollment metadata")
		}
	}
}

func TestPostgresMFAHTTPCompletionCookiesTokensAndRejectedAttempts(t *testing.T) {
	for _, kind := range []string{"session", "token"} {
		t.Run(kind, func(t *testing.T) {
			s := mfaSetup(t)
			_, codes := s.enroll(t)
			kernel := s.kernel(t, kind)
			pending := pendingCredential(t, kernel)
			router := s.routes(t)
			path := "/mfa/web/recovery"
			body := `{"code":"` + codes.Codes()[0].Secret().Reveal() + `"}`
			cookie := pending
			origin := "https://app.test"
			if kind == "token" {
				path = "/mfa/api/recovery"
				body = `{"challenge":"` + pending.Reveal() + `","code":"` + codes.Codes()[0].Secret().Reveal() + `"}`
				cookie = secret.String{}
				origin = ""
			}
			if kind == "session" {
				rejected := mfaRequest(t, router, path, body, cookie, "https://foreign.test")
				if rejected.Code != 403 || len(rejected.Result().Cookies()) != 0 {
					t.Fatal("cross-origin completion", rejected.Code)
				}
				missing := mfaRequest(t, router, path, body, secret.String{}, origin)
				if missing.Code != 401 {
					t.Fatal("missing cookie accepted", missing.Code)
				}
			}
			w := mfaRequest(t, router, path, body, cookie, origin)
			var full secret.String
			if kind == "session" {
				cookies := w.Result().Cookies()
				if w.Code != 204 || len(cookies) != 1 || !cookies[0].Secure || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteLaxMode || cookies[0].Path != "/" {
					t.Fatal("full cookie staging", w.Code)
				}
				full = secret.New(cookies[0].Value)
				if strings.Contains(w.Body.String(), full.Reveal()) {
					t.Fatal("cookie secret appeared in body")
				}
			} else {
				if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" {
					t.Fatal("token delivery", w.Code)
				}
				var wire struct {
					Tokens struct {
						Access  string `json:"access_token"`
						Refresh string `json:"refresh_token"`
					} `json:"tokens"`
					MFA bool `json:"mfa_required"`
				}
				if err := json.Unmarshal(w.Body.Bytes(), &wire); err != nil {
					t.Fatal(err)
				}
				if wire.MFA || wire.Tokens.Refresh == "" {
					t.Fatal("completion did not deliver full pair")
				}
				full = secret.New(wire.Tokens.Access)
			}
			if full.IsZero() || full == pending {
				t.Fatal("completion did not rotate secret")
			}
			if err := kernel.require(t, full); err != nil {
				t.Fatal("full HTTP credential rejected", err)
			}
			if err := kernel.require(t, pending); !errors.Is(err, auth.Unauthenticated) {
				t.Fatal("old challenge survived", err)
			}
			replay := mfaRequest(t, router, path, body, cookie, origin)
			if replay.Code != 401 || len(replay.Result().Cookies()) != 0 || strings.Contains(replay.Body.String(), full.Reveal()) {
				t.Fatal("replayed challenge disclosed credential", replay.Code)
			}
		})
	}
}

type mfaDeliveryClock func() time.Time

func (c mfaDeliveryClock) Now() time.Time { return c() }
func mfaDeliveryEndpoint[R any](response foundryhttp.Response[R]) foundryhttp.Endpoint[foundryhttp.NoPath, foundryhttp.NoQuery, foundryhttp.NoBody, R] {
	return foundryhttp.DefineEndpoint(foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "delivery", Method: foundryhttp.POST, Access: foundryhttp.Public}, foundryhttp.StaticPath("/deliver")), foundryhttp.EmptyQuery(), foundryhttp.EmptyBody(), response)
}
func TestPostgresMFASecretResponsesNeverDiscloseOnFailures(t *testing.T) {
	s := mfaSetup(t)
	enrollment, codes := s.enroll(t)
	for _, result := range []any{enrollment, codes} {
		encoded, err := json.Marshal(result)
		text := string(encoded) + fmt.Sprintf("%#v", result)
		if err != nil || strings.Contains(text, enrollment.Secret().Secret().Reveal()) || strings.Contains(text, codes.Codes()[0].Secret().Reveal()) {
			t.Fatal("ordinary result serialization disclosed secrets", err)
		}
	}
	for _, mode := range []string{"zero", "expired", "future", "limit", "error", "cancel", "clock-panic", "clock-goexit", "plaintext"} {
		t.Run(mode, func(t *testing.T) {
			var source clock.Clock = s.clock
			if mode == "future" {
				source = mfaDeliveryClock(func() time.Time { return enrollment.CreatedAt().UTC().Add(-time.Second) })
			}
			if mode == "expired" {
				source = mfaDeliveryClock(func() time.Time { return enrollment.ExpiresAt().UTC() })
			}
			if mode == "clock-panic" {
				source = mfaDeliveryClock(func() time.Time { panic("private delivery detail") })
			}
			if mode == "clock-goexit" {
				source = mfaDeliveryClock(func() time.Time { runtime.Goexit(); return time.Time{} })
			}
			endpoint := mfaDeliveryEndpoint(foundryhttp.MFAEnrollmentResponse[multifactor.Account](200, source))
			if mode == "limit" {
				limits := foundryhttp.DefaultEndpointLimits()
				limits.Response.Bytes = 8
				endpoint = endpoint.WithLimits(limits)
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			calls := 0
			registration := endpoint.Handle(func(context.Context, foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, foundryhttp.NoBody]) (mfa.Enrollment[multifactor.Account], error) {
				calls++
				if mode == "zero" {
					return mfa.Enrollment[multifactor.Account]{}, nil
				}
				if mode == "error" {
					return enrollment, errors.New("private delivery failure")
				}
				if mode == "cancel" {
					cancel()
				}
				return enrollment, nil
			})
			router, err := foundryhttp.NewRouter(registration)
			if err != nil {
				t.Fatal(err)
			}
			scheme := "https"
			if mode == "plaintext" {
				scheme = "http"
			}
			request := httptest.NewRequest("POST", scheme+"://app.test/deliver", nil).WithContext(ctx)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, request)
			if w.Code < 400 || strings.Contains(w.Body.String(), enrollment.Secret().Secret().Reveal()) || w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("failed response disclosed enrollment", w.Code)
			}
			if mode == "plaintext" && calls != 0 {
				t.Fatal("plaintext reached credential handler")
			}
		})
	}
	for _, mode := range []string{"zero", "limit", "error"} {
		endpoint := mfaDeliveryEndpoint(foundryhttp.MFARecoveryResponse[multifactor.Account](200))
		if mode == "limit" {
			limits := foundryhttp.DefaultEndpointLimits()
			limits.Response.Bytes = 8
			endpoint = endpoint.WithLimits(limits)
		}
		router, err := foundryhttp.NewRouter(endpoint.Handle(func(context.Context, foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, foundryhttp.NoBody]) (mfa.RecoveryCodes[multifactor.Account], error) {
			if mode == "zero" {
				return mfa.RecoveryCodes[multifactor.Account]{}, nil
			}
			if mode == "error" {
				return codes, errors.New("delivery failed")
			}
			return codes, nil
		}))
		if err != nil {
			t.Fatal(err)
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest("POST", "https://app.test/deliver", nil))
		if w.Code < 400 || strings.Contains(w.Body.String(), codes.Codes()[0].Secret().Reveal()) {
			t.Fatal("failed response disclosed recovery codes", w.Code)
		}
	}
}

func TestPostgresMFAHTTPWithholdsCompletedCookieAfterResponseFailure(t *testing.T) {
	for _, mode := range []string{"handler-error", "expires-before-publish"} {
		t.Run(mode, func(t *testing.T) {
			s := mfaSetup(t)
			_, codes := s.enroll(t)
			kernel := s.kernel(t, "session")
			pending := pendingCredential(t, kernel)
			browser := s.browser(t)
			endpoint := foundryhttp.DefineEndpoint(foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "mfa.fail", Method: foundryhttp.POST, Access: foundryhttp.Public}, foundryhttp.StaticPath("/mfa/fail")), foundryhttp.EmptyQuery(), foundryhttp.EmptyBody(), foundryhttp.EmptyResponse(204)).WithMiddleware(browser.Middleware())
			factor := verifier(t, s, codes.Codes()[0])
			router, err := foundryhttp.NewRouter(endpoint.Handle(func(ctx context.Context, _ foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, foundryhttp.NoBody]) (foundryhttp.NoContent, error) {
				_, err := browser.CompleteMFA(ctx, factor, session.IssueOptions{})
				if err != nil {
					return foundryhttp.NoContent{}, err
				}
				if mode == "handler-error" {
					return foundryhttp.NoContent{}, errors.New("private response failure")
				}
				s.clock.Advance(48 * time.Hour)
				return foundryhttp.NoContent{}, nil
			}))
			if err != nil {
				t.Fatal(err)
			}
			request := httptest.NewRequest("POST", "https://app.test/mfa/fail", nil)
			request.Header.Set("Origin", "https://app.test")
			request.AddCookie(&http.Cookie{Name: "__Host-foundry_session", Value: pending.Reveal()})
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code < 400 || len(response.Result().Cookies()) != 0 {
				t.Fatal("failed response published cookie", response.Code)
			}
			if err := kernel.require(t, pending); !errors.Is(err, auth.Unauthenticated) {
				t.Fatal("HTTP error resurrected consumed credential", err)
			}
		})
	}
}
