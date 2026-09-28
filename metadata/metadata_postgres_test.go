package metadata

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/extensions"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/extensiontest"
	"github.com/weiloon1234/Foundry-Go/model"
)

func TestPostgresTypedValuesBatchVisibilityAndOwners(t *testing.T) {
	f := extensiontest.Open(t, Migrations())
	owner, other := extensiontest.Members, extensiontest.Others
	theme := Define(owner, "theme", 1, contract.StringJSON[string]())
	otherTheme := Define(other, "theme", 1, contract.StringJSON[string]())
	dynamic := Define(owner, "properties", 1, contract.DynamicJSON())
	m, err := New(f.Store, theme.Registration(), otherTheme.Registration(), dynamic.Registration())
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []int64{1, 2, 3} {
		if err := theme.Set(t.Context(), m, owner.Reference(key), "dark"); err != nil {
			t.Fatal(err)
		}
	}
	if err := otherTheme.Set(t.Context(), m, other.Reference(1), "different"); err != nil {
		t.Fatal(err)
	}
	input := json.RawMessage(`{"enabled":true,"nested":[1,2]}`)
	if err := dynamic.Set(t.Context(), m, owner.Reference(1), input); err != nil {
		t.Fatal(err)
	}
	input[2] = 'X'
	f.Queries.Store(0)
	batch, err := theme.Load(t.Context(), m, []model.Reference[extensiontest.Member, int64]{owner.Reference(1), owner.Reference(2), owner.Reference(3), owner.Reference(4)})
	if err != nil {
		t.Fatal(err)
	}
	if n := f.Queries.Load(); n != 2 {
		t.Fatal("metadata batch performed per-owner queries", n)
	}
	for _, key := range []int64{1, 2, 3} {
		v, err := batch.Get(t.Context(), owner.Reference(key))
		text, present := v.Get()
		if err != nil || !present || text != "dark" {
			t.Fatal("typed batch", err)
		}
	}
	if v, err := batch.Get(t.Context(), owner.Reference(4)); err != nil || v.IsSet() {
		t.Fatal("missing key confused with missing owner", err)
	}
	if _, err := batch.Get(t.Context(), owner.Reference(99)); !errors.Is(err, database.NotFound) {
		t.Fatal("missing owner", err)
	}
	if f.Queries.Load() != 2 {
		t.Fatal("batch access performed database I/O")
	}
	all, err := All(t.Context(), m, owner, owner.Reference(1))
	if err != nil || len(all) != 2 {
		t.Fatal("complete metadata", err)
	}
	for _, row := range all {
		if row.Name() == "properties" {
			raw, err := row.DynamicValue()
			if err != nil || string(raw) != `{"enabled":true,"nested":[1,2]}` {
				t.Fatal("input snapshot changed", err)
			}
		}
		if _, err := json.Marshal(row); err == nil {
			t.Fatal("metadata implicitly serialized")
		}
	}
	if err := f.Store.Write(t.Context(), func(ctx context.Context, tx *database.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE extension_members SET deleted_at=CURRENT_TIMESTAMP WHERE id=1`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := theme.Get(t.Context(), m, owner.Reference(1)); !errors.Is(err, database.NotFound) {
		t.Fatal("soft-deleted owner remained readable", err)
	}
	if err := theme.Set(t.Context(), m, owner.Reference(1), "new"); !errors.Is(err, database.NotFound) {
		t.Fatal("soft-deleted owner remained writable", err)
	}
	page, err := InspectOrphans(t.Context(), m, owner.Name(), Cursor{}, 100)
	if err != nil || len(page.Orphans) != 0 {
		t.Fatal("soft deletion treated as orphan", err)
	}
	if err := f.Store.Write(t.Context(), func(ctx context.Context, tx *database.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE extension_members SET deleted_at=NULL WHERE id=1`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if v, err := theme.Get(t.Context(), m, owner.Reference(1)); err != nil || !v.IsSet() {
		t.Fatal("restore lost metadata", err)
	}
	foreign, err := otherTheme.Get(t.Context(), m, other.Reference(1))
	text, _ := foreign.Get()
	if err != nil || text != "different" {
		t.Fatal("same-key model collision", err)
	}
}

func TestPostgresRollbackCorruptionAndLifecycleCleanup(t *testing.T) {
	f := extensiontest.Open(t, Migrations())
	owner := extensiontest.Members
	flag := Define(owner, "flag", 1, contract.BooleanJSON[bool]())
	m, err := New(f.Store, flag.Registration())
	if err != nil {
		t.Fatal(err)
	}
	veto := errors.New("rollback")
	if err := f.DB.Transaction(t.Context(), func(tx *database.Tx) error {
		if err := flag.SetIn(t.Context(), tx, m, owner.Reference(1), true); err != nil {
			return err
		}
		return veto
	}); !errors.Is(err, veto) {
		t.Fatal(err)
	}
	if v, err := flag.Get(t.Context(), m, owner.Reference(1)); err != nil || v.IsSet() {
		t.Fatal("metadata escaped parent rollback", err)
	}
	if err := flag.Set(t.Context(), m, owner.Reference(1), true); err != nil {
		t.Fatal(err)
	}
	if err := f.Store.Write(t.Context(), func(ctx context.Context, tx *database.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE foundry_model_metadata SET value='"wrong"'::jsonb`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := flag.Get(t.Context(), m, owner.Reference(1)); err == nil {
		t.Fatal("stored type drift silently coerced")
	}
	if err := flag.Set(t.Context(), m, owner.Reference(1), false); err != nil {
		t.Fatal(err)
	}
	if err := f.Store.Write(t.Context(), func(ctx context.Context, tx *database.Tx) error {
		return Cleanup(ctx, tx, m, owner, owner.Reference(1), lifecycle.Delete)
	}); !errors.Is(err, fault.Conflict) {
		t.Fatal("live owner cleanup accepted", err)
	}
	if err := f.Store.Write(t.Context(), func(ctx context.Context, tx *database.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM extension_members WHERE id=1`); err != nil {
			return err
		}
		if err := Cleanup(ctx, tx, m, owner, owner.Reference(1), lifecycle.Delete); err != nil {
			return err
		}
		return veto
	}); !errors.Is(err, veto) {
		t.Fatal(err)
	}
	if v, err := flag.Get(t.Context(), m, owner.Reference(1)); err != nil || !v.IsSet() {
		t.Fatal("cleanup escaped owner rollback", err)
	}
	if err := f.Store.Write(t.Context(), func(ctx context.Context, tx *database.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM extension_members WHERE id=1`); err != nil {
			return err
		}
		return Cleanup(ctx, tx, m, owner, owner.Reference(1), lifecycle.ForceDelete)
	}); err != nil {
		t.Fatal(err)
	}
	page, err := InspectOrphans(t.Context(), m, owner.Name(), Cursor{}, 100)
	if err != nil || page.Scanned != 0 {
		t.Fatal("owner deletion left metadata", err)
	}
}

func TestPostgresConcurrentSetAndOrphanPages(t *testing.T) {
	f := extensiontest.Open(t, Migrations())
	owner := extensiontest.Members
	key := Define(owner, "number", 1, contract.IntegerJSON[int64]())
	m, err := New(f.Store, key.Registration())
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := range int64(8) {
		wg.Go(func() {
			if err := key.Set(t.Context(), m, owner.Reference(1), i); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	for i := int64(2); i <= 10; i++ {
		if err := key.Set(t.Context(), m, owner.Reference(i), i); err != nil {
			t.Fatal(err)
		}
	}
	all, err := All(t.Context(), m, owner, owner.Reference(1))
	if err != nil || len(all) != 1 {
		t.Fatal("simultaneous upserts duplicated keys", err)
	}
	if err := f.Store.Write(t.Context(), func(ctx context.Context, tx *database.Tx) error {
		_, err := tx.Exec(ctx, `DELETE FROM extension_members WHERE id IN (3,7,9)`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	var cursor Cursor
	scanned := 0
	var orphanKeys []string
	for {
		page, err := InspectOrphans(t.Context(), m, owner.Name(), cursor, 2)
		if err != nil {
			t.Fatal(err)
		}
		scanned += page.Scanned
		for _, orphan := range page.Orphans {
			orphanKeys = append(orphanKeys, orphan.Key)
		}
		if page.Next.IsZero() {
			break
		}
		cursor = page.Next
	}
	if scanned != 10 || len(orphanKeys) != 3 {
		t.Fatal("orphan pages skipped later records", scanned, len(orphanKeys))
	}
	if n, err := PruneOrphans(t.Context(), m, owner.Name(), orphanKeys); err != nil || n != 3 {
		t.Fatal("orphan pruning", n, err)
	}
	if n, err := PruneOrphans(t.Context(), m, owner.Name(), orphanKeys); err != nil || n != 0 {
		t.Fatal("orphan pruning not idempotent", n, err)
	}
	if _, err := InspectOrphans(t.Context(), m, extensions.OwnerName("unregistered"), Cursor{}, 2); err == nil {
		t.Fatal("unknown owner table guessed")
	}
}
