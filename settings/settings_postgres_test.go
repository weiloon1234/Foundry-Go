package settings

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/extensiontest"
)

func TestPostgresTypedSettingsAndPresentation(t *testing.T) {
	f := extensiontest.Open(t, Migrations())
	title := Define("site.title", 1, contract.StringJSON[string](), Presentation{Label: "Title", Group: "site", Public: true, Order: 2})
	count := Define("site.count", 1, contract.IntegerJSON[uint64](), Presentation{Kind: Number, Label: "Count", Group: "site", Order: 1})
	dynamic := Define("other.dynamic", 1, contract.DynamicJSON(), Presentation{Kind: JSON, Group: "other"})
	m, err := New(f.Store, title.Registration(), count.Registration(), dynamic.Registration())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := New(f.Store, title.Registration(), title.Registration()); !errors.Is(err, fault.Duplicate) {
		t.Fatal("duplicate registration", err)
	}
	shadow := Define("site.title", 1, contract.StringJSON[string](), Presentation{})
	if err := shadow.Upsert(t.Context(), m, "shadow"); err == nil {
		t.Fatal("unregistered lookalike declaration")
	}
	if got, err := title.GetOr(t.Context(), m, "fallback"); err != nil || got != "fallback" {
		t.Fatal("missing default", err)
	}
	if err := title.Set(t.Context(), m, "missing"); !errors.Is(err, database.NotFound) {
		t.Fatal("set created a missing setting", err)
	}
	if err := title.Create(t.Context(), m, "original"); err != nil {
		t.Fatal(err)
	}
	if err := title.Create(t.Context(), m, "duplicate"); err == nil {
		t.Fatal("duplicate create")
	}
	if err := title.Ensure(t.Context(), m, "not used"); err != nil {
		t.Fatal(err)
	}
	if got, err := title.GetOr(t.Context(), m, ""); err != nil || got != "original" {
		t.Fatal("ensure replaced existing value", err)
	}
	p := Presentation{Kind: Textarea, Label: "Custom", Group: "custom", Order: 3, Public: true}
	if err := title.Configure(t.Context(), m, p); err != nil {
		t.Fatal(err)
	}
	if err := title.Upsert(t.Context(), m, "updated"); err != nil {
		t.Fatal(err)
	}
	found, err := title.Find(t.Context(), m)
	row, ok := found.Get()
	if err != nil || !ok || row.Presentation().Label != "Custom" || row.Presentation().Kind != Textarea {
		t.Fatal("upsert changed presentation", err)
	}
	if _, err := json.Marshal(row); err == nil {
		t.Fatal("private setting record implicitly serialized")
	}
	if err := count.Upsert(t.Context(), m, ^uint64(0)); err != nil {
		t.Fatal(err)
	}
	if got, err := count.GetOr(t.Context(), m, 0); err != nil || got != ^uint64(0) {
		t.Fatal("integer precision lost", err)
	}
	raw := json.RawMessage(`{"nested":[1,2]}`)
	if err := dynamic.Upsert(t.Context(), m, raw); err != nil {
		t.Fatal(err)
	}
	raw[2] = 'X'
	got, err := dynamic.Get(t.Context(), m)
	snapshot, _ := got.Get()
	if err != nil || string(snapshot) != `{"nested":[1,2]}` {
		t.Fatal("setting retained mutable input", err)
	}
	snapshot[2] = 'Y'
	again, err := dynamic.Get(t.Context(), m)
	fresh, _ := again.Get()
	if err != nil || string(fresh) != `{"nested":[1,2]}` {
		t.Fatal("setting retained decoded value", err)
	}
	f.Queries.Store(0)
	records, err := List(t.Context(), m, Filter{})
	if err != nil || len(records) != 3 || records[0].Name() != title.Name() || f.Queries.Load() != 1 {
		t.Fatal("bounded sorted list", err, f.Queries.Load())
	}
	public, err := List(t.Context(), m, Filter{PublicOnly: true})
	if err != nil || len(public) != 1 || public[0].Name() != title.Name() {
		t.Fatal("private setting leaked to public list", err)
	}
	literal, err := List(t.Context(), m, Filter{Prefix: "site.%"})
	if err != nil || len(literal) != 0 {
		t.Fatal("prefix treated as SQL wildcard", err)
	}
	if n, err := title.Remove(t.Context(), m); err != nil || !n {
		t.Fatal("remove", err)
	}
	if n, err := title.Remove(t.Context(), m); err != nil || n {
		t.Fatal("idempotent remove", err)
	}
}

func TestPostgresSettingsConcurrencyRollbackAndCorruption(t *testing.T) {
	f := extensiontest.Open(t, Migrations())
	key := Define("feature.enabled", 1, contract.BooleanJSON[bool](), Presentation{Kind: Boolean})
	m, err := New(f.Store, key.Registration())
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			if err := key.Upsert(t.Context(), m, true); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	rows, err := List(t.Context(), m, Filter{})
	if err != nil || len(rows) != 1 {
		t.Fatal("concurrent upsert duplicated rows", err)
	}
	veto := errors.New("rollback")
	if err := f.DB.Transaction(t.Context(), func(tx *database.Tx) error {
		if err := key.SetIn(t.Context(), tx, m, false); err != nil {
			return err
		}
		return veto
	}); !errors.Is(err, veto) {
		t.Fatal(err)
	}
	if got, err := key.GetOr(t.Context(), m, false); err != nil || !got {
		t.Fatal("value escaped transaction rollback", err)
	}
	if err := f.Store.Write(t.Context(), func(ctx context.Context, tx *database.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE foundry_settings SET value='"wrong"'::jsonb`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := key.GetOr(t.Context(), m, false); err == nil {
		t.Fatal("corrupt value became a default")
	}
	if err := key.Set(t.Context(), m, false); err != nil {
		t.Fatal(err)
	}
	if err := f.Store.Write(t.Context(), func(ctx context.Context, tx *database.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE foundry_settings SET version=2`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := key.Get(t.Context(), m); !errors.Is(err, fault.Conflict) {
		t.Fatal("version mismatch silently decoded", err)
	}
	if err := key.Upsert(t.Context(), m, true); err != nil {
		t.Fatal(err)
	}
	if err := f.Store.Write(t.Context(), func(ctx context.Context, tx *database.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE foundry_settings SET kind='unknown'`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := key.Find(t.Context(), m); err == nil {
		t.Fatal("unknown stored presentation")
	}
}
