package relationshipfilters_test

import (
	"context"
	"errors"
	"testing"

	"foundry.test/consumer/internal/queryfixture"
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/fault"
)

func TestPostgresRelationshipFilters(t *testing.T) {
	queryfixture.Run(t, func(tx *database.Tx, users []models.User) error {
		u, o := models.UserFields(), models.OrderFields()
		orders := models.UserRelations().Orders.Where(o.TotalCents.Gt(1)).OrderBy(o.TotalCents.Desc())
		counter := &queryCounter{Executor: tx}
		builder := models.QueryUsers().WhereHas(orders)
		selected, err := builder.OrderBy(u.Email.Asc()).All(t.Context(), counter)
		if err != nil {
			return err
		}
		if len(selected) != 2 || counter.calls != 1 || selected[0].Orders.IsLoaded() {
			t.Fatal("existence did not filter parents in one query without loading children")
		}
		// The generated wrapper remains available after relationship filtering.
		if user, err := builder.RequireFind(t.Context(), tx, users[1].ID); err != nil || user.ID != users[1].ID {
			t.Error("relationship filter lost typed Find", err)
		}
		for _, test := range []struct {
			predicate query.Predicate[models.User]
			want      int64
		}{
			{orders.Exists(), 2},
			{orders.Exists().Not(), 1},
			{models.UserRelations().SingleOrder.Exists(), 2}, // existence does not hydrate a singular slot
			{models.UserRelations().Introducer.Exists(), 1},
			{models.UserRelations().Introducer.Exists().Not(), 2},
			{models.UserRelations().Referrals.Where(orders.Exists()).Exists(), 1},
			{query.Or(models.UserRelations().Orders.Where(o.TotalCents.Gt(2)).Exists(), u.ID.Eq(users[2].ID)), 2},
		} {
			if n, err := models.QueryUsers().Where(test.predicate).Count(t.Context(), tx); err != nil || n != test.want {
				t.Error("relationship predicate returned wrong parent count", n, test.want, err)
			}
		}
		if n, err := models.QueryUsers().WhereDoesntHave(orders).Count(t.Context(), tx); err != nil || n != 1 {
			t.Error("WhereDoesntHave mismatch", err)
		}
		if n, err := models.QueryOrders().WhereHas(models.OrderRelations().Buyer).Count(t.Context(), tx); err != nil || n != 3 {
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
		if updated, err := builder.Update(t.Context(), tx, users[0].ID, models.UserDraft{}.SetAge(31)); err != nil || updated.Age != 31 {
			t.Error("relation-scoped update failed", err)
		}
		if _, err := builder.Update(t.Context(), tx, users[2].ID, models.UserDraft{}.SetAge(31)); !errors.Is(err, database.NotFound) {
			t.Error("relation scope failed to constrain update", err)
		}
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		if _, err := builder.All(ctx, queryfixture.NoQueries(t)); !errors.Is(err, context.Canceled) {
			t.Error("canceled filter reached executor", err)
		}
		if _, err := models.QueryUsers().WhereHas(models.UserRelations().Introducer.Where(u.Status.Eq(models.Status("bad")))).All(t.Context(), queryfixture.NoQueries(t)); !errors.Is(err, fault.Invalid) {
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
		if _, err := models.QueryFriendships().Create(t.Context(), tx, models.FriendshipDraft{}.SetFromID(users[edge[0]].ID).SetToID(users[edge[1]].ID).SetNote("active")); err != nil {
			return err
		}
	}
	u := models.UserFields()
	grandchildren := models.UserRelations().Friends.Where(models.UserRelations().Friends.Where(u.ID.Eq(users[2].ID)).Exists())
	selected, err := models.QueryUsers().WhereHas(grandchildren).All(t.Context(), tx)
	if err != nil {
		return err
	}
	if len(selected) != 1 || selected[0].ID != users[0].ID {
		t.Error("nested self-pivot relation lost aliases or multiplied parents")
	}
	if n, err := models.QueryUsers().WhereHas(models.UserRelations().Friends.WherePivot(models.FriendshipFields().Note.Eq("inactive"))).Count(t.Context(), tx); err != nil || n != 0 {
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
		if _, err := models.QueryGroups().Create(t.Context(), tx, models.GroupDraft{}.SetCode(code).SetName(string(code)).SetOwnerID(users[i].ID)); err != nil {
			return err
		}
	}
	for _, draft := range []models.MembershipDraft{
		models.MembershipDraft{}.SetID("a").SetUserID(users[0].ID).SetGroupCode("main").SetPriority(2).SetInviterID(users[1].ID),
		models.MembershipDraft{}.SetID("b").SetUserID(users[0].ID).SetGroupCode("main").SetPriority(3).SetInviterID(users[0].ID),
		models.MembershipDraft{}.SetID("c").SetUserID(users[1].ID).SetGroupCode("other").SetPriority(0),
		models.MembershipDraft{}.SetID("d").SetUserID(users[2].ID).ClearGroupCode().SetPriority(5).SetInviterID(users[0].ID),
		models.MembershipDraft{}.SetID("e").SetUserID(users[2].ID).SetGroupCode("missing").SetPriority(5).SetInviterID(users[0].ID),
	} {
		if _, err := models.QueryMemberships().Create(t.Context(), tx, draft.SetLookup("filter").SetLevel(models.LevelBasic)); err != nil {
			return err
		}
	}
	groups := models.UserRelations().Groups.Where(models.GroupFields().Code.Eq("main")).WherePivot(models.MembershipFields().Priority.Gte(2))
	for _, test := range []struct {
		relation query.ExistenceRelation[models.User]
		want     int64
	}{
		{models.UserRelations().Groups, 2}, {groups, 1},
		{groups.Where(models.GroupRelations().Owner.Where(models.UserRelations().Orders.Exists()).Exists()), 1},
		{models.UserRelations().Groups.WherePivot(models.MembershipRelations().Inviter.Where(u.ID.Eq(users[0].ID)).Exists()), 1},
	} {
		if n, err := models.QueryUsers().WhereHas(test.relation).Count(t.Context(), tx); err != nil || n != test.want {
			t.Error("natural-key or nested target/pivot filter mismatch", n, test.want, err)
		}
	}
	if n, err := models.QueryUsers().WhereDoesntHave(models.UserRelations().Groups).Count(t.Context(), tx); err != nil || n != 1 {
		t.Error("NULL/missing pivot targets became matches", err)
	}
	return nil
}

func checkNestedSelfFilters(t *testing.T, tx *database.Tx, users []models.User) error {
	parent := users[1]
	for _, email := range []string{"d@example.test", "e@example.test", "f@example.test"} {
		created, err := models.QueryUsers().Create(t.Context(), tx, models.UserDraft{}.SetEmail(email).SetAge(18).SetStatus(models.StatusActive).SetLevel(models.LevelBasic).SetIntroducerID(parent.ID))
		if err != nil {
			return err
		}
		parent = created
	}
	branch := models.UserRelations().Referrals.Where(models.UserFields().Email.Eq(parent.Email))
	for range 3 {
		branch = models.UserRelations().Referrals.Where(branch.Exists())
	}
	selected, err := models.QueryUsers().WhereHas(branch).All(t.Context(), tx)
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
