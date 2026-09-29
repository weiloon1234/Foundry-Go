package http_test

import (
	"context"
	stdhttp "net/http"
	"strings"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/auth/session"
	"github.com/weiloon1234/Foundry-Go/auth/token"
	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/httpclient"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/secret"
	httptest "github.com/weiloon1234/Foundry-Go/testkit/http"
	"github.com/weiloon1234/Foundry-Go/value"
)

// recorder captures assertion failures so negative cases can be checked.
type recorder struct {
	testing.TB
	failures []string
}

func (r *recorder) Helper() {}
func (r *recorder) Errorf(format string, args ...any) {
	r.failures = append(r.failures, format)
}

func TestClientTimeoutIsConfigurable(t *testing.T) {
	slow := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		select {
		case <-time.After(300 * time.Millisecond):
			w.WriteHeader(204)
		case <-r.Context().Done():
		}
	})
	short := httptest.New(t, slow, httptest.WithTimeout(50*time.Millisecond))
	if _, err := short.Do(t.Context(), short.Get("/")); err == nil {
		t.Fatal("short test timeout did not bound the request")
	}
	patient := short.With(t, httptest.WithTimeout(5*time.Second))
	response, err := patient.Do(t.Context(), patient.Get("/"))
	if err != nil {
		t.Fatal(err)
	}
	httptest.AssertNoContent(t, response)
}

func TestResponseAssertions(t *testing.T) {
	issues := foundryhttp.ErrorResponse{Status: 422, Code: foundryhttp.ValidationFailed, Message: "Invalid", Issues: []contract.Issue{{Path: "/email", Code: "required"}, {Path: "/items/0/name", Code: "required"}}}
	envelope, err := foundryhttp.ErrorResponseJSON().Encode(t.Context(), issues, foundryhttp.DefaultEndpointLimits().Response)
	if err != nil {
		t.Fatal(err)
	}
	client := httptest.New(t, stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		switch r.URL.Path {
		case "/json":
			w.Header().Set("Content-Type", "application/json")
			w.Header().Add("Set-Cookie", "theme=dark; Path=/; HttpOnly")
			_, _ = w.Write([]byte(`{"data":{"items":[{"name":"one","count":2}],"a/b":{"~":true}}}`))
		case "/invalid":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(422)
			_, _ = w.Write(envelope)
		case "/redirect":
			stdhttp.Redirect(w, r, "/next", stdhttp.StatusFound)
		default:
			w.WriteHeader(204)
		}
	}))
	get := func(path string) httpclient.Response {
		response, err := client.Do(t.Context(), client.Get(path))
		if err != nil {
			t.Fatal(err)
		}
		return response
	}
	document := get("/json")
	httptest.AssertJSONPath(t, document, "/data/items/0/name", "one")
	httptest.AssertJSONPath(t, document, "/data/items/0", struct {
		Count int    `json:"count"`
		Name  string `json:"name"`
	}{2, "one"})
	httptest.AssertJSONPath(t, document, "/data/a~1b/~0", true)
	httptest.AssertHeader(t, document, "Content-Type", "application/json")
	if cookie := httptest.AssertCookie(t, document, "theme"); cookie == nil || !cookie.HttpOnly || cookie.Value != "dark" {
		t.Fatal("cookie attributes were not returned")
	}
	httptest.AssertValidationErrors(t, get("/invalid"), "email", "/items/0/name")
	httptest.AssertRedirect(t, get("/redirect"), "/next")
	httptest.AssertNoContent(t, get("/empty"))

	failed := &recorder{TB: t}
	httptest.AssertJSONPath(failed, document, "/data/items/0/name", "two")
	httptest.AssertJSONPath(failed, document, "/data/items/1", "absent")
	httptest.AssertValidationErrors(failed, get("/invalid"), "/password")
	httptest.AssertValidationErrors(failed, document, "/email")
	httptest.AssertRedirect(failed, document, "/next")
	httptest.AssertNoContent(failed, document)
	if failed.AssertCookie(document) {
		t.Fatal("absent cookie accepted")
	}
	if len(failed.failures) != 8 {
		t.Fatalf("negative assertions reported %d failure(s): %v", len(failed.failures), failed.failures)
	}
}

func (r *recorder) AssertCookie(response httpclient.Response) bool {
	return httptest.AssertCookie(r, response, "missing") != nil
}

type member struct{ ID int64 }

func (m member) reference() model.Reference[member, int64] {
	return model.NewReference[member]("testkit_http_members", m.ID, codec.Signed[int64]())
}
func (m member) FoundryIdentity() (model.Identity, error) { return m.reference().Identity() }

type tokenBackend struct {
	token.Backend
	saved token.Record
}

func (b *tokenBackend) Create(_ context.Context, address token.Address, creation token.Creation) (token.Record, error) {
	record, err := creation.At(address, time.Now())
	b.saved = record
	return record, err
}

func TestActingAsTokenSendsARealIssuedCredential(t *testing.T) {
	backend := &tokenBackend{}
	provider := auth.DefineProvider("members", member{}.reference(), func(_ context.Context, id int64) (value.Optional[member], error) { return value.Set(member{id}), nil }, func(context.Context, member) (bool, error) { return true, nil })
	store, err := token.NewStore(backend, token.DefaultConfig(keyspace.Namespace{Application: "testkit", Environment: "test"}))
	if err != nil {
		t.Fatal(err)
	}
	allowed, err := auth.NewAccessScopes(auth.DefineAccessScope[member]("orders.read"))
	if err != nil {
		t.Fatal(err)
	}
	tokens, err := token.New(store, "api", provider, "bearer", allowed)
	if err != nil {
		t.Fatal(err)
	}
	proof, err := auth.NewProof(member{7}.reference(), auth.Authenticated)
	if err != nil {
		t.Fatal(err)
	}
	client := httptest.New(t, stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		// Like BearerCredential, reject an ambiguous repeated credential.
		values := r.Header.Values("Authorization")
		if len(values) != 1 {
			w.WriteHeader(401)
			return
		}
		raw, found := strings.CutPrefix(values[0], "Bearer ")
		digest, err := token.HashSecret(secret.New(raw))
		if !found || err != nil || !backend.saved.AccessHash.Equal(digest) {
			w.WriteHeader(401)
			return
		}
		w.WriteHeader(204)
	}))
	anonymous, err := client.Do(t.Context(), client.Get("/"))
	if err != nil {
		t.Fatal(err)
	}
	httptest.AssertStatus(t, anonymous, 401)
	acting := httptest.ActingAsToken(t, client, tokens, proof, token.IssueOptions[member]{Name: "test"})
	response, err := acting.Do(t.Context(), acting.Get("/"))
	if err != nil {
		t.Fatal(err)
	}
	httptest.AssertNoContent(t, response)
	if unchanged, err := client.Do(t.Context(), client.Get("/")); err != nil || unchanged.Status() != 401 {
		t.Fatal("acting client changed the original client", err)
	}
	// Re-authenticating an authenticated client replaces its credential.
	stale := client.With(t, httptest.WithHeaders(stdhttp.Header{"Authorization": {"Bearer stale"}}))
	for _, base := range []*httptest.Client{acting, stale} {
		again := httptest.ActingAsToken(t, base, tokens, proof, token.IssueOptions[member]{Name: "again"})
		response, err := again.Do(t.Context(), again.Get("/"))
		if err != nil {
			t.Fatal(err)
		}
		httptest.AssertNoContent(t, response)
	}
}

type sessionBackend struct {
	session.Backend
	saved session.Record
}

func (b *sessionBackend) Create(_ context.Context, address session.Address, creation session.Creation) (session.Record, error) {
	record, err := creation.At(address, time.Now())
	b.saved = record
	return record, err
}

func TestActingAsSessionReplacesOnlyTheSessionCookie(t *testing.T) {
	backend := &sessionBackend{}
	provider := auth.DefineProvider("members", member{}.reference(), func(_ context.Context, id int64) (value.Optional[member], error) { return value.Set(member{id}), nil }, func(context.Context, member) (bool, error) { return true, nil })
	store, err := session.NewStore(backend, session.DefaultConfig(keyspace.Namespace{Application: "testkit", Environment: "test"}))
	if err != nil {
		t.Fatal(err)
	}
	sessions, err := session.New(store, "web", provider, "session_cookie")
	if err != nil {
		t.Fatal(err)
	}
	proof, err := auth.NewProof(member{7}.reference(), auth.Authenticated)
	if err != nil {
		t.Fatal(err)
	}
	cookie := foundryhttp.DefineCookie("app_session", foundryhttp.SecretCookie(), foundryhttp.DefaultCookieOptions())
	client := httptest.New(t, stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		// One Cookie header, one session pair and the unrelated cookie kept.
		sent, sessionErr := r.Cookie("app_session")
		theme, themeErr := r.Cookie("theme")
		if len(r.Header.Values("Cookie")) != 1 || len(r.CookiesNamed("app_session")) != 1 || sessionErr != nil || themeErr != nil || theme.Value != "dark" {
			w.WriteHeader(400)
			return
		}
		digest, err := session.HashSecret(secret.New(sent.Value))
		if err != nil || !backend.saved.Hash.Equal(digest) {
			w.WriteHeader(401)
			return
		}
		w.WriteHeader(204)
	}))
	signed := client.With(t, httptest.WithHeaders(stdhttp.Header{"Cookie": {"theme=dark; app_session=stale"}}))
	acting := httptest.ActingAsSession(t, signed, sessions, proof, session.IssueOptions{}, cookie)
	again := httptest.ActingAsSession(t, acting, sessions, proof, session.IssueOptions{}, cookie)
	response, err := again.Do(t.Context(), again.Get("/"))
	if err != nil {
		t.Fatal(err)
	}
	httptest.AssertNoContent(t, response)
}
