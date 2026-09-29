package settings

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/extensions"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/extensiontest"
)

type manualClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *manualClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}
func (c *manualClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// clockedStore shares the fixture database and schema with an injected clock.
func clockedStore(t *testing.T, f extensiontest.Fixture, source *manualClock) *extensions.Store {
	t.Helper()
	registry, err := extensions.NewRegistry()
	if err != nil {
		t.Fatal(err)
	}
	config := extensions.DefaultConfig()
	config.Schema, config.Clock = f.Schema, source
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
func execute(t *testing.T, f extensiontest.Fixture, statement string) {
	t.Helper()
	if err := f.Store.Write(t.Context(), func(ctx context.Context, tx *database.Tx) error {
		_, err := tx.Exec(ctx, statement)
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

func TestPostgresSettingsCacheServesHotReadsAndInvalidatesOnWrites(t *testing.T) {
	f := extensiontest.Open(t, Migrations())
	source := &manualClock{now: time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)}
	store := clockedStore(t, f, source)
	name := Define("site.name", 1, contract.StringJSON[string](), Presentation{Group: "site"})
	plain := Define("site.plain", 1, contract.StringJSON[string](), Presentation{Group: "site"})
	if _, err := New(store, name.RegistrationWith(Options[string]{Cache: MaxCacheTTL + time.Second})); err == nil {
		t.Fatal("unbounded cache TTL accepted")
	}
	m, err := New(store, name.RegistrationWith(Options[string]{Cache: time.Minute}), plain.Registration())
	if err != nil {
		t.Fatal(err)
	}
	if err := name.Ensure(t.Context(), m, "one"); err != nil {
		t.Fatal(err)
	}
	if err := plain.Ensure(t.Context(), m, "plain"); err != nil {
		t.Fatal(err)
	}
	f.Queries.Store(0)
	for range 5 {
		if got, err := name.GetOr(t.Context(), m, ""); err != nil || got != "one" {
			t.Fatal("cached read", got, err)
		}
	}
	if n := f.Queries.Load(); n != 1 {
		t.Fatal("hot cached reads queried the database", n)
	}
	for range 2 {
		if _, err := plain.GetOr(t.Context(), m, ""); err != nil {
			t.Fatal(err)
		}
	}
	if n := f.Queries.Load(); n != 3 {
		t.Fatal("an uncached key must read each time", n)
	}
	// Another process's write is visible after the TTL, never later.
	execute(t, f, `UPDATE foundry_settings SET value='"external"' WHERE name='site.name'`)
	if got, _ := name.GetOr(t.Context(), m, ""); got != "one" {
		t.Fatal("cache expired early", got)
	}
	source.Advance(time.Minute)
	if got, err := name.GetOr(t.Context(), m, ""); err != nil || got != "external" {
		t.Fatal("cache outlived its TTL", got, err)
	}
	// Writes through this manager invalidate immediately.
	if err := name.Set(t.Context(), m, "two"); err != nil {
		t.Fatal(err)
	}
	if got, _ := name.GetOr(t.Context(), m, ""); got != "two" {
		t.Fatal("stale value after Set", got)
	}
	veto := errors.New("rollback")
	if err := f.DB.Transaction(t.Context(), func(tx *database.Tx) error {
		if err := name.SetIn(t.Context(), tx, m, "rolled back"); err != nil {
			return err
		}
		return veto
	}); !errors.Is(err, veto) {
		t.Fatal(err)
	}
	if got, _ := name.GetOr(t.Context(), m, ""); got != "two" {
		t.Fatal("rolled back value visible", got)
	}
	// A read between a joined write and its commit may cache the old value; the
	// after-commit invalidation removes it.
	if err := f.DB.Transaction(t.Context(), func(tx *database.Tx) error {
		if err := name.SetIn(t.Context(), tx, m, "three"); err != nil {
			return err
		}
		got, err := name.GetOr(t.Context(), m, "")
		if err != nil || got != "two" {
			t.Error("uncommitted value visible to another connection", got, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if got, _ := name.GetOr(t.Context(), m, ""); got != "three" {
		t.Fatal("after-commit invalidation", got)
	}
	execute(t, f, `UPDATE foundry_settings SET value='"manual"' WHERE name='site.name'`)
	if err := m.Invalidate(name); err != nil {
		t.Fatal(err)
	}
	if got, _ := name.GetOr(t.Context(), m, ""); got != "manual" {
		t.Fatal("explicit invalidation", got)
	}
	if removed, err := name.Remove(t.Context(), m); err != nil || !removed {
		t.Fatal(err)
	}
	if got, _ := name.GetOr(t.Context(), m, "fallback"); got != "fallback" {
		t.Fatal("removed value cached", got)
	}
	shadow := Define("site.name", 1, contract.StringJSON[string](), Presentation{})
	if err := m.Invalidate(shadow); err == nil {
		t.Fatal("lookalike declaration accepted")
	}
}

func TestPostgresSettingsVersionUpgradesAndReconcile(t *testing.T) {
	f := extensiontest.Open(t, Migrations())
	v1 := Define("mail.port", 1, contract.StringJSON[string](), Presentation{Group: "mail", Label: "Port"})
	m1, err := New(f.Store, v1.Registration())
	if err != nil {
		t.Fatal(err)
	}
	if err := v1.Create(t.Context(), m1, "2525"); err != nil {
		t.Fatal(err)
	}
	v2 := Define("mail.port", 2, contract.IntegerJSON[int64](), Presentation{Kind: Number, Group: "mail", Label: "SMTP port"})
	strict, err := New(f.Store, v2.Registration())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := v2.Get(t.Context(), strict); !errors.Is(err, fault.Conflict) {
		t.Fatal("undeclared upgrade decoded", err)
	}
	report, err := Reconcile(t.Context(), strict)
	if !errors.Is(err, fault.Conflict) || !slices.Equal(report.Incompatible, []Name{"mail.port"}) || len(report.Upgraded) != 0 {
		t.Fatal("incompatible reconcile", report, err)
	}
	if got, err := v1.GetOr(t.Context(), m1, ""); err != nil || got != "2525" {
		t.Fatal("failed reconcile wrote data", got, err)
	}
	upgrade := UpgradeFrom(1, contract.StringJSON[string](), func(_ context.Context, old string) (int64, error) {
		return strconv.ParseInt(old, 10, 64)
	})
	for _, invalid := range []Options[int64]{
		{Upgrades: []Upgrade[int64]{UpgradeFrom(2, contract.StringJSON[string](), func(context.Context, string) (int64, error) { return 0, nil })}},
		{Upgrades: []Upgrade[int64]{upgrade, upgrade}},
		{Upgrades: []Upgrade[int64]{UpgradeFrom[string, int64](1, contract.StringJSON[string](), nil)}},
		{Upgrades: []Upgrade[int64]{{}}},
	} {
		if _, err := New(f.Store, v2.RegistrationWith(invalid)); err == nil {
			t.Fatal("invalid upgrade declaration accepted")
		}
	}
	m2, err := New(f.Store, v2.RegistrationWith(Options[int64]{Upgrades: []Upgrade[int64]{upgrade}}))
	if err != nil {
		t.Fatal(err)
	}
	found, err := v2.Find(t.Context(), m2)
	record, ok := found.Get()
	if err != nil || !ok || record.Version() != 2 {
		t.Fatal("declared upgrade not applied on read", err)
	}
	if got, err := v2.GetOr(t.Context(), m2, 0); err != nil || got != 2525 {
		t.Fatal("upgraded value", got, err)
	}
	report, err = Reconcile(t.Context(), m2)
	if err != nil || !slices.Equal(report.Upgraded, []Name{"mail.port"}) || !slices.Equal(report.Presented, []Name{"mail.port"}) {
		t.Fatal("reconcile", report, err)
	}
	if _, err := v1.Get(t.Context(), m1); !errors.Is(err, fault.Conflict) {
		t.Fatal("reconcile did not persist the new version", err)
	}
	found, err = v2.Find(t.Context(), m2)
	record, _ = found.Get()
	if err != nil || record.Presentation().Label != "SMTP port" || record.Presentation().Kind != Number {
		t.Fatal("declared presentation not refreshed", err)
	}
	if report, err := Reconcile(t.Context(), m2); err != nil || len(report.Upgraded)+len(report.Presented)+len(report.Incompatible) != 0 {
		t.Fatal("reconcile is not idempotent", report, err)
	}
	panicking := UpgradeFrom(1, contract.StringJSON[string](), func(context.Context, string) (int64, error) { panic("application bug") })
	execute(t, f, `UPDATE foundry_settings SET version=1, value='"25"'`)
	m3, err := New(f.Store, v2.RegistrationWith(Options[int64]{Upgrades: []Upgrade[int64]{panicking}}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := v2.Get(t.Context(), m3); err == nil {
		t.Fatal("panicking upgrade reported success")
	}
	execute(t, f, `UPDATE foundry_settings SET version=3`)
	if report, err := Reconcile(t.Context(), m2); !errors.Is(err, fault.Conflict) || len(report.Incompatible) != 1 {
		t.Fatal("a newer stored version must never be downgraded", report, err)
	}
}

func TestPostgresSettingsPresentationFollowsDeclarationsUntilConfigured(t *testing.T) {
	f := extensiontest.Open(t, Migrations())
	first := Define("site.title", 1, contract.StringJSON[string](), Presentation{Group: "site", Label: "Title"})
	m, err := New(f.Store, first.Registration())
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Ensure(t.Context(), m, "Home"); err != nil {
		t.Fatal(err)
	}
	relabeled := Define("site.title", 1, contract.StringJSON[string](), Presentation{Group: "site", Label: "Site title", Order: 4})
	m2, err := New(f.Store, relabeled.Registration())
	if err != nil {
		t.Fatal(err)
	}
	if err := relabeled.Ensure(t.Context(), m2, "ignored"); err != nil {
		t.Fatal(err)
	}
	found, err := relabeled.Find(t.Context(), m2)
	record, _ := found.Get()
	if err != nil || record.Presentation().Label != "Site title" || record.Presentation().Order != 4 || record.Configured() {
		t.Fatal("Ensure did not refresh a declared presentation", err)
	}
	if got, _ := relabeled.GetOr(t.Context(), m2, ""); got != "Home" {
		t.Fatal("Ensure replaced a value", got)
	}
	if err := relabeled.Configure(t.Context(), m2, Presentation{Group: "custom", Label: "Custom"}); err != nil {
		t.Fatal(err)
	}
	again := Define("site.title", 1, contract.StringJSON[string](), Presentation{Group: "site", Label: "Declared again"})
	m3, err := New(f.Store, again.Registration())
	if err != nil {
		t.Fatal(err)
	}
	if err := again.Ensure(t.Context(), m3, "ignored"); err != nil {
		t.Fatal(err)
	}
	if report, err := Reconcile(t.Context(), m3); err != nil || len(report.Presented) != 0 {
		t.Fatal("reconcile replaced a configured presentation", report, err)
	}
	found, err = again.Find(t.Context(), m3)
	record, _ = found.Get()
	if err != nil || record.Presentation().Label != "Custom" || !record.Configured() {
		t.Fatal("configured presentation replaced", err)
	}
	if err := again.ResetPresentation(t.Context(), m3); err != nil {
		t.Fatal(err)
	}
	found, err = again.Find(t.Context(), m3)
	record, _ = found.Get()
	if err != nil || record.Presentation().Label != "Declared again" || record.Configured() {
		t.Fatal("reset presentation", err)
	}
	// Rows written before presentation ownership keep their stored presentation.
	execute(t, f, `INSERT INTO foundry_settings (name,version,value,kind,parameters,group_name,label,description,sort_order,is_public,created_at,updated_at) VALUES ('site.legacy',1,'"x"','text','{}','legacy','Legacy','',0,false,now(),now())`)
	legacy := Define("site.legacy", 1, contract.StringJSON[string](), Presentation{Group: "site", Label: "New"})
	m4, err := New(f.Store, legacy.Registration())
	if err != nil {
		t.Fatal(err)
	}
	if report, err := Reconcile(t.Context(), m4); err != nil || len(report.Presented) != 0 {
		t.Fatal("pre-ownership presentation replaced", report, err)
	}
}

func TestPostgresSettingsListPagesInsteadOfFailingAndLoadsBatches(t *testing.T) {
	f := extensiontest.Open(t, Migrations())
	large := strings.Repeat("x", MaxValueBytes-1024)
	var registrations []Registration
	var keys []Key[string]
	for i := range 20 {
		key := Define(Name("bulk.item"+strconv.Itoa(10+i)), 1, contract.StringJSON[string](), Presentation{Group: "bulk", Order: int32(i % 3)})
		keys = append(keys, key)
		registrations = append(registrations, key.Registration())
	}
	site := Define("site.name", 1, contract.StringJSON[string](), Presentation{Group: "site"})
	siteCached := Define("site.motto", 1, contract.StringJSON[string](), Presentation{Group: "site"})
	missing := Define("site.missing", 1, contract.StringJSON[string](), Presentation{Group: "site"})
	registrations = append(registrations, site.Registration(), siteCached.RegistrationWith(Options[string]{Cache: time.Minute}), missing.Registration())
	m, err := New(f.Store, registrations...)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range keys {
		if err := key.Ensure(t.Context(), m, large); err != nil {
			t.Fatal(err)
		}
	}
	if err := site.Ensure(t.Context(), m, "Foundry"); err != nil {
		t.Fatal(err)
	}
	if err := siteCached.Ensure(t.Context(), m, "Typed"); err != nil {
		t.Fatal(err)
	}
	all, err := List(t.Context(), m, Filter{Group: "bulk"})
	if err != nil || len(all) != 20 {
		t.Fatal("list above the page byte budget failed", len(all), err)
	}
	page, err := ListPage(t.Context(), m, Filter{Group: "bulk"}, Cursor{}, 0)
	if err != nil || len(page.Records) == 0 || len(page.Records) >= 20 || page.Next.IsZero() {
		t.Fatal("a page must end early at its byte budget", len(page.Records), err)
	}
	var names []Name
	var cursor Cursor
	for {
		page, err := ListPage(t.Context(), m, Filter{}, cursor, 3)
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Records) > 3 {
			t.Fatal("page limit")
		}
		for _, r := range page.Records {
			names = append(names, r.Name())
		}
		if page.Next.IsZero() {
			break
		}
		cursor = page.Next
	}
	if len(names) != 22 {
		t.Fatal("paged listing", len(names))
	}
	for i, r := range all {
		if names[i] != r.Name() {
			t.Fatal("pages and List disagree on order", i)
		}
	}
	for _, limit := range []int{-1, MaxKeys + 1} {
		if _, err := ListPage(t.Context(), m, Filter{}, Cursor{}, limit); err == nil {
			t.Fatal("invalid page size accepted", limit)
		}
	}
	if _, err := ListPage(t.Context(), m, Filter{}, Cursor{Name: "bad name", Group: "site"}, 1); err == nil {
		t.Fatal("invalid cursor accepted")
	}
	f.Queries.Store(0)
	groups, err := Groups(t.Context(), m)
	if err != nil || !slices.Equal(groups, []Group{"bulk", "site"}) || f.Queries.Load() != 1 {
		t.Fatal("groups", groups, err)
	}
	f.Queries.Store(0)
	batch, err := LoadGroup(t.Context(), m, "site")
	if err != nil || f.Queries.Load() != 1 {
		t.Fatal("group load", err, f.Queries.Load())
	}
	if got, err := site.FromOr(t.Context(), batch, ""); err != nil || got != "Foundry" {
		t.Fatal("batch value", got, err)
	}
	if got, err := missing.FromOr(t.Context(), batch, "default"); err != nil || got != "default" {
		t.Fatal("batch fallback", got, err)
	}
	if _, err := keys[0].From(t.Context(), batch); err == nil {
		t.Fatal("unselected key read from a batch")
	}
	f.Queries.Store(0)
	if _, err := Load(t.Context(), m, siteCached); err != nil {
		t.Fatal(err)
	}
	if f.Queries.Load() != 0 {
		t.Fatal("cached key reloaded after a group load")
	}
	batch, err = Load(t.Context(), m, site, siteCached, keys[0], site)
	if err != nil || f.Queries.Load() != 1 {
		t.Fatal("multi-key load", err, f.Queries.Load())
	}
	if got, err := keys[0].FromOr(t.Context(), batch, ""); err != nil || got != large {
		t.Fatal("multi-key value", err)
	}
	shadow := Define("site.name", 1, contract.StringJSON[string](), Presentation{})
	if _, err := Load(t.Context(), m, shadow); err == nil {
		t.Fatal("lookalike declaration loaded")
	}
	if _, err := shadow.From(t.Context(), batch); err == nil {
		t.Fatal("lookalike declaration decoded from a batch")
	}
}
