package http_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/clock"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
)

func TestMFARequestContractsRejectInvalidInputsBeforeHandlers(t *testing.T) {
	const pending = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	for _, kind := range []string{"totp", "recovery"} {
		t.Run(kind, func(t *testing.T) {
			calls := 0
			route := foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "mfa.input", Method: foundryhttp.POST, Access: foundryhttp.Public}, foundryhttp.StaticPath("/mfa"))
			var registration foundryhttp.RouteRegistration
			check := func(input any, challenge, code string) error {
				calls++
				if challenge != pending || code == "" {
					t.Error("typed input lost")
				}
				encoded, err := json.Marshal(input)
				if err != nil || strings.Contains(string(encoded), pending) || strings.Contains(fmt.Sprintf("%#v", input), pending) {
					t.Error("input disclosed challenge", err)
				}
				return nil
			}
			code := "001234"
			if kind == "totp" {
				registration = foundryhttp.DefineEndpoint(route, foundryhttp.EmptyQuery(), foundryhttp.MFATOTPBody(), foundryhttp.EmptyResponse(204)).Handle(func(_ context.Context, input foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, foundryhttp.MFATOTPRequest]) (foundryhttp.NoContent, error) {
					return foundryhttp.NoContent{}, check(input.Body, input.Body.Challenge.Secret().Reveal(), input.Body.Code.Secret().Reveal())
				})
			} else {
				code = "BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBA"
				registration = foundryhttp.DefineEndpoint(route, foundryhttp.EmptyQuery(), foundryhttp.MFARecoveryBody(), foundryhttp.EmptyResponse(204)).Handle(func(_ context.Context, input foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, foundryhttp.MFARecoveryRequest]) (foundryhttp.NoContent, error) {
					return foundryhttp.NoContent{}, check(input.Body, input.Body.Challenge.Secret().Reveal(), input.Body.Code.Secret().Reveal())
				})
			}
			router := newAuthRouter(t, registration)
			valid := `{"challenge":"` + pending + `","code":"` + code + `"}`
			for _, body := range []string{valid, `{}`, `{"challenge":null,"code":"` + code + `"}`, `{"challenge":"` + pending + `"}`, `{"challenge":"bad","code":"` + code + `"}`, `{"challenge":"` + pending + `","code":null}`, `{"challenge":"` + pending + `","code":"bad"}`, `{"challenge":"` + pending + `","code":"` + code + `","code":"` + code + `"}`, `{"challenge":"` + pending + `","code":"` + code + `","extra":true}`} {
				request := httptest.NewRequest("POST", "https://app.test/mfa", strings.NewReader(body))
				request.Header.Set("Content-Type", "application/json")
				w := httptest.NewRecorder()
				router.ServeHTTP(w, request)
				want := 400
				if body == valid {
					want = 204
				}
				if w.Code != want {
					t.Fatal("unexpected MFA input status", w.Code, want)
				}
				if strings.Contains(w.Body.String(), pending) {
					t.Fatal("input failure disclosed credential")
				}
			}
			if calls != 1 {
				t.Fatal("invalid input reached handler", calls)
			}
			var value foundryhttp.MFAChallengeCredential
			if err := json.Unmarshal([]byte(`"`+pending+`"`), &value); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal([]byte(`null`), &value); err == nil || !value.Secret().IsZero() {
				t.Fatal("invalid input retained credential")
			}
		})
	}
}

func TestMFAResponseDescriptorsRequireSecurePostContract(t *testing.T) {
	for _, method := range []foundryhttp.Method{foundryhttp.GET, foundryhttp.HEAD, foundryhttp.PUT, foundryhttp.DELETE} {
		route := foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "mfa.disclosure", Method: method, Access: foundryhttp.Public}, foundryhttp.StaticPath("/mfa"))
		enrollment := foundryhttp.DefineEndpoint(route, foundryhttp.EmptyQuery(), foundryhttp.EmptyBody(), foundryhttp.MFAEnrollmentResponse[authAccount](200, clock.System{}))
		recovery := foundryhttp.DefineEndpoint(route, foundryhttp.EmptyQuery(), foundryhttp.EmptyBody(), foundryhttp.MFARecoveryResponse[authAccount](200))
		if enrollment.Validate() == nil || recovery.Validate() == nil {
			t.Fatal("disclosure method accepted", method)
		}
	}
	if foundryhttp.MFAEnrollmentResponse[authAccount](200, nil).Validate() == nil || foundryhttp.MFARecoveryResponse[authAccount](204).Validate() == nil {
		t.Fatal("invalid MFA response configuration")
	}
}
