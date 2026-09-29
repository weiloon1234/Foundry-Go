package tenantqueries_test

import (
	"context"
	"slices"
	"testing"
	"time"

	"foundry.test/consumer/tenantqueries"
	"github.com/weiloon1234/Foundry-Go/database"
)

// Two synchronizations of one source serialize on the source row: the second
// waits for the first to commit, then sees its links. Without that lock both
// would read the old links and leave the union of their targets.
func TestConcurrentPivotSynchronizationSerializesPerSource(t *testing.T) {
	db := tenantDB(t)
	ctx := tenantqueries.WithTenant(t.Context(), alpha)
	document := seedDocument(t, db, alpha, "shared", true)
	tags := make([]tenantqueries.Tag, 0, 3)
	for _, code := range []tenantqueries.TagCode{"a", "b", "c"} {
		tag, err := tenantqueries.QueryTenantTags().Create(ctx, db, tenantqueries.TagDraft{}.SetCode(code).SetName(string(code)))
		if err != nil {
			t.Fatal(err)
		}
		tags = append(tags, tag)
	}
	link := tenantqueries.DocumentRelations().Tags
	draft := tenantqueries.DocumentTagDraft{}.SetWeight(1)
	stored := func() []tenantqueries.TagCode {
		t.Helper()
		pivots, err := tenantqueries.QueryTenantDocumentTags().Where(tenantqueries.DocumentTagFields().DocumentID.Eq(document.ID)).All(ctx, db)
		if err != nil {
			t.Fatal(err)
		}
		codes := make([]tenantqueries.TagCode, len(pivots))
		for i, pivot := range pivots {
			codes[i] = pivot.TagCode
		}
		slices.Sort(codes)
		return codes
	}
	// first runs inside a transaction that stays open until released, while
	// second starts and must block on the source lock.
	race := func(first, second func(context.Context, database.Transactor) error) {
		t.Helper()
		started, release := make(chan struct{}), make(chan struct{})
		firstDone, secondDone := make(chan error, 1), make(chan error, 1)
		go func() {
			firstDone <- db.Transaction(ctx, func(tx *database.Tx) error {
				err := first(ctx, tx)
				close(started)
				<-release
				return err
			})
		}()
		<-started
		go func() { secondDone <- second(ctx, db) }()
		select {
		case err := <-secondDone:
			t.Fatal("second write did not wait for the first", err)
		case <-time.After(200 * time.Millisecond):
		}
		close(release)
		if err := <-firstDone; err != nil {
			t.Fatal(err)
		}
		if err := <-secondDone; err != nil {
			t.Fatal(err)
		}
	}
	if _, err := link.Sync(ctx, db, document, tags[:1], draft, nil); err != nil {
		t.Fatal(err)
	}
	race(func(ctx context.Context, w database.Transactor) error {
		_, err := link.Sync(ctx, w, document, tags[1:2], draft, nil)
		return err
	}, func(ctx context.Context, w database.Transactor) error {
		_, err := link.Sync(ctx, w, document, tags[2:3], draft, nil)
		return err
	})
	if got := stored(); !slices.Equal(got, []tenantqueries.TagCode{"c"}) {
		t.Fatal("concurrent syncs did not serialize", got)
	}
	// Two toggles of one target cancel out instead of attaching it twice.
	race(func(ctx context.Context, w database.Transactor) error {
		_, err := link.Toggle(ctx, w, document, tags[:1], draft)
		return err
	}, func(ctx context.Context, w database.Transactor) error {
		_, err := link.Toggle(ctx, w, document, tags[:1], draft)
		return err
	})
	if got := stored(); !slices.Equal(got, []tenantqueries.TagCode{"c"}) {
		t.Fatal("concurrent toggles duplicated a link", got)
	}
}
