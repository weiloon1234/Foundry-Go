package linkqueries_test

import (
	"errors"
	"testing"

	"foundry.test/consumer/linkqueries"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/testkit"
)

func TestPostgresPerModelAndBulkSemanticsStayExplicit(t *testing.T) {
	trace := &linkqueries.Trace{}
	runORMLinks(t, func(tx *database.Tx, _ *testkit.Clock) error {
		member, group, err := ormEndpoints(t, tx)
		if err != nil {
			return err
		}
		ctx := linkqueries.WithTrace(t.Context(), trace)
		q := linkqueries.Memberships.Query()
		draft := []orm.Assignment[linkqueries.Membership]{linkqueries.Memberships.MemberID.Set(member.ID), linkqueries.Memberships.GroupCode.Set(group.Code), linkqueries.Memberships.Role.Set(" BULK ")}
		bulk, err := linkqueries.Memberships.InsertMany(ctx, tx, [][]orm.Assignment[linkqueries.Membership]{draft, draft})
		if err != nil {
			return err
		}
		if len(bulk) != 2 || bulk[0].Role != "bulk" || len(trace.Steps) != 0 {
			return errors.New("shared draft preparation changed explicit bulk semantics")
		}
		each, err := orm.CreateEach(ctx, tx, q, [][]orm.Assignment[linkqueries.Membership]{draft})
		if err != nil {
			return err
		}
		if len(each) != 1 || each[0].Role != "bulk" || len(trace.Steps) != 8 {
			return errors.New("per-model creation skipped its normal observers or mutator")
		}
		trace.Steps = nil
		trace.VetoDeletion = 2
		if rows, err := orm.DeleteEach(ctx, tx, q, 3); !errors.Is(err, linkqueries.ErrVeto) || rows != nil {
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
	runORMLinks(t, func(tx *database.Tx, _ *testkit.Clock) error {
		member, group, err := ormEndpoints(t, tx)
		if err != nil {
			return err
		}
		other, err := linkqueries.Members.Insert(t.Context(), tx, linkqueries.Members.Name.Set("other"))
		if err != nil {
			return err
		}
		q := linkqueries.Friendships.Query()
		forward := []orm.Assignment[linkqueries.Friendship]{linkqueries.Friendships.FromID.Set(member.ID), linkqueries.Friendships.ToID.Set(other.ID)}
		reverse := []orm.Assignment[linkqueries.Friendship]{linkqueries.Friendships.FromID.Set(other.ID), linkqueries.Friendships.ToID.Set(member.ID)}
		links, err := orm.CreateEach(t.Context(), tx, q, [][]orm.Assignment[linkqueries.Friendship]{forward, reverse})
		if err != nil {
			return err
		}
		if len(links) != 2 {
			return errors.New("per-model creation lost physical-model rows")
		}
		removed, err := orm.DeleteEach(t.Context(), wrappedTransactor{tx}, q.Where(linkqueries.Friendships.FromID.Eq(member.ID)), 2)
		if err != nil {
			return err
		}
		if len(removed) != 1 || removed[0].ID != links[0].ID {
			return errors.New("physical per-model deletion lost its pre-write model or filter")
		}
		if count, err := q.Count(t.Context(), tx); err != nil || count != 1 {
			return errors.New("physical per-model deletion ignored its scope")
		}
		groups := linkqueries.Groups.Query().Where(linkqueries.Groups.Key().Eq(group.Code))
		deleted, err := orm.DeleteEach(t.Context(), tx, groups, 1)
		if err != nil {
			return err
		}
		if len(deleted) != 1 || deleted[0].Code != group.Code || deleted[0].DeletedAt.IsNull() {
			return errors.New("natural-key deletion lost typed identity or deletion time")
		}
		restored, err := orm.RestoreEach(t.Context(), tx, groups, linkqueries.Groups.Soft, 1)
		if err != nil {
			return err
		}
		if len(restored) != 1 || restored[0].Code != group.Code || !restored[0].DeletedAt.IsNull() {
			return errors.New("natural-key restoration lost identity or NULL state")
		}
		return nil
	})
}
