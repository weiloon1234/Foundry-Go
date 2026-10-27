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
		orders := models.UserLinks.Orders.Where(o.TotalCents.Gt(1)).OrderBy(o.TotalCents.Desc())
		counter := &queryCounter{Executor: tx}
		builder := models.Users.Query().Where(orm.Has(orders))
		selected, err := builder.OrderBy(u.Email.Asc()).All(t.Context(), counter)
		if err != nil {
			return err
		}
		if len(selected) != 2 || counter.calls != 1 || selected[0].Orders.IsLoaded() {
			t.Fatal("existence did not filter parents in one query without loading children")
		}
		// The typed model query remains available after relationship filtering.
		if user, err := builder.Where(models.Users.ID.Eq(users[1].ID)).Require(t.Context(), tx); err != nil || user.ID != users[1].ID {
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
			{orm.Or(orm.Has(models.UserLinks.Orders.Where(o.TotalCents.Gt(2))), u.ID.Eq(users[2].ID)), 2},
		} {
			if n, err := models.Users.Query().Where(test.predicate).Count(t.Context(), tx); err != nil || n != test.want {
				t.Error("relationship predicate returned wrong parent count", n, test.want, err)
			}
		}
		if n, err := models.Users.Query().Where(orm.Not(orm.Has(orders))).Count(t.Context(), tx); err != nil || n != 1 {
			t.Error("WhereDoesntHave mismatch", err)
		}
		if n, err := models.Orders.Query().Where(orm.Has(models.OrderLinks.Buyer)).Count(t.Context(), tx); err != nil || n != 3 {
			t.Error("inverse relation mismatch", err)
		}
		// Reuse the descriptor for eager loading; the filter query does not alter it.
		counter.calls = 0
		loaded, err := builder.With(orders).OrderBy(u.Email.Asc()).All(t.Context(), counter)
		if err != nil {
			return err
		}
		if len(loaded) != 2 || counter.calls != 2 {
			t.Fatal("filter plus eager loading did not use one parent and one child query")
		}
		if children, ok := loaded[0].Orders.Get(); !ok || len(children) != 1 || children[0].TotalCents != 2 {
			t.Error("reused descriptor lost child filter")
		}
		if updated, err := orm.Update(t.Context(), tx, builder, models.Users.Key(), users[0].ID, models.Users.Age.Set(31)); err != nil || updated.Age != 31 {
			t.Error("relation-scoped update failed", err)
		}
		if _, err := orm.Update(t.Context(), tx, builder, models.Users.Key(), users[2].ID, models.Users.Age.Set(31)); !errors.Is(err, database.NotFound) {
			t.Error("relation scope failed to constrain update", err)
		}
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		if _, err := builder.All(ctx, queryfixture.NoQueries(t)); !errors.Is(err, context.Canceled) {
			t.Error("canceled filter reached executor", err)
		}
		if _, err := models.Users.Query().Where(orm.Has(models.UserLinks.Introducer.Where(u.Status.Eq(models.Status("bad"))))).All(t.Context(), queryfixture.NoQueries(t)); !errors.Is(err, fault.Invalid) {
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
		if _, err := models.Friendships.Insert(t.Context(), tx, models.Friendships.FromID.Set(users[edge[0]].ID), models.Friendships.ToID.Set(users[edge[1]].ID), models.Friendships.Note.Set("active")); err != nil {
			return err
		}
	}
	u := models.Users
	grandchildren := models.UserLinks.Friends.Where(orm.Has(models.UserLinks.Friends.Where(u.ID.Eq(users[2].ID))))
	selected, err := models.Users.Query().Where(orm.Has(grandchildren)).All(t.Context(), tx)
	if err != nil {
		return err
	}
	if len(selected) != 1 || selected[0].ID != users[0].ID {
		t.Error("nested self-pivot relation lost aliases or multiplied parents")
	}
	if n, err := models.Users.Query().Where(orm.Has(models.UserLinks.Friends.WherePivot(models.Friendships.Note.Eq("inactive")))).Count(t.Context(), tx); err != nil || n != 0 {
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
		if _, err := models.Groups.Insert(t.Context(), tx, models.Groups.Code.Set(code), models.Groups.Name.Set(string(code)), models.Groups.OwnerID.Set(users[i].ID)); err != nil {
			return err
		}
	}
	for _, draft := range [][]orm.Assignment[models.Membership]{
		{models.Memberships.ID.Set("a"), models.Memberships.UserID.Set(users[0].ID), models.Memberships.GroupCode.Set("main"), models.Memberships.Priority.Set(2), models.Memberships.InviterID.Set(users[1].ID)},
		{models.Memberships.ID.Set("b"), models.Memberships.UserID.Set(users[0].ID), models.Memberships.GroupCode.Set("main"), models.Memberships.Priority.Set(3), models.Memberships.InviterID.Set(users[0].ID)},
		{models.Memberships.ID.Set("c"), models.Memberships.UserID.Set(users[1].ID), models.Memberships.GroupCode.Set("other"), models.Memberships.Priority.Set(0)},
		{models.Memberships.ID.Set("d"), models.Memberships.UserID.Set(users[2].ID), models.Memberships.GroupCode.SetNull(), models.Memberships.Priority.Set(5), models.Memberships.InviterID.Set(users[0].ID)},
		{models.Memberships.ID.Set("e"), models.Memberships.UserID.Set(users[2].ID), models.Memberships.GroupCode.Set("missing"), models.Memberships.Priority.Set(5), models.Memberships.InviterID.Set(users[0].ID)},
	} {
		if _, err := models.Memberships.Insert(t.Context(), tx, append(draft, models.Memberships.Lookup.Set("filter"), models.Memberships.Level.Set(models.LevelBasic))...); err != nil {
			return err
		}
	}
	groups := models.UserLinks.Groups.Where(models.Groups.Code.Eq("main")).WherePivot(models.Memberships.Priority.Gte(2))
	for _, test := range []struct {
		relation orm.ExistenceRelation[models.User, models.Group]
		want     int64
	}{
		{models.UserLinks.Groups, 2}, {groups, 1},
		{groups.Where(orm.Has(models.GroupLinks.Owner.Where(orm.Has(models.UserLinks.Orders)))), 1},
		{models.UserLinks.Groups.WherePivot(orm.Has(models.MembershipLinks.Inviter.Where(u.ID.Eq(users[0].ID)))), 1},
	} {
		if n, err := models.Users.Query().Where(orm.Has(test.relation)).Count(t.Context(), tx); err != nil || n != test.want {
			t.Error("natural-key or nested target/pivot filter mismatch", n, test.want, err)
		}
	}
	if n, err := models.Users.Query().Where(orm.Not(orm.Has(models.UserLinks.Groups))).Count(t.Context(), tx); err != nil || n != 1 {
		t.Error("NULL/missing pivot targets became matches", err)
	}
	return nil
}

func checkNestedSelfFilters(t *testing.T, tx *database.Tx, users []models.User) error {
	parent := users[1]
	for _, email := range []string{"d@example.test", "e@example.test", "f@example.test"} {
		created, err := models.Users.Insert(t.Context(), tx, models.Users.Email.Set(email), models.Users.Age.Set(18), models.Users.Status.Set(models.StatusActive), models.Users.Level.Set(models.LevelBasic), models.Users.IntroducerID.Set(parent.ID))
		if err != nil {
			return err
		}
		parent = created
	}
	branch := models.UserLinks.Referrals.Where(models.Users.Email.Eq(parent.Email))
	for range 3 {
		branch = models.UserLinks.Referrals.Where(orm.Has(branch))
	}
	selected, err := models.Users.Query().Where(orm.Has(branch)).All(t.Context(), tx)
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
