package http_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/auth/session"
	sessionpg "github.com/weiloon1234/Foundry-Go/auth/session/postgres"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/internal/errorgraphtest"
	"github.com/weiloon1234/Foundry-Go/model"
)

type failingResumeBackend struct {
	*sessionpg.Backend
	failure atomic.Pointer[errorgraphtest.Error]
	calls   atomic.Int32
}

func (b *failingResumeBackend) List(ctx context.Context, address session.Address, subject model.Identity, limit int) ([]session.Record, error) {
	if cause := b.failure.Load(); cause != nil {
		b.calls.Add(1)
		return nil, cause
	}
	return b.Backend.List(ctx, address, subject, limit)
}
func TestStopImpersonatingContainsBackendErrorMatching(t *testing.T) {
	for _, mode := range []string{"cycle", "panic", "goexit"} {
		t.Run(mode, func(t *testing.T) {
			var backend *failingResumeBackend
			s := browserPrepareWithBackend(t, func(raw *sessionpg.Backend) session.Backend {
				backend = &failingResumeBackend{Backend: raw}
				return backend
			})
			impersonation, err := session.NewImpersonation(s.sessions, s.sessions, time.Hour)
			if err != nil {
				t.Fatal(err)
			}
			login := browserEndpoint("/login", foundryhttp.POST, foundryhttp.Public).WithMiddleware(s.web.Middleware()).Handle(func(ctx context.Context, _ authInput) (foundryhttp.NoContent, error) {
				_, err := s.web.Login(ctx, browserProof(t), session.IssueOptions{})
				return foundryhttp.NoContent{}, err
			})
			guarded := func(path string) foundryhttp.AuthenticatedEndpoint[foundryhttp.NoPath, foundryhttp.NoQuery, foundryhttp.NoBody, authAccount, foundryhttp.NoContent] {
				return foundryhttp.RequireAuthentication(browserEndpoint(path, foundryhttp.POST, foundryhttp.Guarded), s.web.Authentication(), s.web.Guard())
			}
			start := guarded("/start").Handle(func(ctx context.Context, _ authAccount, _ authInput) (foundryhttp.NoContent, error) {
				_, err := s.web.Impersonate(ctx, impersonation, authAccount{ID: 9}.reference(), time.Minute)
				return foundryhttp.NoContent{}, err
			})
			var retained error
			stop := guarded("/stop").Handle(func(ctx context.Context, _ authAccount, _ authInput) (foundryhttp.NoContent, error) {
				_, retained = s.web.StopImpersonating(ctx, impersonation)
				return foundryhttp.NoContent{}, retained
			})
			router, err := foundryhttp.NewRouter(login, start, stop)
			if err != nil {
				t.Fatal(err)
			}
			admin := issuedCookie(t, browserServe(router, "POST", "/login", nil))
			acting := issuedCookie(t, browserServe(router, "POST", "/start", admin))
			cause := &errorgraphtest.Error{Mode: mode}
			backend.failure.Store(cause)
			response := browserServe(router, "POST", "/stop", acting)
			if response.Code != 500 || !errors.Is(retained, cause) || response.Header().Get("Set-Cookie") != "" || backend.calls.Load() != 1 || cause.Formatted.Load() != 0 || cause.Visits.Load() > 2048 {
				t.Fatal("resume error changed cookie or escaped containment", response.Code, cause.Visits.Load())
			}
		})
	}
}
