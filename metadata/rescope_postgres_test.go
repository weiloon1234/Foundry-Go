package metadata

import (
	"context"
	"testing"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/extensions"
	"github.com/weiloon1234/Foundry-Go/internal/extensiontest"
	"github.com/weiloon1234/Foundry-Go/value"
)

func valueOf(text string) value.Optional[string] { return value.Set(text) }

// renamedStore shares the fixture database and schema with a registry that
// declares the member owner over its renamed table.
func renamedStore(t *testing.T, f extensiontest.Fixture, owner extensions.Owner[extensiontest.Member, int64]) *extensions.Store {
	t.Helper()
	registry, err := extensions.NewRegistry(owner.Registration())
	if err != nil {
		t.Fatal(err)
	}
	config := extensions.DefaultConfig()
	config.Schema = f.Schema
	store, err := extensions.New(f.DB, registry, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	return store
}

func TestPostgresTableRenameKeepsMetadataThroughStorageModelAndRescope(t *testing.T) {
	f := extensiontest.Open(t, Migrations())
	theme := Define(extensiontest.Members, "theme", 1, contract.StringJSON[string]())
	m, err := New(f.Store, theme.Registration())
	if err != nil {
		t.Fatal(err)
	}
	for key, text := range map[int64]string{1: "dark", 2: "light"} {
		if err := theme.Set(t.Context(), m, extensiontest.Members.Reference(key), text); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.Store.Write(t.Context(), func(ctx context.Context, tx *database.Tx) error {
		_, err := tx.Exec(ctx, `ALTER TABLE extension_members RENAME TO extension_people`)
		return err
	}); err != nil {
		t.Fatal(err)
	}

	// A declared storage model keeps the persisted scope: no data migration.
	pinned := extensions.DefineOwnerWith("members", extensiontest.MemberIdentity("extension_people"), extensions.OwnerOptions{StorageModel: "extension_members"})
	pinnedTheme := Define(pinned, "theme", 1, contract.StringJSON[string]())
	pm, err := New(renamedStore(t, f, pinned), pinnedTheme.Registration())
	if err != nil {
		t.Fatal(err)
	}
	if got, err := pinnedTheme.Get(t.Context(), pm, pinned.Reference(1)); err != nil || got != (valueOf("dark")) {
		t.Fatal("pinned owner lost data after a rename", err)
	}
	if all, err := All(t.Context(), pm, pinned, pinned.Reference(2)); err != nil || len(all) != 1 {
		t.Fatal("pinned owner rejected pre-rename identities", err)
	}
	if err := pinnedTheme.Set(t.Context(), pm, pinned.Reference(1), "darker"); err != nil {
		t.Fatal(err)
	}

	// An undeclared rename changes the scope; rows are hidden, not corrupted.
	// Re-scoping adopts them only once the earlier model is declared.
	undeclared := extensions.DefineOwner("members", extensiontest.MemberIdentity("extension_people"))
	undeclaredTheme := Define(undeclared, "theme", 1, contract.StringJSON[string]())
	um, err := New(renamedStore(t, f, undeclared), undeclaredTheme.Registration())
	if err != nil {
		t.Fatal(err)
	}
	refused, err := Rescope(t.Context(), um, "members", Cursor{}, 10)
	if err != nil || len(refused.Rows) != 0 || len(refused.Undeclared) != 2 {
		t.Fatal("rows of an undeclared model were adopted", refused, err)
	}
	renamed := extensions.DefineOwnerWith("members", extensiontest.MemberIdentity("extension_people"), extensions.OwnerOptions{PreviousModels: []string{"extension_members"}})
	renamedTheme := Define(renamed, "theme", 1, contract.StringJSON[string]())
	rm, err := New(renamedStore(t, f, renamed), renamedTheme.Registration())
	if err != nil {
		t.Fatal(err)
	}
	if got, err := renamedTheme.Get(t.Context(), rm, renamed.Reference(1)); err != nil || got.IsSet() {
		t.Fatal("renamed scope unexpectedly shared", err)
	}
	stale, err := InspectStale(t.Context(), rm, "members", Cursor{}, 1)
	if err != nil || len(stale.Rows) != 1 || stale.Next.IsZero() {
		t.Fatal("stale inspection page", err)
	}
	if got, err := renamedTheme.Get(t.Context(), rm, renamed.Reference(1)); err != nil || got.IsSet() {
		t.Fatal("inspection wrote data", err)
	}
	moved := 0
	var cursor Cursor
	for {
		page, err := Rescope(t.Context(), rm, "members", cursor, 1)
		if err != nil {
			t.Fatal(err)
		}
		moved += len(page.Rows)
		if len(page.Conflicts) != 0 {
			t.Fatal("unexpected conflict", page.Conflicts)
		}
		if page.Next.IsZero() {
			break
		}
		cursor = page.Next
	}
	if moved != 2 {
		t.Fatal("rescope moved", moved)
	}
	for key, text := range map[int64]string{1: "darker", 2: "light"} {
		if got, err := renamedTheme.Get(t.Context(), rm, renamed.Reference(key)); err != nil || got != valueOf(text) {
			t.Fatal("rescoped value", key, err)
		}
	}
	if again, err := InspectStale(t.Context(), rm, "members", Cursor{}, 10); err != nil || len(again.Rows)+len(again.Conflicts) != 0 {
		t.Fatal("rescope left stale rows", err)
	}

	// A current-scope row is never overwritten: the stale one stays and is reported.
	if err := pinnedTheme.Set(t.Context(), pm, pinned.Reference(1), "stale"); err != nil {
		t.Fatal(err)
	}
	conflict, err := Rescope(t.Context(), rm, "members", Cursor{}, 10)
	if err != nil || len(conflict.Rows) != 0 || len(conflict.Conflicts) != 1 {
		t.Fatal("conflict handling", err)
	}
	if got, err := renamedTheme.Get(t.Context(), rm, renamed.Reference(1)); err != nil || got != valueOf("darker") {
		t.Fatal("rescope overwrote a current value", err)
	}

	// A stale row whose subject no longer exists is reported, never moved into
	// the current scope as an orphan.
	if err := pinnedTheme.Set(t.Context(), pm, pinned.Reference(3), "gone"); err != nil {
		t.Fatal(err)
	}
	if err := f.Store.Write(t.Context(), func(ctx context.Context, tx *database.Tx) error {
		_, err := tx.Exec(ctx, `DELETE FROM extension_people WHERE id = 3`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	orphan, err := Rescope(t.Context(), rm, "members", Cursor{}, 10)
	if err != nil || len(orphan.Rows) != 0 || len(orphan.Missing) != 1 || len(orphan.Conflicts) != 1 {
		t.Fatal("missing subject handling", orphan, err)
	}
	if again, err := InspectStale(t.Context(), rm, "members", Cursor{}, 10); err != nil || len(again.Missing) != 1 {
		t.Fatal("rescope moved a row of a missing subject", again, err)
	}
}
