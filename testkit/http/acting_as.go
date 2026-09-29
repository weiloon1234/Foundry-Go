package http

import (
	stdhttp "net/http"
	"testing"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/auth/session"
	"github.com/weiloon1234/Foundry-Go/auth/token"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/secret"
)

// ActingAsToken issues a real access token for proof through the application's
// configured token binding, then returns an independent client that sends it as
// a bearer credential. Every request still runs token verification, provider
// eligibility, scopes and policies; the helper grants nothing by itself. Build
// proof with auth.NewProof for the actor and assurance the test represents.
func ActingAsToken[M model.Identifiable, K any](t testing.TB, client *Client, tokens *token.Tokens[M, K], proof auth.Proof[M, K], options token.IssueOptions[M]) *Client {
	t.Helper()
	issued, err := tokens.Issue(t.Context(), proof, options)
	if err != nil {
		t.Fatalf("issue test access token: %v", err)
	}
	// Replace, never add: a second Authorization header is rejected as ambiguous.
	headers := make(stdhttp.Header)
	headers.Set("Authorization", "Bearer "+issued.AccessSecret().Reveal())
	return client.With(t, WithHeaders(headers))
}

// ActingAsSession issues a real session through the configured session binding
// and returns an independent client sending it in the application's declared
// credential cookie. Cookie-authenticated unsafe requests still require the
// application's origin/CSRF policy; the helper does not bypass it.
func ActingAsSession[M model.Identifiable, K any](t testing.TB, client *Client, sessions *session.Sessions[M, K], proof auth.Proof[M, K], options session.IssueOptions, cookie foundryhttp.Cookie[secret.String]) *Client {
	t.Helper()
	if err := cookie.Validate(); err != nil {
		t.Fatalf("session credential cookie: %v", err)
	}
	issued, err := sessions.Issue(t.Context(), proof, options)
	if err != nil {
		t.Fatalf("issue test session: %v", err)
	}
	credential := &stdhttp.Cookie{Name: string(cookie.Name()), Value: issued.Secret().Reveal()}
	if credential.String() == "" {
		t.Fatal("issued session cannot be sent as its declared cookie")
	}
	// Replace an inherited session cookie of the same name; keep other cookies.
	return client.With(t, withCookie(credential))
}
