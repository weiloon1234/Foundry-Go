package cachestore_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/cache"
	"github.com/weiloon1234/Foundry-Go/cache/memory"
	"github.com/weiloon1234/Foundry-Go/clock"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/maintenance"
	"github.com/weiloon1234/Foundry-Go/maintenance/cachestore"
)

func TestCacheStoreSharesStateBetweenGates(t *testing.T) {
	backend, err := memory.New(memory.DefaultConfig(), clock.System{})
	if err != nil {
		t.Fatal(err)
	}
	defer backend.Close(context.Background())
	shared, err := cache.NewStore(backend, cache.DefaultConfig(cache.Namespace{Application: "maintenance", Environment: "test"}))
	if err != nil {
		t.Fatal(err)
	}
	first, err := cachestore.New(shared)
	if err != nil {
		t.Fatal(err)
	}
	second, err := cachestore.New(shared)
	if err != nil {
		t.Fatal(err)
	}
	if _, found, err := first.Load(t.Context()); err != nil || found {
		t.Fatal("empty store reported a record", err)
	}
	issuer, observer := &maintenance.Gate{}, &maintenance.Gate{}
	digest, _ := maintenance.DigestSecret("shared-secret-value-01")
	state := maintenance.State{Down: true, RetryAfter: 30 * time.Second, Message: "Upgrading", Secret: digest}
	if err := maintenance.Publish(t.Context(), issuer, first, state); err != nil {
		t.Fatal(err)
	}
	if err := maintenance.Refresh(t.Context(), observer, second); err != nil {
		t.Fatal(err)
	}
	applied := observer.State()
	if observer.Mode() != maintenance.Paused || applied.Message != "Upgrading" || applied.RetryAfter != 30*time.Second || applied.Secret != digest {
		t.Fatal("instance did not apply the shared record", applied)
	}
	if err := first.Save(t.Context(), maintenance.State{Message: "invalid while serving"}); !errors.Is(err, fault.Invalid) {
		t.Fatal("invalid record saved", err)
	}
	if _, err := cachestore.New(nil); !errors.Is(err, fault.Invalid) {
		t.Fatal("missing cache store accepted")
	}
}
