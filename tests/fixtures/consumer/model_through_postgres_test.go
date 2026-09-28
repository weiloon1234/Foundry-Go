package consumer_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"foundry.test/consumer/internal/queryfixture"
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/database/relation"
	"github.com/weiloon1234/Foundry-Go/fault"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
)

func TestPostgresGeneratedManyToMany(t *testing.T) {
	db := pgtest.Open(t)
	namespace := pgtest.Namespace(t, db)
	err := db.Transaction(t.Context(), func(tx *database.Tx) error {
		for _, sql := range []string{
			`SET LOCAL search_path TO "` + namespace + `"`,
			`CREATE TABLE users (id uuid PRIMARY KEY, email_address text NOT NULL, age bigint NOT NULL, nickname text, status text NOT NULL, level smallint NOT NULL, birthday date, introducer_id uuid)`,
			`CREATE TABLE groups (code text PRIMARY KEY, name text NOT NULL, owner_id uuid NOT NULL)`,
			`CREATE TABLE memberships (id text PRIMARY KEY, user_id uuid NOT NULL, group_code text, lookup text NOT NULL, priority bigint NOT NULL, level smallint NOT NULL, inviter_id uuid)`,
			`CREATE TABLE friendships (id uuid PRIMARY KEY, from_id uuid NOT NULL, to_id uuid NOT NULL, note text NOT NULL)`,
		} {
			if _, err := tx.Exec(t.Context(), sql); err != nil {
				return err
			}
		}
		var users []models.User
		for i := range 4 {
			draft := models.UserDraft{}.SetEmail("through-fixture").SetAge(20 + i).SetStatus(models.StatusActive).SetLevel(models.LevelBasic)
			if i == 1 {
				draft = draft.SetStatus(models.StatusDisabled)
			}
			user, err := models.QueryUsers().Create(t.Context(), tx, draft)
			if err != nil {
				return err
			}
			users = append(users, user)
		}
		for i, code := range []models.GroupCode{"a", "b", "c"} {
			name := "same"
			if i == 2 {
				name = "third"
			}
			if _, err := models.QueryGroups().Create(t.Context(), tx, models.GroupDraft{}.SetCode(code).SetName(name).SetOwnerID(users[i].ID)); err != nil {
				return err
			}
		}
		for _, edge := range []struct {
			id       string
			owner    int
			group    models.GroupCode
			priority int
		}{
			{"a", 0, "a", 2}, {"b", 0, "b", 1}, {"c", 0, "a", 1}, {"d", 1, "a", 3}, {"e", 2, "c", 1},
			{"null", 0, "", 1}, {"orphan", 0, "missing", 1},
		} {
			draft := models.MembershipDraft{}.SetID(edge.id).SetUserID(users[edge.owner].ID).SetLookup("same").SetPriority(edge.priority).SetLevel(models.LevelBasic).SetInviterID(users[1].ID)
			if edge.group != "" {
				draft = draft.SetGroupCode(edge.group)
			}
			if _, err := models.QueryMemberships().Create(t.Context(), tx, draft); err != nil {
				return err
			}
		}
		counter := &queryfixture.QueryCounter{Executor: tx}
		limits := query.DefaultRelationLimits()
		limits.BatchSize = 2
		groups := models.UserRelations().Groups.OrderByPivot(models.MembershipFields().Priority.Asc()).OrderBy(models.GroupFields().Code.Desc())
		base := models.QueryUsers().OrderBy(models.UserFields().Age.Asc()).WithRelationLimits(limits)
		loaded, err := base.With(groups).All(t.Context(), counter)
		if err != nil {
			return err
		}
		if counter.Queries.Load() != 3 || len(loaded) != 4 {
			t.Error("many-to-many did not batch distinct source keys")
		}
		if !reflect.DeepEqual(membershipIDs(loaded[0]), []string{"b", "c", "a"}) || !reflect.DeepEqual(membershipIDs(loaded[1]), []string{"d"}) || !reflect.DeepEqual(membershipIDs(loaded[2]), []string{"e"}) {
			t.Error("pivot ordering, duplicate edges or parent association incorrect")
		}
		if links, ok := loaded[3].Groups.Get(); !ok || len(links) != 0 {
			t.Error("missing links were not marked loaded-empty")
		}
		for _, user := range users {
			if user.Groups.IsLoaded() {
				t.Error("load mutated supplied users")
			}
		}
		links, _ := loaded[0].Groups.Get()
		pivotCode, pivotPresent := links[0].Pivot.GroupCode.Get()
		if links[0].Model.Code != "b" || links[1].Model.Code != "a" || links[2].Model.Code != "a" || !pivotPresent || pivotCode != "b" {
			t.Error("target or nullable pivot hydration incorrect")
		}
		counter.Queries.Store(0)
		if _, err := base.With(groups).LoadMissing(t.Context(), counter, loaded); err != nil {
			return err
		}
		if counter.Queries.Load() != 0 {
			t.Error("LoadMissing repeated loaded many-to-many queries")
		}
		counter.Queries.Store(0)
		duplicates, err := base.With(groups).Load(t.Context(), counter, []models.User{users[0], users[0]})
		if err != nil {
			return err
		}
		if counter.Queries.Load() != 1 || len(membershipIDs(duplicates[1])) != 3 {
			t.Error("duplicate source keys were not shared")
		}
		one, _ := duplicates[0].Groups.Get()
		one[0].Pivot.Priority = 999
		two, _ := duplicates[1].Groups.Get()
		if two[0].Pivot.Priority == 999 {
			t.Error("duplicate parents share mutable link containers")
		}

		counter.Queries.Store(0)
		nested := groups.With(models.GroupRelations().Owner.Where(models.UserFields().Status.Eq(models.StatusActive))).WithPivot(models.MembershipRelations().Inviter)
		loaded, err = base.With(nested).All(t.Context(), counter)
		if err != nil {
			return err
		}
		if counter.Queries.Load() != 6 {
			t.Errorf("expected root, two join batches, two owner batches and one inviter batch; got %d", counter.Queries.Load())
		}
		links, _ = loaded[0].Groups.Get()
		owner, ownerLoaded := links[0].Model.Owner.Get()
		inviter, inviterLoaded := links[0].Pivot.Inviter.Get()
		inviterModel, inviterPresent := inviter.Get()
		if !ownerLoaded || owner.IsSet() || !inviterLoaded || !inviterPresent || inviterModel.ID != users[1].ID {
			t.Error("target/pivot nested scopes or loaded states incorrect")
		}
		filtered, err := base.With(groups.Where(models.GroupFields().Code.Eq("a")).WherePivot(models.MembershipFields().Priority.Gt(1))).All(t.Context(), counter)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(membershipIDs(filtered[0]), []string{"a"}) || len(membershipIDs(filtered[2])) != 0 {
			t.Error("target and pivot scopes were not both applied")
		}

		for _, link := range []struct{ from, to int }{{0, 1}, {0, 2}, {1, 0}} {
			if _, err := models.QueryFriendships().Create(t.Context(), tx, models.FriendshipDraft{}.SetFromID(users[link.from].ID).SetToID(users[link.to].ID).SetNote("friend")); err != nil {
				return err
			}
		}
		friends := models.UserRelations().Friends.Where(models.UserFields().Status.Eq(models.StatusActive)).OrderBy(models.UserFields().Age.Asc())
		self, err := base.With(friends.With(friends)).All(t.Context(), counter)
		if err != nil {
			return err
		}
		f, ok := self[0].Friends.Get()
		if !ok || len(f) != 1 || f[0].Model.ID != users[2].ID || f[0].Pivot.FromID != users[0].ID || !f[0].Model.Friends.IsLoaded() {
			t.Error("self many-to-many mixed parent/target identity")
		}
		if err := assertThroughFailures(t, tx, counter, base, groups, users); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func membershipIDs(user models.User) []string {
	links, _ := user.Groups.Get()
	ids := make([]string, len(links))
	for i, link := range links {
		ids[i] = link.Pivot.ID
	}
	return ids
}

func assertThroughFailures(t *testing.T, tx *database.Tx, counter *queryfixture.QueryCounter, base models.UserQuery, groups query.ThroughRelation[models.User, models.Group, models.Membership], users []models.User) error {
	t.Helper()
	for _, test := range []struct {
		cap     int
		parents []models.User
	}{{2, users}, {5, []models.User{users[0]}}, {6, []models.User{users[0], users[0]}}} {
		limits := query.DefaultRelationLimits()
		limits.MaxRows = test.cap
		result, err := base.WithRelationLimits(limits).With(groups).Load(t.Context(), counter, test.parents)
		if !errors.Is(err, fault.Invalid) || result != nil {
			t.Error("many-to-many shared row/attachment bound ignored")
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	counter.Queries.Store(0)
	if result, err := base.With(groups).Load(ctx, counter, users); !errors.Is(err, context.Canceled) || result != nil || counter.Queries.Load() != 0 {
		t.Error("canceled many-to-many performed I/O")
	}
	limits := query.DefaultRelationLimits()
	limits.MaxDepth = 1
	if result, err := base.WithRelationLimits(limits).With(groups.WithPivot(models.MembershipRelations().Inviter)).All(t.Context(), counter); !errors.Is(err, fault.Invalid) || result != nil || counter.Queries.Load() != 0 {
		t.Error("nested pivot depth was not checked before root query")
	}
	// A declared alternate key need not be unique in the physical schema. A
	// pivot that joins two targets must fail instead of selecting an arbitrary one.
	ambiguous := query.ManyToMany(models.UserFields().ID, models.MembershipFields().UserID, models.MembershipFields().Lookup, models.GroupFields().Name).
		Bind("Groups", models.QueryUsers().Query, models.QueryGroups().Query, models.QueryMemberships().Query,
			func(m models.User) relation.Through[models.Group, models.Membership] { return m.Groups },
			func(m models.User, loaded relation.Through[models.Group, models.Membership]) models.User {
				m.Groups = loaded
				return m
			}).
		WherePivot(models.MembershipFields().ID.Eq("a"))
	if result, err := base.With(ambiguous).Load(t.Context(), counter, users); !errors.Is(err, database.TooManyRows) || result != nil {
		t.Error("ambiguous pivot join did not reject duplicate target rows")
	}
	// Invalid stored enum data in the second model decoder discards the target
	// and every already-loaded parent branch, and releases the stream.
	if _, err := tx.Exec(t.Context(), `UPDATE memberships SET level = 255 WHERE id = $1`, "a"); err != nil {
		return err
	}
	if result, err := base.With(models.UserRelations().Friends, groups).Load(t.Context(), counter, users); err == nil || result != nil {
		t.Error("failed pivot hydration published partial parents")
	}
	for _, user := range users {
		if user.Groups.IsLoaded() || user.Friends.IsLoaded() {
			t.Error("failed branch mutated caller values")
		}
	}
	if _, err := models.QueryGroups().Count(t.Context(), counter); err != nil {
		return err
	}
	return nil
}
