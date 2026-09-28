// Package authfixture owns the shared loopback identity used by HTTP fixtures.
package authfixture

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/secret"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Actor deliberately remains a model identity outside public wire DTOs.
type Actor struct {
	ID     int64
	Tenant string
}

func (a Actor) reference() model.Reference[Actor, int64] {
	return model.NewReference[Actor]("idempotent_actors", a.ID, codec.Signed[int64]())
}
func (a Actor) FoundryIdentity() (model.Identity, error) { return a.reference().Identity() }

// Authentication uses fixed loopback test credentials, never deployment secrets.
func Authentication() (*http.Authentication, auth.Guard[Actor], error) {
	provider := auth.DefineProvider("idempotent_actors", (Actor{}).reference(), func(_ context.Context, id int64) (value.Optional[Actor], error) {
		tenant := "tenant-a"
		if id == 3 {
			tenant = "tenant-b"
		}
		return value.Set(Actor{id, tenant}), nil
	}, func(context.Context, Actor) (bool, error) { return true, nil })
	strategy := auth.DefineStrategy("bearer", func(_ context.Context, token secret.String) (value.Optional[auth.Proof[Actor, int64]], error) {
		id := int64(0)
		switch token.Reveal() {
		case "alice":
			id = 1
		case "bob":
			id = 2
		case "tenant":
			id = 3
		default:
			return value.Optional[auth.Proof[Actor, int64]]{}, nil
		}
		proof, err := auth.NewProof((Actor{ID: id}).reference(), auth.Authenticated)
		return value.Set(proof), err
	})
	guard := auth.DefineGuard("idempotent", provider, strategy)
	registry, err := auth.NewRegistry(auth.DefaultConfig(), guard.Registration())
	if err != nil {
		return nil, guard, err
	}
	transport, err := http.NewAuthentication(registry, http.BearerCredential("bearer"))
	return transport, guard, err
}
