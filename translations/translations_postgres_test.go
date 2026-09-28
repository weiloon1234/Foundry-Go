package translations

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/i18n"
	"github.com/weiloon1234/Foundry-Go/internal/extensiontest"
	"github.com/weiloon1234/Foundry-Go/model"
)

type countingCatalog struct {
	set   i18n.LocaleSet
	calls atomic.Int64
}

func (c *countingCatalog) Snapshot(ctx context.Context) (i18n.LocaleSet, error) {
	c.calls.Add(1)
	return c.set.Snapshot(ctx)
}

func TestPostgresTypedTranslationBatchAndNaturalKeys(t *testing.T) {
	fixture := extensiontest.Open(t, Migrations())
	owner := extensiontest.Members
	name := Define(owner, "name", Options{MaxBytes: 100})
	other := Define(extensiontest.Others, "name", Options{})
	product := Define(extensiontest.Products, "title", Options{})
	catalog := &countingCatalog{set: testLocales(t)}
	m, err := New(fixture.Store, catalog, name.Registration(), other.Registration(), product.Registration())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := New(fixture.Store, catalog, name.Registration(), name.Registration()); !errors.Is(err, fault.Duplicate) {
		t.Fatal("duplicate fields", err)
	}
	if err := Set(t.Context(), m, owner.Reference(1), name.SetValue("en", "English"), name.SetValue("ms", "Melayu")); err != nil {
		t.Fatal(err)
	}
	if err := name.Set(t.Context(), m, owner.Reference(2), "zh", "中文"); err != nil {
		t.Fatal(err)
	}
	if err := other.Set(t.Context(), m, extensiontest.Others.Reference(1), "en", "Other model"); err != nil {
		t.Fatal(err)
	}
	if err := product.Set(t.Context(), m, extensiontest.Products.Reference("shirt:蓝/L"), "en", "Shirt"); err != nil {
		t.Fatal(err)
	}
	if result, err := product.Get(t.Context(), m, extensiontest.Products.Reference("shirt:蓝/L"), "en"); err != nil || !result.IsSet() {
		t.Fatal("natural string owner key", err)
	}
	fixture.Queries.Store(0)
	catalog.calls.Store(0)
	batch, err := name.Load(t.Context(), m, []model.Reference[extensiontest.Member, int64]{owner.Reference(1), owner.Reference(2), owner.Reference(3)})
	if err != nil {
		t.Fatal(err)
	}
	if fixture.Queries.Load() != 2 || catalog.calls.Load() != 1 {
		t.Fatal("batch ownership or locale snapshot was repeated", fixture.Queries.Load(), catalog.calls.Load())
	}
	values, err := batch.Get(owner.Reference(1))
	if err != nil {
		t.Fatal(err)
	}
	result, err := values.Resolve("zh")
	resolved, ok := result.Get()
	if err != nil || !ok || resolved.Locale != "en" || resolved.Text != "English" {
		t.Fatal("default fallback", err)
	}
	values, err = batch.Get(owner.Reference(2))
	if err != nil {
		t.Fatal(err)
	}
	result, err = values.Resolve("ms")
	resolved, ok = result.Get()
	if err != nil || !ok || resolved.Locale != "zh" {
		t.Fatal("final fallback", err)
	}
	if _, err := batch.Get(owner.Reference(99)); !errors.Is(err, database.NotFound) {
		t.Fatal("absent owner", err)
	}
	values, err = batch.Get(owner.Reference(3))
	if err != nil {
		t.Fatal(err)
	}
	if v, err := values.Resolve("en"); err != nil || v.IsSet() {
		t.Fatal("present owner with no value", err)
	}
	if fixture.Queries.Load() != 2 {
		t.Fatal("batch access performed lazy per-owner query")
	}
	if err := name.Set(t.Context(), m, owner.Reference(1), "fr", "invalid"); err == nil {
		t.Fatal("unsupported locale accepted")
	}
	if err := name.Set(t.Context(), m, owner.Reference(1), "en", strings.Repeat("x", 101)); err == nil {
		t.Fatal("descriptor value bound ignored")
	}
	if err := name.Set(t.Context(), m, owner.Reference(1), "en", "bad\x00text"); err == nil {
		t.Fatal("database-invalid text accepted")
	}
	if err := fixture.Store.Write(t.Context(), func(ctx context.Context, tx *database.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE extension_members SET deleted_at=CURRENT_TIMESTAMP WHERE id=1`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := name.Get(t.Context(), m, owner.Reference(1), "en"); !errors.Is(err, database.NotFound) {
		t.Fatal("soft-deleted owner remained readable", err)
	}
	if err := name.Set(t.Context(), m, owner.Reference(1), "en", "new"); !errors.Is(err, database.NotFound) {
		t.Fatal("soft-deleted owner remained writable", err)
	}
	page, err := InspectOrphans(t.Context(), m, owner.Name(), Cursor{}, 10)
	if err != nil || len(page.Orphans) != 0 {
		t.Fatal("soft-deleted owner treated as orphan", err)
	}
	if err := fixture.Store.Write(t.Context(), func(ctx context.Context, tx *database.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE extension_members SET deleted_at=NULL WHERE id=1`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if result, err := name.Get(t.Context(), m, owner.Reference(1), "ms"); err != nil || !result.IsSet() {
		t.Fatal("restore lost translations", err)
	}
}

func TestPostgresTranslationAtomicWritesRollbackAndLocaleChanges(t *testing.T) {
	fixture := extensiontest.Open(t, Migrations())
	owner := extensiontest.Members
	name := Define(owner, "name", Options{})
	description := Define(owner, "description", Options{})
	catalog := &countingCatalog{set: testLocales(t)}
	m, err := New(fixture.Store, catalog, name.Registration(), description.Registration())
	if err != nil {
		t.Fatal(err)
	}
	if err := Set(t.Context(), m, owner.Reference(1), name.SetValue("en", "must roll back"), description.SetValue("fr", "unsupported")); err == nil {
		t.Fatal("invalid atomic set")
	}
	if rows, err := All(t.Context(), m, owner, owner.Reference(1)); err != nil || len(rows) != 0 {
		t.Fatal("partial atomic set", err)
	}
	if err := Set(t.Context(), m, owner.Reference(1), name.SetValue("en", "one"), name.SetValue("en", "two")); !errors.Is(err, fault.Duplicate) {
		t.Fatal("duplicate assignment", err)
	}
	if err := name.Set(t.Context(), m, owner.Reference(1), "en", "original"); err != nil {
		t.Fatal(err)
	}
	veto := errors.New("rollback")
	if err := fixture.DB.Transaction(t.Context(), func(tx *database.Tx) error {
		if err := name.SetIn(t.Context(), tx, m, owner.Reference(1), "en", "discard"); err != nil {
			return err
		}
		return veto
	}); !errors.Is(err, veto) {
		t.Fatal(err)
	}
	result, err := name.Get(t.Context(), m, owner.Reference(1), "en")
	got, _ := result.Get()
	if err != nil || got != "original" {
		t.Fatal("translation escaped rollback", err)
	}
	if err := name.Set(t.Context(), m, owner.Reference(1), "ms", "retained"); err != nil {
		t.Fatal(err)
	}
	catalog.set, err = i18n.NewLocaleSet("en", "en", "zh")
	if err != nil {
		t.Fatal(err)
	}
	values, err := name.Values(t.Context(), m, owner.Reference(1))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := values.Entries()["ms"]; ok {
		t.Fatal("removed locale leaked into active values")
	}
	if rows, err := All(t.Context(), m, owner, owner.Reference(1)); err != nil || len(rows) != 2 {
		t.Fatal("administrative inspection lost removed locale", err)
	}
	if err := fixture.Store.Write(t.Context(), func(ctx context.Context, tx *database.Tx) error {
		return Cleanup(ctx, tx, m, owner, owner.Reference(1), lifecycle.Delete)
	}); !errors.Is(err, fault.Conflict) {
		t.Fatal("live owner cleanup", err)
	}
	if err := fixture.Store.Write(t.Context(), func(ctx context.Context, tx *database.Tx) error {
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
	if rows, err := All(t.Context(), m, owner, owner.Reference(1)); err != nil || len(rows) != 2 {
		t.Fatal("cleanup escaped owner rollback", err)
	}
	if err := fixture.Store.Write(t.Context(), func(ctx context.Context, tx *database.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM extension_members WHERE id=1`); err != nil {
			return err
		}
		return Cleanup(ctx, tx, m, owner, owner.Reference(1), lifecycle.ForceDelete)
	}); err != nil {
		t.Fatal(err)
	}
	if page, err := InspectOrphans(t.Context(), m, owner.Name(), Cursor{}, 10); err != nil || page.Scanned != 0 {
		t.Fatal("cleanup left rows", err)
	}
}

func TestPostgresTranslationConcurrentUpsertsAndOrphanPages(t *testing.T) {
	fixture := extensiontest.Open(t, Migrations())
	owner := extensiontest.Members
	name := Define(owner, "name", Options{})
	m, err := New(fixture.Store, testLocales(t), name.Registration())
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			if err := name.Set(t.Context(), m, owner.Reference(1), "en", "updated"); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	for i := int64(2); i <= 10; i++ {
		if err := name.Set(t.Context(), m, owner.Reference(i), "en", "value"); err != nil {
			t.Fatal(err)
		}
	}
	if rows, err := All(t.Context(), m, owner, owner.Reference(1)); err != nil || len(rows) != 1 {
		t.Fatal("concurrent upsert duplicated rows", err)
	}
	if err := fixture.Store.Write(t.Context(), func(ctx context.Context, tx *database.Tx) error {
		_, err := tx.Exec(ctx, `DELETE FROM extension_members WHERE id IN (2,8,9)`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	scanned := 0
	var keys []string
	var cursor Cursor
	for {
		page, err := InspectOrphans(t.Context(), m, owner.Name(), cursor, 2)
		if err != nil {
			t.Fatal(err)
		}
		scanned += page.Scanned
		for _, row := range page.Orphans {
			keys = append(keys, row.Key)
		}
		if page.Next.IsZero() {
			break
		}
		cursor = page.Next
	}
	if scanned != 10 || len(keys) != 3 {
		t.Fatal("orphan pagination", scanned, len(keys))
	}
	if n, err := PruneOrphans(t.Context(), m, owner.Name(), keys); err != nil || n != 3 {
		t.Fatal("prune", n, err)
	}
	if n, err := PruneOrphans(t.Context(), m, owner.Name(), keys); err != nil || n != 0 {
		t.Fatal("idempotent prune", n, err)
	}
}
