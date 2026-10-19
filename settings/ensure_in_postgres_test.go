package settings

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/extensiontest"
)

func TestPostgresEnsureInRollbackAndPreservation(t *testing.T) {
	f := extensiontest.Open(t, Migrations())
	key := Define("site.seeded", 1, contract.StringJSON[string](), Presentation{Label: "Seeded", Group: "site"})
	m, err := New(f.Store, key.RegistrationWith(Options[string]{Cache: time.Hour}))
	if err != nil {
		t.Fatal(err)
	}
	if err := key.EnsureIn(t.Context(), nil, m, "seed"); !errors.Is(err, fault.Invalid) {
		t.Fatal("nil transaction accepted", err)
	}
	if got, err := key.GetOr(t.Context(), m, "missing"); err != nil || got != "missing" {
		t.Fatal("missing setting", got, err)
	}
	veto := errors.New("rollback seed")
	if err := f.DB.Transaction(t.Context(), func(tx *database.Tx) error {
		if err := key.EnsureIn(t.Context(), tx, m, "seed"); err != nil {
			return err
		}
		return veto
	}); !errors.Is(err, veto) {
		t.Fatal(err)
	}
	if got, err := key.Get(t.Context(), m); err != nil || got.IsSet() {
		t.Fatal("seed escaped parent rollback", err)
	}
	if err := f.DB.Transaction(t.Context(), func(tx *database.Tx) error {
		return key.EnsureIn(t.Context(), tx, m, "seed")
	}); err != nil {
		t.Fatal(err)
	}
	if got, err := key.GetOr(t.Context(), m, "missing"); err != nil || got != "seed" {
		t.Fatal("commit did not invalidate cached absence", got, err)
	}
	if err := key.Set(t.Context(), m, "admin value"); err != nil {
		t.Fatal(err)
	}
	custom := Presentation{Kind: Textarea, Label: "Admin label", Group: "custom"}
	if err := key.Configure(t.Context(), m, custom); err != nil {
		t.Fatal(err)
	}
	newKey := Define("site.seeded", 2, contract.StringJSON[string](), Presentation{Label: "New declaration", Group: "site"})
	newManager, err := New(f.Store, newKey.RegistrationWith(Options[string]{Upgrades: []Upgrade[string]{
		UpgradeFrom(1, contract.StringJSON[string](), func(_ context.Context, v string) (string, error) { return v, nil }),
	}}))
	if err != nil {
		t.Fatal(err)
	}
	if err := f.DB.Transaction(t.Context(), func(tx *database.Tx) error {
		return newKey.EnsureIn(t.Context(), tx, newManager, "replacement")
	}); err != nil {
		t.Fatal(err)
	}
	// The original declaration still reads version 1: EnsureIn did not upgrade it.
	m.Invalidate(key)
	if got, err := key.GetOr(t.Context(), m, "missing"); err != nil || got != "admin value" {
		t.Fatal("existing value or version replaced", got, err)
	}
	found, err := key.Find(t.Context(), m)
	row, ok := found.Get()
	if err != nil || !ok || !row.Configured() || row.Presentation().Label != custom.Label || row.Presentation().Kind != custom.Kind {
		t.Fatal("configured presentation replaced", err)
	}
}

func TestPostgresEnsureInConcurrentInitialization(t *testing.T) {
	f := extensiontest.Open(t, Migrations())
	key := Define("site.concurrent_seed", 1, contract.StringJSON[string](), Presentation{})
	m, err := New(f.Store, key.Registration())
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	var workers sync.WaitGroup
	for i := range 8 {
		workers.Go(func() {
			<-start
			if err := f.DB.Transaction(t.Context(), func(tx *database.Tx) error {
				return key.EnsureIn(t.Context(), tx, m, strconv.Itoa(i))
			}); err != nil {
				t.Error(err)
			}
		})
	}
	close(start)
	workers.Wait()
	rows, err := List(t.Context(), m, Filter{})
	if err != nil || len(rows) != 1 {
		t.Fatal("concurrent seed did not create exactly one setting", err)
	}
	first, err := key.GetOr(t.Context(), m, "missing")
	winner, parseErr := strconv.Atoi(first)
	if err != nil || parseErr != nil || winner < 0 || winner >= 8 {
		t.Fatal("seed value lost", first, err)
	}
	if err := f.DB.Transaction(t.Context(), func(tx *database.Tx) error {
		return key.EnsureIn(t.Context(), tx, m, "replacement")
	}); err != nil {
		t.Fatal(err)
	}
	if got, err := key.GetOr(t.Context(), m, "missing"); err != nil || got != first {
		t.Fatal("repeat seed changed the winner", got, err)
	}
}
