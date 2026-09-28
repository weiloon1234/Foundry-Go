package recovering_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"foundry.test/consumer/recovering"
	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/auth/challenge"
	"github.com/weiloon1234/Foundry-Go/auth/passwordreset"
	"github.com/weiloon1234/Foundry-Go/contract"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
)

func recoveryHTTP(handler http.Handler, method, scheme, path, body, origin string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, scheme+"://app.test"+path, strings.NewReader(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	if origin != "" {
		request.Header.Set("Origin", origin)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}
func recoveryRouter(t *testing.T, s *fixture) *foundryhttp.Router {
	t.Helper()
	router, err := recovering.CompletionRoutes(s.reset, s.verification)
	if err != nil {
		t.Fatal(err)
	}
	return router
}
func TestPostgresRecoveryHTTPCommitsModelAndCredentialInvalidation(t *testing.T) {
	s := prepare(t)
	credentials := attachCredentials(t, s)
	web, api := issueCredentials(t, credentials, passwordProof(t, s, "old long password"))
	reset := issue(t, s)
	verify, err := s.verification.Issue(t.Context(), s.member.FoundryReference())
	if err != nil {
		t.Fatal(err)
	}
	router := recoveryRouter(t, s)
	body := `{"token":"` + reset.Token().Secret().Reveal() + `","password":"next long password"}`
	result := recoveryHTTP(router, "POST", "https", "/recovery/reset", body, "https://app.test")
	if result.Code != 204 || result.Body.Len() != 0 || len(result.Result().Cookies()) != 0 || result.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("reset response", result.Code)
	}
	assertRevoked(t, credentials, web, api)
	if valid, err := s.hasher.Check(t.Context(), plain(t, "next long password"), current(t, s).Password); err != nil || !valid {
		t.Fatal("new HTTP password not stored", err)
	}
	retry := recoveryHTTP(router, "POST", "https", "/recovery/reset", body, "https://app.test")
	if retry.Code != 401 {
		t.Fatal("reset reused", retry.Code)
	}
	verification := `{"token":"` + verify.Token().Secret().Reveal() + `"}`
	result = recoveryHTTP(router, "POST", "https", "/recovery/verify", verification, "https://app.test")
	if result.Code != 204 || !current(t, s).EmailVerified || len(result.Result().Cookies()) != 0 {
		t.Fatal("verification did not preserve no-login semantics", result.Code)
	}
	if duplicate := recoveryHTTP(router, "POST", "https", "/recovery/verify", verification, "https://app.test"); duplicate.Code != 401 {
		t.Fatal("verification reused", duplicate.Code)
	}
}
func TestPostgresRecoveryHTTPRejectedRequestsLeaveChallengeAvailable(t *testing.T) {
	s := prepare(t)
	reset := issue(t, s)
	router := recoveryRouter(t, s)
	raw := reset.Token().Secret().Reveal()
	valid := `{"token":"` + raw + `","password":"next long password"}`
	for _, test := range []struct {
		name, method, scheme, path, body, origin string
		status                                   int
	}{
		{name: "GET prefetch", method: "GET", scheme: "https", path: "/recovery/reset", status: 405},
		{name: "plaintext", method: "POST", scheme: "http", path: "/recovery/reset", body: valid, origin: "http://app.test", status: 400},
		{name: "cross origin", method: "POST", scheme: "https", path: "/recovery/reset", body: valid, origin: "https://foreign.test", status: 403},
		{name: "missing origin", method: "POST", scheme: "https", path: "/recovery/reset", body: valid, status: 403},
		{name: "token query", method: "POST", scheme: "https", path: "/recovery/reset?token=" + raw, body: valid, origin: "https://app.test", status: 400},
		{name: "null", method: "POST", scheme: "https", path: "/recovery/reset", body: `{"token":null,"password":"next long password"}`, origin: "https://app.test", status: 400},
		{name: "unknown", method: "POST", scheme: "https", path: "/recovery/reset", body: strings.TrimSuffix(valid, "}") + `,"extra":1}`, origin: "https://app.test", status: 400},
		{name: "duplicate", method: "POST", scheme: "https", path: "/recovery/reset", body: strings.TrimSuffix(valid, "}") + `,"token":"` + raw + `"}`, origin: "https://app.test", status: 400},
		{name: "wrong purpose", method: "POST", scheme: "https", path: "/recovery/verify", body: `{"token":"` + raw + `"}`, origin: "https://app.test", status: 401},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := recoveryHTTP(router, test.method, test.scheme, test.path, test.body, test.origin)
			if response.Code != test.status || strings.Contains(response.Body.String(), raw) || strings.Contains(response.Body.String(), "next long password") {
				t.Fatal("unsafe recovery request", response.Code)
			}
			if current(t, s).Password != s.member.Password || s.invalidations.Load() != 0 {
				t.Fatal("rejected request mutated model")
			}
		})
	}
	if _, err := s.reset.Complete(t.Context(), reset.Token(), plain(t, "next long password")); err != nil {
		t.Fatal("rejection consumed challenge", err)
	}
}
func TestPostgresRecoveryHTTPExpiredAndDisabledResponsesAgree(t *testing.T) {
	for _, mode := range []string{"expired", "disabled", "absent"} {
		t.Run(mode, func(t *testing.T) {
			s := prepare(t)
			reset := issue(t, s)
			if mode == "expired" {
				s.clock.Advance(passwordreset.DefaultLifetime)
			}
			if mode == "disabled" {
				update(t, s, recovering.MemberDraft{}.SetEnabled(false))
			}
			raw := reset.Token().Secret().Reveal()
			if mode == "absent" {
				raw = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
			}
			response := recoveryHTTP(recoveryRouter(t, s), "POST", "https", "/recovery/reset", `{"token":"`+raw+`","password":"next long password"}`, "https://app.test")
			if response.Code != 401 || strings.Contains(response.Body.String(), raw) {
				t.Fatal("invalid challenge response", response.Code)
			}
			if current(t, s).Password != s.member.Password {
				t.Fatal("invalid challenge changed password")
			}
		})
	}
}
func TestRecoveryGeneratedContractsRetainPurposeAndNativeRedaction(t *testing.T) {
	limits := foundryhttp.DefaultEndpointLimits().Response
	const raw = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	input, err := recovering.ResetRequestJSON().Decode(t.Context(), []byte(`{"token":"`+raw+`","password":"next long password"}`), limits)
	if err != nil {
		t.Fatal(err)
	}
	var typed passwordreset.Token[recovering.Member] = input.Token
	if typed.Secret().Reveal() != raw {
		t.Fatal("decoded token changed")
	}
	encoded, err := json.Marshal(input)
	if err != nil || strings.Contains(string(encoded), raw) || strings.Contains(string(encoded), "next long password") {
		t.Fatal("request disclosed secrets", err)
	}
	reset, err := recovering.ResetRequestJSON().Description()
	if err != nil {
		t.Fatal(err)
	}
	verify, err := recovering.VerificationRequestJSON().Description()
	if err != nil {
		t.Fatal(err)
	}
	tokenType := func(schema contract.Schema) contract.TypeID {
		for _, node := range schema.Types {
			if node.ID == schema.Root {
				for _, p := range node.Properties {
					if p.Name == "token" {
						return p.Type
					}
				}
			}
		}
		return ""
	}
	if tokenType(reset) == "" || tokenType(reset) == tokenType(verify) {
		t.Fatal("wire metadata lost distinct purpose")
	}
	var zero challenge.Token[recovering.Member, challenge.PasswordReset]
	if !errors.Is(zero.Validate(), auth.Unauthenticated) {
		t.Fatal("zero token gained authority")
	}
}
