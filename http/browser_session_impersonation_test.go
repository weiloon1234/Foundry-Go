package http_test

import (
	"context"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/auth/session"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
)

// An administrator impersonates an account through the browser cookie, cannot
// reach refused sensitive routes while impersonating, and stopping restores a
// fresh administrator cookie while every intermediate cookie stops working.
func TestBrowserImpersonationSwapsAndRestoresTheSessionCookie(t *testing.T) {
	s := browserPrepare(t)
	impersonation, err := session.NewImpersonation(s.sessions, s.sessions, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	proof := browserProof(t)
	login := browserEndpoint("/login", foundryhttp.POST, foundryhttp.Public).WithMiddleware(s.web.Middleware()).Handle(func(ctx context.Context, _ authInput) (foundryhttp.NoContent, error) {
		_, err := s.web.Login(ctx, proof, session.IssueOptions{})
		return foundryhttp.NoContent{}, err
	})
	guarded := func(path string) foundryhttp.AuthenticatedEndpoint[foundryhttp.NoPath, foundryhttp.NoQuery, foundryhttp.NoBody, authAccount, foundryhttp.NoContent] {
		return foundryhttp.RequireAuthentication(browserEndpoint(path, foundryhttp.POST, foundryhttp.Guarded), s.web.Authentication(), s.web.Guard())
	}
	var seen int64
	start := guarded("/impersonate").Handle(func(ctx context.Context, _ authAccount, _ authInput) (foundryhttp.NoContent, error) {
		_, err := s.web.Impersonate(ctx, impersonation, authAccount{ID: 9}.reference(), 30*time.Minute)
		return foundryhttp.NoContent{}, err
	})
	stop := guarded("/stop").Handle(func(ctx context.Context, _ authAccount, _ authInput) (foundryhttp.NoContent, error) {
		_, err := s.web.StopImpersonating(ctx, impersonation)
		return foundryhttp.NoContent{}, err
	})
	whoami := guarded("/whoami").Handle(func(_ context.Context, account authAccount, _ authInput) (foundryhttp.NoContent, error) {
		seen = account.ID
		return foundryhttp.NoContent{}, nil
	})
	password := guarded("/password").WithActorMiddleware(s.web.RefuseImpersonation()).Handle(func(context.Context, authAccount, authInput) (foundryhttp.NoContent, error) {
		return foundryhttp.NoContent{}, nil
	})
	router, err := foundryhttp.NewRouter(login, start, stop, whoami, password)
	if err != nil {
		t.Fatal(err)
	}
	admin := issuedCookie(t, browserServe(router, "POST", "/login", nil))
	impersonating := issuedCookie(t, browserServe(router, "POST", "/impersonate", admin))
	if w := browserServe(router, "POST", "/whoami", impersonating); w.Code != 204 || seen != 9 {
		t.Fatal("impersonation cookie does not act as the target", w.Code, seen)
	}
	if impersonating.MaxAge != 0 || !impersonating.Expires.IsZero() {
		t.Fatal("impersonation cookie was persisted beyond the browser session")
	}
	if w := browserServe(router, "POST", "/password", impersonating); w.Code != 403 {
		t.Fatal("sensitive route accepted an impersonation session", w.Code)
	}
	if w := browserServe(router, "POST", "/impersonate", impersonating); w.Code != 403 {
		t.Fatal("nested impersonation accepted", w.Code)
	}
	restored := issuedCookie(t, browserServe(router, "POST", "/stop", impersonating))
	if w := browserServe(router, "POST", "/whoami", restored); w.Code != 204 || seen != 7 {
		t.Fatal("stopping did not restore the administrator", w.Code, seen)
	}
	if w := browserServe(router, "POST", "/password", restored); w.Code != 204 {
		t.Fatal("restored administrator was refused", w.Code)
	}
	for _, ended := range []string{"impersonating", "admin"} {
		cookie := impersonating
		if ended == "admin" {
			cookie = admin
		}
		if w := browserServe(router, "POST", "/whoami", cookie); w.Code != 401 {
			t.Fatal("ended cookie still authenticates:", ended, w.Code)
		}
	}
}
