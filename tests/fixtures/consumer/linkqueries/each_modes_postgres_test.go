package linkqueries_test

import (
	"errors"
	"testing"

	"foundry.test/consumer/linkqueries"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/testkit"
)

func TestPostgresPerModelAndBulkSemanticsStayExplicit(t *testing.T) {
	trace := &linkqueries.Trace{}
	runLinks(t, func(tx *database.Tx, _ *testkit.Clock) error {
		member, group, err := endpoints(t, tx)
		if err != nil {
			return err
		}
		ctx := linkqueries.WithTrace(t.Context(), trace)
		q := linkqueries.QueryLinkMemberships()
		draft := linkqueries.MembershipDraft{}.SetMemberID(member.ID).SetGroupCode(group.Code).SetRole(" BULK ")
		bulk, err := q.CreateMany(ctx, tx, []linkqueries.MembershipDraft{draft, draft})
		if err != nil {
			return err
		}
		if len(bulk) != 2 || bulk[0].Role != "bulk" || len(trace.Steps) != 0 {
			return errors.New("shared draft preparation changed explicit bulk semantics")
		}
		each, err := q.CreateEach(ctx, tx, []linkqueries.MembershipDraft{draft})
		if err != nil {
			return err
		}
		if len(each) != 1 || each[0].Role != "bulk" || len(trace.Steps) != 8 {
			return errors.New("per-model creation skipped its normal observers or mutator")
		}
		trace.Steps = nil
		trace.VetoDeletion = 2
		if rows, err := q.DeleteEach(ctx, tx, 3); !errors.Is(err, linkqueries.ErrVeto) || rows != nil {
			return errors.New("later model hook did not abort the complete batch")
		}
		if count, err := q.Count(t.Context(), tx); err != nil || count != 3 {
			return errors.New("later model hook retained an earlier deletion")
		}
		return nil
	})
	if len(trace.Committed) != 2 {
		t.Fatal("failed per-model deletion retained after-commit callbacks")
	}
	for _, operation := range trace.Committed {
		if operation != lifecycle.Create {
			t.Fatal("failed deletion published its operation")
		}
	}
}

func TestPostgresPerModelPhysicalDeletionAndNaturalKeys(t *testing.T) {
	runLinks(t, func(tx *database.Tx, _ *testkit.Clock) error {
		member, group, err := endpoints(t, tx)
		if err != nil {
			return err
		}
		other, err := linkqueries.QueryLinkMembers().Create(t.Context(), tx, linkqueries.MemberDraft{}.SetName("other"))
		if err != nil {
			return err
		}
		q := linkqueries.QueryLinkFriendships()
		forward := linkqueries.FriendshipDraft{}.SetFromID(member.ID).SetToID(other.ID)
		reverse := linkqueries.FriendshipDraft{}.SetFromID(other.ID).SetToID(member.ID)
		links, err := q.CreateEach(t.Context(), tx, []linkqueries.FriendshipDraft{forward, reverse})
		if err != nil {
			return err
		}
		if len(links) != 2 {
			return errors.New("per-model creation lost physical-model rows")
		}
		removed, err := q.Where(linkqueries.FriendshipFields().FromID.Eq(member.ID)).DeleteEach(t.Context(), wrappedTransactor{tx}, 2)
		if err != nil {
			return err
		}
		if len(removed) != 1 || removed[0].ID != links[0].ID {
			return errors.New("physical per-model deletion lost its pre-write model or filter")
		}
		if count, err := q.Count(t.Context(), tx); err != nil || count != 1 {
			return errors.New("physical per-model deletion ignored its scope")
		}
		groups := linkqueries.QueryLinkGroups().Where(linkqueries.GroupFields().Code.Eq(group.Code))
		deleted, err := groups.DeleteEach(t.Context(), tx, 1)
		if err != nil {
			return err
		}
		if len(deleted) != 1 || deleted[0].Code != group.Code || deleted[0].DeletedAt.IsNull() {
			return errors.New("natural-key deletion lost typed identity or deletion time")
		}
		restored, err := groups.RestoreEach(t.Context(), tx, 1)
		if err != nil {
			return err
		}
		if len(restored) != 1 || restored[0].Code != group.Code || !restored[0].DeletedAt.IsNull() {
			return errors.New("natural-key restoration lost identity or NULL state")
		}
		return nil
	})
}
