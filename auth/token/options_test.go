package token

import (
	"errors"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/fault"
)

// One store serves users and admins: the admin guard shortens its own families'
// lifetimes, a refresh keeps the issued lifetime, and other modes keep the store's.
func TestGuardLifetimesShortenOnlyThatGuardsFamilies(t *testing.T) {
	backend := newMemoryBackend()
	config := settings()
	admin := Lifetime{Access: 5 * time.Minute, RefreshIdle: 12 * time.Hour, Absolute: 24 * time.Hour}
	admins := bindingWithin(t, backend, config, scopes(t, "orders.read"), WithLifetimes(Lifetimes{Renewable: admin}))
	issued, err := admins.Issue(t.Context(), proof(t, auth.Authenticated), IssueOptions[member]{Refresh: true})
	if err != nil {
		t.Fatal(err)
	}
	info := issued.Info()
	if backend.saved.Lifetime != admin || info.ExpiresAt().UTC().Sub(info.CreatedAt().UTC()) != admin.Absolute || info.AccessExpiresAt().UTC().Sub(info.IssuedAt().UTC()) != admin.Access {
		t.Fatal("admin family did not use the guard's lifetime", backend.saved.Lifetime)
	}
	raw, _ := issued.RefreshSecret().Get()
	if _, err := admins.Refresh(t.Context(), raw); err != nil || backend.saved.Lifetime != admin {
		t.Fatal("refresh changed the issued lifetime", err)
	}
	if _, err := admins.Issue(t.Context(), proof(t, auth.Authenticated), IssueOptions[member]{}); err != nil || backend.saved.Lifetime != config.Personal {
		t.Fatal("personal token did not keep the store's lifetime", err)
	}
	users := bindingWithin(t, backend, config, scopes(t, "orders.read"))
	if _, err := users.Issue(t.Context(), proof(t, auth.Authenticated), IssueOptions[member]{Refresh: true}); err != nil || backend.saved.Lifetime != config.Renewable {
		t.Fatal("another guard on the store was shortened", err)
	}
}

func TestGuardLifetimesStayWithinTheStore(t *testing.T) {
	config := settings()
	store, err := NewStore(newMemoryBackend(), config)
	if err != nil {
		t.Fatal(err)
	}
	bind := func(opts ...Option) error {
		_, err := New(store, "admin", memberProvider(), "admin.bearer", scopes(t, "orders.read"), opts...)
		return err
	}
	for name, test := range map[string]struct {
		opts []Option
		want error
	}{
		"longer access":       {[]Option{WithLifetimes(Lifetimes{Renewable: Lifetime{Access: config.Renewable.Access + time.Minute, RefreshIdle: time.Hour, Absolute: 24 * time.Hour}})}, fault.Invalid},
		"longer refresh idle": {[]Option{WithLifetimes(Lifetimes{Renewable: Lifetime{Access: time.Minute, RefreshIdle: config.Renewable.RefreshIdle + time.Hour, Absolute: config.Renewable.Absolute}})}, fault.Invalid},
		"longer personal":     {[]Option{WithLifetimes(Lifetimes{Personal: Lifetime{Access: config.Personal.Absolute + time.Hour, Absolute: config.Personal.Absolute + time.Hour}})}, fault.Invalid},
		"invalid for mode":    {[]Option{WithLifetimes(Lifetimes{Personal: Lifetime{Access: time.Hour, Absolute: 2 * time.Hour}})}, fault.Invalid},
		"partial":             {[]Option{WithLifetimes(Lifetimes{Renewable: Lifetime{Access: time.Minute}})}, fault.Invalid},
		"below access grace":  {[]Option{WithLifetimes(Lifetimes{Renewable: Lifetime{Access: config.AccessGrace / 2, RefreshIdle: time.Hour, Absolute: 24 * time.Hour}})}, fault.Invalid},
		"repeated":            {[]Option{WithLifetimes(Lifetimes{}), WithLifetimes(Lifetimes{})}, fault.Duplicate},
		"nil":                 {[]Option{nil}, fault.Invalid},
	} {
		if err := bind(test.opts...); !errors.Is(err, test.want) {
			t.Fatalf("%s: %v", name, err)
		}
	}
	if err := bind(WithLifetimes(Lifetimes{Challenge: Lifetime{Access: time.Minute, Absolute: time.Minute}})); err != nil {
		t.Fatal("a shorter challenge lifetime was rejected", err)
	}
}
