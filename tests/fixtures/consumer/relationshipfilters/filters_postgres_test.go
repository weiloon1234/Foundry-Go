package relationshipfilters_test

import (
	"context"
	"errors"
	"testing"

	"foundry.test/consumer/internal/queryfixture"
	"foundry.test/consumer/models"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/fault"
)

func TestPostgresRelationshipFilters(t *testing.T) {
	queryfixture.Run(t, func(tx *database.Tx, users []models.User) error {
		u, o := models.Users, models.Orders
		orders := models.UserLinks.Orders.Where(orm.NumberGt(o.TotalCents, 1)).OrderBy(orm.NumberDesc(o.TotalCents))
		counter := &queryCounter{Executor: tx}
		builder := models.Users.Query().Where(orm.Has(orders))
		selected, err := orm.All(t.Context(), counter, builder.OrderBy(orm.TextAsc(u.Email)))
		if err != nil {
			return err
		}
		if len(selected) != 2 || counter.calls != 1 || selected[0].Orders.IsLoaded() {
			t.Fatal("existence did not filter parents in one query without loading children")
		}
		// The typed model query remains available after relationship filtering.
		if user, err := orm.Require(t.Context(), tx, builder.Where(orm.KeyEq(models.Users.ID, users[1].ID))); err != nil || user.ID != users[1].ID {
			t.Error("relationship filter lost typed Find", err)
		}
		for _, test := range []struct {
			predicate orm.Predicate[models.User]
			want      int64
		}{
			{orm.Has(orders), 2},
			{orm.Not(orm.Has(orders)), 1},
			{orm.Has(models.UserLinks.SingleOrder), 2}, // existence does not hydrate a singular slot
			{orm.Has(models.UserLinks.Introducer), 1},
			{orm.Not(orm.Has(models.UserLinks.Introducer)), 2},
			{orm.Has(models.UserLinks.Referrals.Where(orm.Has(orders))), 1},
			{orm.Or(orm.Has(models.UserLinks.Orders.Where(orm.NumberGt(o.TotalCents, 2))), orm.KeyEq(u.ID, users[2].ID)), 2},
		} {
			if n, err := orm.Count(t.Context(), tx, models.Users.Query().Where(test.predicate)); err != nil || n != test.want {
				t.Error("relationship predicate returned wrong parent count", n, test.want, err)
			}
		}
		if n, err := orm.Count(t.Context(), tx, models.Users.Query().Where(orm.Not(orm.Has(orders)))); err != nil || n != 1 {
			t.Error("WhereDoesntHave mismatch", err)
		}
		if n, err := orm.Count(t.Context(), tx, models.Orders.Query().Where(orm.Has(models.OrderLinks.Buyer))); err != nil || n != 3 {
			t.Error("inverse relation mismatch", err)
		}
		// Reuse the descriptor for eager loading; the filter query does not alter it.
		counter.calls = 0
		loaded, err := orm.All(t.Context(), counter, builder.With(orders).OrderBy(orm.TextAsc(u.Email)))
		if err != nil {
			return err
		}
		if len(loaded) != 2 || counter.calls != 2 {
			t.Fatal("filter plus eager loading did not use one parent and one child query")
		}
		if children, ok := loaded[0].Orders.Get(); !ok || len(children) != 1 || children[0].TotalCents != 2 {
			t.Error("reused descriptor lost child filter")
		}
		if updated, err := orm.Update(t.Context(), tx, builder, models.Users.Key(), users[0].ID, orm.NumberSet(models.Users.Age, 31)); err != nil || updated.Age != 31 {
			t.Error("relation-scoped update failed", err)
		}
		if _, err := orm.Update(t.Context(), tx, builder, models.Users.Key(), users[2].ID, orm.NumberSet(models.Users.Age, 31)); !errors.Is(err, database.NotFound) {
			t.Error("relation scope failed to constrain update", err)
		}
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		if _, err := orm.All(ctx, queryfixture.NoQueries(t), builder); !errors.Is(err, context.Canceled) {
			t.Error("canceled filter reached executor", err)
		}
		if _, err := orm.All(t.Context(), queryfixture.NoQueries(t), models.Users.Query().Where(orm.Has(models.UserLinks.Introducer.Where(orm.EnumEq(u.Status, models.Status("bad")))))); !errors.Is(err, fault.Invalid) {
			t.Error("invalid related enum reached executor", err)
		}
		if err := checkThroughFilters(t, tx, users); err != nil {
			return err
		}
		return checkNestedSelfFilters(t, tx, users)
	})
}

func checkThroughFilters(t *testing.T, tx *database.Tx, users []models.User) error {
	if err := queryfixture.CreateFriendshipTable(t.Context(), tx); err != nil {
		return err
	}
	for _, edge := range [][2]int{{0, 1}, {0, 1}, {1, 2}} {
		if _, err := orm.Insert(t.Context(), tx, models.Friendships.Table, orm.ColumnSet(models.Friendships.FromID, users[edge[0]].ID), orm.ColumnSet(models.Friendships.ToID, users[edge[1]].ID), orm.TextSet(models.Friendships.Note, "active")); err != nil {
			return err
		}
	}
	u := models.Users
	grandchildren := models.UserLinks.Friends.Where(orm.Has(models.UserLinks.Friends.Where(orm.KeyEq(u.ID, users[2].ID))))
	selected, err := orm.All(t.Context(), tx, models.Users.Query().Where(orm.Has(grandchildren)))
	if err != nil {
		return err
	}
	if len(selected) != 1 || selected[0].ID != users[0].ID {
		t.Error("nested self-pivot relation lost aliases or multiplied parents")
	}
	if n, err := orm.Count(t.Context(), tx, models.Users.Query().Where(orm.Has(models.UserLinks.Friends.WherePivot(orm.TextEq(models.Friendships.Note, "inactive"))))); err != nil || n != 0 {
		t.Error("pivot filter was lost", err)
	}
	// Natural target keys, nullable pivot keys, duplicate links and absent targets.
	for _, sql := range []string{
		`CREATE TABLE groups (code text PRIMARY KEY,name text NOT NULL,owner_id uuid NOT NULL)`,
		`CREATE TABLE memberships (id text PRIMARY KEY,user_id uuid NOT NULL,group_code text,lookup text NOT NULL,priority bigint NOT NULL,level smallint NOT NULL,inviter_id uuid)`,
	} {
		if _, err := tx.Exec(t.Context(), sql); err != nil {
			return err
		}
	}
	for i, code := range []models.GroupCode{"main", "other"} {
		if _, err := orm.Insert(t.Context(), tx, models.Groups.Table, orm.KeySet(models.Groups.Code, code), orm.TextSet(models.Groups.Name, string(code)), orm.ColumnSet(models.Groups.OwnerID, users[i].ID)); err != nil {
			return err
		}
	}
	for _, draft := range [][]orm.Assignment[models.Membership]{
		{orm.KeySet(models.Memberships.ID, "a"), orm.ColumnSet(models.Memberships.UserID, users[0].ID), orm.NullableTextSet(models.Memberships.GroupCode, "main"), orm.NumberSet(models.Memberships.Priority, 2), orm.NullableColumnSet(models.Memberships.InviterID, users[1].ID)},
		{orm.KeySet(models.Memberships.ID, "b"), orm.ColumnSet(models.Memberships.UserID, users[0].ID), orm.NullableTextSet(models.Memberships.GroupCode, "main"), orm.NumberSet(models.Memberships.Priority, 3), orm.NullableColumnSet(models.Memberships.InviterID, users[0].ID)},
		{orm.KeySet(models.Memberships.ID, "c"), orm.ColumnSet(models.Memberships.UserID, users[1].ID), orm.NullableTextSet(models.Memberships.GroupCode, "other"), orm.NumberSet(models.Memberships.Priority, 0)},
		{orm.KeySet(models.Memberships.ID, "d"), orm.ColumnSet(models.Memberships.UserID, users[2].ID), orm.NullableTextSetNull(models.Memberships.GroupCode), orm.NumberSet(models.Memberships.Priority, 5), orm.NullableColumnSet(models.Memberships.InviterID, users[0].ID)},
		{orm.KeySet(models.Memberships.ID, "e"), orm.ColumnSet(models.Memberships.UserID, users[2].ID), orm.NullableTextSet(models.Memberships.GroupCode, "missing"), orm.NumberSet(models.Memberships.Priority, 5), orm.NullableColumnSet(models.Memberships.InviterID, users[0].ID)},
	} {
		if _, err := orm.Insert(t.Context(), tx, models.Memberships.Table, append(draft, orm.TextSet(models.Memberships.Lookup, "filter"), orm.EnumSet(models.Memberships.Level, models.LevelBasic))...); err != nil {
			return err
		}
	}
	groups := models.UserLinks.Groups.Where(orm.KeyEq(models.Groups.Code, "main")).WherePivot(orm.NumberGte(models.Memberships.Priority, 2))
	for _, test := range []struct {
		relation orm.ExistenceRelation[models.User, models.Group]
		want     int64
	}{
		{models.UserLinks.Groups, 2}, {groups, 1},
		{groups.Where(orm.Has(models.GroupLinks.Owner.Where(orm.Has(models.UserLinks.Orders)))), 1},
		{models.UserLinks.Groups.WherePivot(orm.Has(models.MembershipLinks.Inviter.Where(orm.KeyEq(u.ID, users[0].ID)))), 1},
	} {
		if n, err := orm.Count(t.Context(), tx, models.Users.Query().Where(orm.Has(test.relation))); err != nil || n != test.want {
			t.Error("natural-key or nested target/pivot filter mismatch", n, test.want, err)
		}
	}
	if n, err := orm.Count(t.Context(), tx, models.Users.Query().Where(orm.Not(orm.Has(models.UserLinks.Groups)))); err != nil || n != 1 {
		t.Error("NULL/missing pivot targets became matches", err)
	}
	return nil
}

func checkNestedSelfFilters(t *testing.T, tx *database.Tx, users []models.User) error {
	parent := users[1]
	for _, email := range []string{"d@example.test", "e@example.test", "f@example.test"} {
		created, err := orm.Insert(t.Context(), tx, models.Users.Table, orm.TextSet(models.Users.Email, email), orm.NumberSet(models.Users.Age, 18), orm.EnumSet(models.Users.Status, models.StatusActive), orm.EnumSet(models.Users.Level, models.LevelBasic), orm.NullableColumnSet(models.Users.IntroducerID, parent.ID))
		if err != nil {
			return err
		}
		parent = created
	}
	branch := models.UserLinks.Referrals.Where(orm.TextEq(models.Users.Email, parent.Email))
	for range 3 {
		branch = models.UserLinks.Referrals.Where(orm.Has(branch))
	}
	selected, err := orm.All(t.Context(), tx, models.Users.Query().Where(orm.Has(branch)))
	if err != nil {
		return err
	}
	if len(selected) != 1 || selected[0].ID != users[0].ID {
		t.Error("four-level self relationship failed")
	}
	return nil
}

type queryCounter struct {
	database.Executor
	calls int
}

func (e *queryCounter) Query(ctx context.Context, sql string, args ...any) (*database.Rows, error) {
	e.calls++
	return e.Executor.Query(ctx, sql, args...)
}
