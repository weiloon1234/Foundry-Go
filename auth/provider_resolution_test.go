package auth_test

import (
	"context"
	"errors"
	"runtime"
	"testing"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestProviderResolveReusesIdentityEligibilityAndDeclaration(t *testing.T) {
	current := account{ID: 7, Enabled: true}
	p := provider(func(context.Context, accountKey) (value.Optional[account], error) { return value.Set(current), nil })
	guard := auth.DefineGuard("resolve", p, strategy(t, "bearer", 7))
	if err := p.ValidateGuard(guard); err != nil {
		t.Fatal(err)
	}
	other := provider(func(context.Context, accountKey) (value.Optional[account], error) { return value.Set(current), nil })
	if err := other.ValidateGuard(guard); !errors.Is(err, fault.Invalid) {
		t.Fatal("same-name authority substitution", err)
	}
	reference := current.FoundryReference()
	if found, err := p.Resolve(t.Context(), reference); err != nil || found.ID != 7 {
		t.Fatal(err)
	}
	current.Enabled = false
	if _, err := p.Resolve(t.Context(), reference); !errors.Is(err, auth.Unauthenticated) {
		t.Fatal("ineligible model returned", err)
	}
	current = account{ID: 8, Enabled: true}
	if _, err := p.Resolve(t.Context(), reference); !errors.Is(err, fault.Invalid) {
		t.Fatal("wrong identity returned", err)
	}
	if _, err := p.Resolve(nil, reference); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
}
func TestProviderResolveOwnsAbnormalLookupExit(t *testing.T) {
	for _, exit := range []func(){func() { panic("private") }, runtime.Goexit} {
		p := provider(func(context.Context, accountKey) (value.Optional[account], error) {
			exit()
			return value.Optional[account]{}, nil
		})
		if _, err := p.Resolve(t.Context(), account{ID: 7}.FoundryReference()); !errors.Is(err, fault.Panicked) {
			t.Fatal("callback escaped", err)
		}
	}
}
