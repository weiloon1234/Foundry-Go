package attachments

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/i18n"
	store "github.com/weiloon1234/Foundry-Go/internal/attachmentstore"
	"github.com/weiloon1234/Foundry-Go/internal/extensiontest"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/storage"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestPostgresAttachmentCollectionsIdentityAndOrdering(t *testing.T) {
	f := openAttachments(t)
	owner := member(t, 1)
	first := addText(t, f, testSingle, owner, "first")
	old := attachmentOf(t, first)
	replacement := addText(t, f, testSingle, owner, "second")
	latest := attachmentOf(t, replacement)
	assertBody(t, f, testSingle, owner, latest, "second")
	if state, err := f.manager.Inspect(t.Context(), first.Operation); err != nil || state.State != Cleaned {
		t.Fatal("old upload not cleaned", err)
	}
	if _, err := f.disk.Stat(t.Context(), old.key, storage.ReadOptions{}); !errors.Is(err, storage.NotFound) {
		t.Fatal("superseded object retained", err)
	}
	if _, err := testSingle.Find(t.Context(), f.manager, member(t, 2), latest.ID()); !errors.Is(err, database.NotFound) {
		t.Fatal("foreign owner attachment escaped", err)
	}
	other, err := testOther.Add(t.Context(), f.manager, extensiontest.Others.Reference(1), uploadText("other"))
	if err != nil || !other.Attachment.IsSet() {
		t.Fatal("owner type collision", err)
	}
	impostor := Define(extensiontest.Members, "avatar", testSingle.definition.policy)
	if _, err := impostor.List(t.Context(), f.manager, owner); err == nil {
		t.Fatal("nominal registration bypass")
	}
	if _, err := New(Dependencies{Store: f.Store, Disks: f.registry}, DefaultConfig(), testSingle.Registration(), testSingle.Registration()); !errors.Is(err, fault.Duplicate) {
		t.Fatal("duplicate collection accepted", err)
	}
	var ids []ID[extensiontest.Member]
	for _, text := range []string{"one", "two", "three"} {
		ids = append(ids, attachmentOf(t, addText(t, f, testMultiple, owner, text)).ID())
	}
	rejected, err := testMultiple.Add(t.Context(), f.manager, owner, uploadText("four"))
	if err == nil || rejected.Publication != Unpublished || journal(t, f, rejected.Operation).State != string(Cleaned) {
		t.Fatal("cardinality failure leaked new object", err)
	}
	if _, err := testMultiple.Reorder(t.Context(), f.manager, owner, ids[:2]); err == nil {
		t.Fatal("partial permutation accepted")
	}
	if _, err := testMultiple.Reorder(t.Context(), f.manager, owner, []ID[extensiontest.Member]{ids[0], ids[0], ids[2]}); err == nil {
		t.Fatal("duplicate permutation accepted")
	}
	ordered, err := testMultiple.Reorder(t.Context(), f.manager, owner, []ID[extensiontest.Member]{ids[2], ids[0], ids[1]})
	if err != nil || len(ordered) != 3 || ordered[0].ID() != ids[2] || ordered[2].Position() != 2 {
		t.Fatal("reorder mismatch", err)
	}
	properties, err := value.ParseJSON[json.RawMessage](`{"sequence":9007199254740993}`)
	if err != nil {
		t.Fatal(err)
	}
	updated, err := testMultiple.SetProperties(t.Context(), f.manager, owner, ids[2], properties)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := updated.Properties()
	if err != nil || string(raw) != `{"sequence":9007199254740993}` {
		t.Fatal("property snapshot", err)
	}
	raw[0] = '!'
	again, _ := updated.Properties()
	if again[0] != '{' {
		t.Fatal("mutable properties leaked")
	}
	changed, err := testMultiple.Detach(t.Context(), f.manager, owner, ids[0])
	if err != nil || changed.Publication != Published || changed.Affected != 1 {
		t.Fatal("detach", err)
	}
	changed, err = testMultiple.Clear(t.Context(), f.manager, owner)
	if err != nil || changed.Affected != 2 {
		t.Fatal("clear", err)
	}
	if files, err := testMultiple.List(t.Context(), f.manager, owner); err != nil || len(files) != 0 {
		t.Fatal("collection not empty", err)
	}
	if result, err := testSingle.Add(t.Context(), f.manager, member(t, 99), uploadText("missing")); !errors.Is(err, database.NotFound) || result.Publication != Unpublished {
		t.Fatal("missing owner accepted", err)
	}
}

type countedLocales struct {
	set   i18n.LocaleSet
	calls atomic.Int64
}

func (c *countedLocales) Snapshot(ctx context.Context) (i18n.LocaleSet, error) {
	c.calls.Add(1)
	return c.set.Snapshot(ctx)
}
func TestPostgresAttachmentLocalizedBatchAndTypedScopes(t *testing.T) {
	f := openAttachments(t)
	owner := member(t, 1)
	second := member(t, 2)
	empty := member(t, 3)
	english := attachmentOf(t, addText(t, f, testLocalized.ForLocale("en"), owner, "English"))
	addText(t, f, testLocalized.ForLocale("fr"), owner, "French")
	addText(t, f, testLocalized.ForLocale("fr"), second, "Second French")
	catalog := &countedLocales{set: f.locales}
	f.manager.locales = catalog
	f.Queries.Store(0)
	batch, err := testLocalized.LoadLocalized(t.Context(), f.manager, []model.Reference[extensiontest.Member, int64]{owner, second, empty})
	if err != nil {
		t.Fatal(err)
	}
	if f.Queries.Load() != 2 || catalog.calls.Load() != 1 {
		t.Fatal("localized batch repeated I/O", f.Queries.Load(), catalog.calls.Load())
	}
	files, err := batch.Get(owner)
	if err != nil {
		t.Fatal(err)
	}
	optional, err := files.Resolve("ms")
	resolved, ok := optional.Get()
	if err != nil || !ok || resolved.Locale() != "en" || resolved.Files()[0].ID() != english.ID() {
		t.Fatal("default fallback", err)
	}
	result := resolved.Files()
	result[0] = Attachment[extensiontest.Member, int64]{}
	if resolved.Files()[0].IsZero() {
		t.Fatal("resolved slice was not copied")
	}
	files, err = batch.Get(second)
	if err != nil {
		t.Fatal(err)
	}
	optional, err = files.Resolve("ms")
	resolved, ok = optional.Get()
	if err != nil || !ok || resolved.Locale() != "fr" {
		t.Fatal("lexical fallback", err)
	}
	files, err = batch.Get(empty)
	if err != nil {
		t.Fatal(err)
	}
	if optional, err := files.Resolve("ms"); err != nil || optional.IsSet() {
		t.Fatal("empty collection fallback", err)
	}
	if _, err := files.Resolve("zh"); err == nil {
		t.Fatal("unsupported locale accepted")
	}
	if f.Queries.Load() != 2 || catalog.calls.Load() != 1 {
		t.Fatal("resolving loaded data performed I/O")
	}
	if _, err := testLocalized.List(t.Context(), f.manager, owner); err == nil {
		t.Fatal("localized operation inferred global locale")
	}
	if _, err := testLocalized.ForLocale("en").LoadLocalized(t.Context(), f.manager, []model.Reference[extensiontest.Member, int64]{owner}); err == nil {
		t.Fatal("bound collection accepted as all-locale descriptor")
	}
	predicate, err := testLocalized.ForLocale("en").Matching(t.Context(), f.manager)
	if err != nil {
		t.Fatal(err)
	}
	var keys []int64
	err = f.Store.Read(t.Context(), func(ctx context.Context, tx *database.Tx) error {
		q := extensiontest.MemberQuery().Where(predicate)
		var err error
		keys, err = query.SelectValue(q, extensiontest.MemberIDField().Value()).All(ctx, tx)
		return err
	})
	if err != nil || len(keys) != 1 || keys[0] != 1 {
		t.Fatal("typed collection scope", err)
	}
	if err := f.Store.Write(t.Context(), func(ctx context.Context, tx *database.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE extension_members SET deleted_at=now() WHERE id=1`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := testLocalized.ForLocale("en").List(t.Context(), f.manager, owner); !errors.Is(err, database.NotFound) {
		t.Fatal("soft-deleted owner visible", err)
	}
	// A previously loaded snapshot remains explicit; fresh reads recheck visibility.
	if _, err := batch.Get(owner); err != nil {
		t.Fatal("snapshot unexpectedly mutated", err)
	}
	if err := f.Store.Write(t.Context(), func(ctx context.Context, tx *database.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE extension_members SET deleted_at=NULL WHERE id=1`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	assertBody(t, f, testLocalized.ForLocale("en"), owner, english, "English")
}

func TestPostgresAttachmentConcurrentReplacementsAcrossManagers(t *testing.T) {
	f := openAttachments(t)
	owner := member(t, 1)
	addText(t, f, testSingle, owner, "initial")
	second, err := New(Dependencies{Store: f.Store, Disks: f.registry}, DefaultConfig(), testSingle.Registration())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := second.Close(context.Background()); err != nil {
			t.Error(err)
		}
	}()
	start := make(chan struct{})
	results := make(chan Result[extensiontest.Member, int64], 2)
	failures := make(chan error, 2)
	var wg sync.WaitGroup
	for i, m := range []*Manager{f.manager, second} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			text := "first concurrent"
			if i == 1 {
				text = "second concurrent"
			}
			result, err := testSingle.Replace(t.Context(), m, owner, uploadText(text))
			results <- result
			failures <- err
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	for result := range results {
		if result.Publication != Published {
			t.Fatal("concurrent replacement lost publication")
		}
	}
	files, err := testSingle.List(t.Context(), f.manager, owner)
	if err != nil || len(files) != 1 {
		t.Fatal("single cardinality lost", err)
	}
	err = f.Store.Read(t.Context(), func(ctx context.Context, tx *database.Tx) error {
		fields := store.FileFields()
		count, err := store.QueryFoundryAttachments().Where(fields.State.Ne(string(Cleaned))).Count(ctx, tx)
		if count != 1 {
			t.Error("unreconciled concurrent objects", count)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	page, err := f.disk.List(t.Context(), storage.ListOptions{Limit: 10})
	if err != nil || len(page.Objects) != 1 {
		t.Fatal("concurrent storage leak", err)
	}
}
