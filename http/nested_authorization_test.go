package http_test

import (
	"context"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/http/modelbinding"
	"github.com/weiloon1234/Foundry-Go/value"
	"sync/atomic"
	"testing"
	"time"
)

func TestSignedTypedResourceAuthorization(t *testing.T) {
	var loads, parses, lookups atomic.Int32
	registry, guard, _ := authSetup(t, "bearer", &loads)
	transport, err := foundryhttp.NewAuthentication(registry, foundryhttp.BearerCredential("bearer"))
	if err != nil {
		t.Fatal(err)
	}
	signer, now := compositionSigner(t)
	base := foundryhttp.DefineEndpoint(compositionRoute(foundryhttp.Guarded, &parses), foundryhttp.EmptyQuery(), foundryhttp.EmptyBody(), foundryhttp.EmptyResponse(204))
	signed := foundryhttp.RequireAuthentication(base, transport, guard).WithAuthorization(func(_ context.Context, _ authAccount, in foundryhttp.Input[compositionPath, foundryhttp.NoQuery, foundryhttp.NoBody]) error {
		if in.Path.Order == 13 {
			return foundryhttp.Forbidden
		}
		return nil
	}).Signed(signer)
	resolver := modelbinding.Define(func(_ context.Context, p compositionPath) (value.Optional[compositionOrder], error) {
		lookups.Add(1)
		return value.Set(compositionOrder{ID: p.Order, Owner: 1}), nil
	})
	bound := modelbinding.BindAuthenticated(signed, resolver).WithAuthorization(func(_ context.Context, actor authAccount, in compositionInput) error {
		if actor.ID != in.Model.Owner {
			return foundryhttp.Forbidden
		}
		return nil
	})
	if bound.WithAuthorization(nil).Validate() == nil {
		t.Fatal("nil typed resource authorization accepted")
	}
	handled := 0
	handler, _ := compositionHandler(t, bound.Handle(func(context.Context, authAccount, compositionInput) (foundryhttp.NoContent, error) {
		handled++
		return foundryhttp.NoContent{}, nil
	}))
	for _, tc := range []struct {
		id                       int64
		token                    string
		status, lookups, handled int
	}{{7, "valid", 204, 1, 1}, {7, "other", 403, 1, 0}, {13, "valid", 403, 0, 0}, {7, "invalid", 401, 0, 0}} {
		url, err := signed.URL(t.Context(), compositionOrigin, compositionPath{Order: tc.id}, foundryhttp.NoQuery{}, now.Now().Add(time.Minute))
		if err != nil {
			t.Fatal(err)
		}
		before, count := lookups.Load(), handled
		response := compositionServe(handler, url, tc.token)
		if response.Code != tc.status || lookups.Load()-before != int32(tc.lookups) || handled-count != tc.handled {
			t.Fatal("signed policy ordering changed", response.Code)
		}
	}
}
