package auth_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/netip"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/weiloon1234/Foundry-Go/attribution"
	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/secret"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestGuardOriginCapturesVerifiedIdentityAndPreservesOnlyRequestMetadata(t *testing.T) {
	var loads atomic.Int32
	p := provider(func(_ context.Context, key accountKey) (value.Optional[account], error) {
		loads.Add(1)
		return value.Set(account{ID: key, Enabled: true}), nil
	})
	api := auth.DefineGuard("api", p, strategy(t, "bearer", 7))
	web := auth.DefineGuard("web", p, strategy(t, "session", 9))
	r := registry(t, api.Registration(), web.Registration())
	prior, err := (attribution.Origin{}).WithSystem("fixture.worker")
	if err != nil {
		t.Fatal(err)
	}
	metadata := attribution.Request{ID: "correlated", IP: netip.MustParseAddr("192.0.2.7"), UserAgent: "fixture"}
	prior, err = prior.WithRequest(metadata)
	if err != nil {
		t.Fatal(err)
	}
	parent, err := attribution.WithContext(t.Context(), prior)
	if err != nil {
		t.Fatal(err)
	}
	owned, err := r.NewScope(parent, inputs(t, auth.Credential{Name: "bearer", Secret: secret.New("valid")}, auth.Credential{Name: "session", Secret: secret.New("valid")}))
	if err != nil {
		t.Fatal(err)
	}
	defer owned.Close()
	ctx, err := api.WithAttribution(owned.Context())
	if err != nil {
		t.Fatal(err)
	}
	snapshot := attribution.FromContext(ctx)
	expected, _ := (account{ID: 7}).FoundryIdentity()
	identity, present := snapshot.Model()
	if !present || identity != expected || snapshot.Guard() != "api" || snapshot.System() != "" || snapshot.Request() != metadata {
		t.Fatal("incorrect verified origin")
	}
	if attribution.FromContext(parent) != prior {
		t.Fatal("parent origin mutated")
	}
	for range 3 {
		got, err := api.Origin(ctx)
		if err != nil || got != snapshot {
			t.Fatal("origin changed", err)
		}
		if _, err := api.Require(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if loads.Load() != 1 {
		t.Fatal("attribution rehydrated the model")
	}
	other, err := web.Origin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	webIdentity, _ := (account{ID: 9}).FoundryIdentity()
	got, _ := other.Model()
	if got != webIdentity || other.Guard() != "web" || loads.Load() != 2 {
		t.Fatal("guard reused inherited attribution")
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "valid") || strings.Contains(string(encoded), "bearer") {
		t.Fatal("provenance serialized credentials")
	}
	if err := owned.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := api.Origin(context.WithoutCancel(ctx)); err == nil {
		t.Fatal("closed scope captured authority")
	}
	// Restoring the serializable snapshot in a fresh execution context does not
	// restore the old request's guard cache or authority.
	var restored attribution.Origin
	if err := json.Unmarshal(encoded, &restored); err != nil || restored != snapshot {
		t.Fatal("provenance round trip", err)
	}
	fresh, err := attribution.WithContext(t.Context(), restored)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := api.Require(fresh); !errors.Is(err, fault.Missing) {
		t.Fatal("restored provenance authenticated", err)
	}
	anonymous, err := r.NewScope(fresh, auth.Credentials{})
	if err != nil {
		t.Fatal(err)
	}
	defer anonymous.Close()
	if _, err := api.Origin(anonymous.Context()); !errors.Is(err, auth.Unauthenticated) {
		t.Fatal("metadata replaced credential", err)
	}
}

func TestGuardOriginRejectsIncompleteAuthentication(t *testing.T) {
	for _, mode := range []string{"absent", "invalid", "pending", "disabled", "canceled", "unregistered"} {
		t.Run(mode, func(t *testing.T) {
			var loads atomic.Int32
			p := provider(func(context.Context, accountKey) (value.Optional[account], error) {
				loads.Add(1)
				return value.Set(account{ID: 7, Enabled: mode != "disabled"}), nil
			})
			state := auth.Authenticated
			if mode == "pending" {
				state = auth.PendingMFA
			}
			verified := proof(t, 7, state)
			verifier := auth.DefineStrategy("bearer", func(context.Context, secret.String) (value.Optional[auth.Proof[account, accountKey]], error) {
				if mode == "invalid" {
					return value.Optional[auth.Proof[account, accountKey]]{}, nil
				}
				return value.Set(verified), nil
			})
			guard := auth.DefineGuard("api", p, verifier)
			r := registry(t, guard.Registration())
			if mode == "unregistered" {
				r = registry(t)
			}
			credentials := inputs(t, auth.Credential{Name: "bearer", Secret: secret.New("valid")})
			if mode == "absent" {
				credentials = auth.Credentials{}
			}
			parent, cancel := context.WithCancel(t.Context())
			defer cancel()
			owned, err := r.NewScope(parent, credentials)
			if err != nil {
				t.Fatal(err)
			}
			defer owned.Close()
			if mode == "canceled" {
				cancel()
			}
			origin, err := guard.Origin(owned.Context())
			if err == nil || origin != (attribution.Origin{}) {
				t.Fatal("failed guard exposed origin", err)
			}
			ctx, err := guard.WithAttribution(owned.Context())
			if err == nil || ctx != nil {
				t.Fatal("failed guard derived context", err)
			}
			if mode != "disabled" && loads.Load() != 0 {
				t.Fatal("invalid credential hydrated model")
			}
		})
	}
}
