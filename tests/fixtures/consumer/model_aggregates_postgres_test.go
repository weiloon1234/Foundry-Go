package consumer_test

import (
	"context"
	"errors"
	"math"
	"testing"

	"foundry.test/consumer/internal/queryfixture"
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/database/relation"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/fault"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestPostgresGeneratedRelationAggregates(t *testing.T) {
	db := pgtest.Open(t)
	namespace := pgtest.Namespace(t, db)
	err := db.Transaction(t.Context(), func(tx *database.Tx) error {
		for _, sql := range []string{
			`SET LOCAL search_path TO "` + namespace + `"`,
			`CREATE TABLE users (id uuid PRIMARY KEY, email_address text NOT NULL, age bigint NOT NULL, nickname text, status text NOT NULL, level smallint NOT NULL, birthday date, introducer_id uuid)`,
			`CREATE TABLE orders (id uuid PRIMARY KEY, buyer_id uuid NOT NULL, total_cents bigint NOT NULL)`,
			`CREATE TABLE groups (code text PRIMARY KEY, name text NOT NULL, owner_id uuid NOT NULL)`,
			`CREATE TABLE memberships (id text PRIMARY KEY, user_id uuid NOT NULL, group_code text, lookup text NOT NULL, priority bigint NOT NULL, level smallint NOT NULL, inviter_id uuid)`,
			`CREATE TABLE measurements (id uuid PRIMARY KEY, user_id uuid NOT NULL, amount numeric, score double precision, label text NOT NULL)`,
		} {
			if _, err := tx.Exec(t.Context(), sql); err != nil {
				return err
			}
		}
		var users []models.User
		for i := range 4 {
			u, err := models.QueryUsers().Create(t.Context(), tx, models.UserDraft{}.SetEmail("aggregate-fixture").SetAge(i+20).SetStatus(models.StatusActive).SetLevel(models.LevelBasic))
			if err != nil {
				return err
			}
			users = append(users, u)
		}
		for _, order := range []struct {
			owner  int
			amount int64
		}{{0, math.MaxInt64}, {0, math.MaxInt64}, {1, 1}, {1, 2}} {
			if _, err := models.QueryOrders().Create(t.Context(), tx, models.OrderDraft{}.SetBuyerID(users[order.owner].ID).SetTotalCents(order.amount)); err != nil {
				return err
			}
		}
		for _, code := range []models.GroupCode{"a", "b"} {
			if _, err := models.QueryGroups().Create(t.Context(), tx, models.GroupDraft{}.SetCode(code).SetName("same").SetOwnerID(users[0].ID)); err != nil {
				return err
			}
		}
		for i, code := range []models.GroupCode{"a", "a", "b", "missing", ""} {
			draft := models.MembershipDraft{}.SetID(string(rune('a' + i))).SetUserID(users[0].ID).SetLookup("same").SetPriority(i + 1).SetLevel(models.LevelBasic)
			if code != "" {
				draft = draft.SetGroupCode(code)
			}
			if _, err := models.QueryMemberships().Create(t.Context(), tx, draft); err != nil {
				return err
			}
		}
		for i, amount := range []string{"1.25", "2.50", "", ""} {
			owner := 0
			if i == 3 {
				owner = 1
			}
			draft := models.MeasurementDraft{}.SetUserID(users[owner].ID).SetLabel([]string{"z", "a", "m", "empty"}[i])
			if amount != "" {
				v, err := decimal.Parse(amount)
				if err != nil {
					return err
				}
				draft = draft.SetAmount(v).SetScore(float64(i+1) / 10)
			}
			if _, err := models.QueryMeasurements().Create(t.Context(), tx, draft); err != nil {
				return err
			}
		}
		counter := &queryfixture.QueryCounter{Executor: tx}
		limits := query.DefaultRelationLimits()
		limits.BatchSize = 2
		base := models.QueryUsers().OrderBy(models.UserFields().Age.Asc()).WithRelationLimits(limits)
		aggregates := models.UserAggregates()
		loaded, err := base.With(models.UserAggregates().OrderCount, aggregates.OrderTotal, aggregates.OrderAverage, aggregates.OrderMinimum, aggregates.OrderMaximum).All(t.Context(), counter)
		if err != nil {
			return err
		}
		if len(loaded) != 4 {
			return errors.New("aggregate parent count mismatch")
		}
		if counter.Queries.Load() != 11 {
			t.Errorf("expected root plus two batches for each of five aggregates, got %d", counter.Queries.Load())
		}
		for i, user := range loaded {
			count, ok := user.OrderCount.Get()
			expected := int64(2)
			if i > 1 {
				expected = 0
			}
			if !ok || count != expected || user.Orders.IsLoaded() {
				t.Error("aggregate hydrated related models or lost empty count")
			}
			if users[i].OrderCount.IsLoaded() {
				t.Error("aggregate mutated caller model")
			}
		}
		assertDecimalAggregate(t, loaded[0].OrderTotal, "18446744073709551614")
		assertDecimalAggregate(t, loaded[0].OrderAverage, "9223372036854775807")
		assertDecimalAggregate(t, loaded[1].OrderTotal, "3")
		assertDecimalAggregate(t, loaded[1].OrderAverage, "1.5")
		for _, slot := range []relation.Value[value.Nullable[decimal.Decimal]]{loaded[2].OrderTotal, loaded[3].OrderAverage} {
			v, ok := slot.Get()
			if !ok || !v.IsNull() {
				t.Error("empty numeric aggregate is not loaded SQL NULL")
			}
		}
		minValue, _ := loaded[1].OrderMinimum.Get()
		minimum, _ := minValue.Get()
		maxValue, _ := loaded[1].OrderMaximum.Get()
		maximum, _ := maxValue.Get()
		if minimum != 1 || maximum != 2 {
			t.Error("typed extrema changed values")
		}
		counter.Queries.Store(0)
		if _, err := base.With(aggregates.OrderCount, aggregates.OrderTotal).LoadMissing(t.Context(), counter, loaded); err != nil {
			return err
		}
		if counter.Queries.Load() != 0 {
			t.Error("LoadMissing reloaded computed aggregate values")
		}
		counter.Queries.Store(0)
		dupes, err := base.With(aggregates.OrderCount).Load(t.Context(), counter, []models.User{users[0], users[0]})
		if err != nil {
			return err
		}
		if counter.Queries.Load() != 1 || len(dupes) != 2 {
			t.Error("aggregate source keys were not deduplicated")
		}
		scoped := aggregates.OrderCount.Using(query.Related(models.UserRelations().Orders.Where(models.OrderFields().TotalCents.Gt(1)), query.Count[models.Order]()))
		filtered, err := base.Where(models.UserFields().Age.Lt(22)).With(scoped).All(t.Context(), counter)
		if err != nil {
			return err
		}
		n, _ := filtered[1].OrderCount.Get()
		if len(filtered) != 2 || n != 1 {
			t.Error("parent or related scope ignored")
		}
		orders, err := models.QueryOrders().With(models.OrderRelations().Buyer.With(aggregates.OrderCount)).All(t.Context(), counter)
		if err != nil {
			return err
		}
		for _, order := range orders {
			buyer, _ := order.Buyer.Get()
			u, ok := buyer.Get()
			count, computed := u.OrderCount.Get()
			if !ok || !computed || count != 2 {
				t.Error("nested aggregate did not load")
			}
		}
		loaded, err = base.With(aggregates.GroupCount, aggregates.GroupDistinct, aggregates.GroupPriority, aggregates.HasGroups).All(t.Context(), counter)
		if err != nil {
			return err
		}
		count, _ := loaded[0].GroupCount.Get()
		distinct, _ := loaded[0].GroupDistinct.Get()
		exists, _ := loaded[0].HasGroups.Get()
		if count != 3 || distinct != 2 || !exists || loaded[0].Groups.IsLoaded() {
			t.Error("pivot aggregates lost duplicate edge semantics")
		}
		assertDecimalAggregate(t, loaded[0].GroupPriority, "6")
		if present, computed := loaded[1].HasGroups.Get(); !computed || present {
			t.Error("empty aggregate exists is not false")
		}
		pivotScope := models.UserRelations().Groups.Where(models.GroupFields().Code.Eq("a")).WherePivot(models.MembershipFields().Priority.Gt(1))
		scopedPriority := aggregates.GroupPriority.Using(query.Related(pivotScope.Pivot(), models.MembershipFields().Priority.Sum()))
		one, err := base.With(scopedPriority).Load(t.Context(), counter, users[:1])
		if err != nil {
			return err
		}
		assertDecimalAggregate(t, one[0].GroupPriority, "2")
		loaded, err = base.With(aggregates.MeasurementTotal, aggregates.MeasurementAverage, aggregates.MeasurementCount, aggregates.MeasurementScore, aggregates.FirstLabel).All(t.Context(), counter)
		if err != nil {
			return err
		}
		assertDecimalAggregate(t, loaded[0].MeasurementTotal, "3.75")
		assertDecimalAggregate(t, loaded[0].MeasurementAverage, "1.875")
		v, _ := loaded[0].MeasurementScore.Get()
		score, valid := v.Get()
		if !valid || math.Abs(score-0.15) > 1e-12 {
			t.Error("float average lost declared approximate result")
		}
		for _, user := range loaded[1:] {
			v, computed := user.MeasurementTotal.Get()
			if !computed || !v.IsNull() {
				t.Error("all-null/absent group converted to numeric zero")
			}
		}
		count, _ = loaded[0].MeasurementCount.Get()
		if count != 2 {
			t.Error("field count included SQL NULL")
		}
		label, _ := loaded[0].FirstLabel.Get()
		first, present := label.Get()
		if !present || first != "a" {
			t.Error("text minimum decoded as numeric data")
		}
		if err := assertFilteredThroughAggregates(t, tx, users); err != nil {
			return err
		}
		return assertAggregateFailures(t, tx, counter, base, users)
	})
	if err != nil {
		t.Fatal(err)
	}
}

func assertDecimalAggregate(t *testing.T, slot relation.Value[value.Nullable[decimal.Decimal]], expected string) {
	t.Helper()
	v, loaded := slot.Get()
	d, present := v.Get()
	if !loaded || !present || d.String() != expected {
		t.Errorf("expected computed exact %s, got %v, loaded=%v present=%v", expected, d, loaded, present)
	}
}

func assertFilteredThroughAggregates(t *testing.T, tx *database.Tx, users []models.User) error {
	t.Helper()
	a, r := models.UserAggregates(), models.UserRelations()
	g, p := models.GroupFields(), models.MembershipFields()
	link := query.Correlate(models.QueryGroups(), models.QueryOrders())
	group := models.GroupFieldsAt(query.OuterScope(link, models.QueryGroups().Scope()))
	order := models.OrderFieldsAt(query.InnerScope(link, models.QueryOrders().Scope()))
	ownersWithOrders := link.Where(order.BuyerID.EqColumn(group.OwnerID)).Exists()
	rows, err := models.QueryUsers().With(
		a.GroupCount.Using(query.Related(r.Groups, query.Count[models.Group]().Filter(g.Code.Eq("a")))),
		a.GroupDistinct.Using(query.Related(r.Groups, g.Code.CountDistinct().Filter(ownersWithOrders))),
		a.GroupPriority.Using(query.Related(r.Groups.Pivot(), p.Priority.Sum().Filter(p.Priority.Gt(1)))),
	).Load(t.Context(), tx, users)
	if err != nil {
		return err
	}
	count, loaded := rows[0].GroupCount.Get()
	distinct, distinctLoaded := rows[0].GroupDistinct.Get()
	if !loaded || count != 2 || !distinctLoaded || distinct != 2 {
		t.Fatal("filtered target/correlated aggregate lost alias or duplicate edge semantics")
	}
	assertDecimalAggregate(t, rows[0].GroupPriority, "5")
	// Conditional values must follow target/pivot qualification, including
	// outer fields captured inside a correlated conditional predicate.
	ownerMatch := query.When(order.BuyerID.EqColumn(group.OwnerID), group.Code).Else(group.Code.Param("missing"))
	conditionalOwners := link.Where(ownerMatch.Ne("missing")).Exists()
	rows, err = models.QueryUsers().With(
		a.GroupCount.Using(query.Related(r.Groups, query.Count[models.Group]().Filter(query.When(g.Code.Eq("a"), g.Code).Else(g.Code.Param("other")).Eq("a")))),
		a.GroupDistinct.Using(query.Related(r.Groups, g.Code.CountDistinct().Filter(conditionalOwners))),
		a.GroupPriority.Using(query.Related(r.Groups.Pivot(), p.Priority.Sum().Filter(query.When(p.Priority.Gt(1), p.Priority.Param(1)).Else(p.Priority.Param(0)).Eq(1)))),
	).Load(t.Context(), tx, users)
	if err != nil {
		return err
	}
	count, loaded = rows[0].GroupCount.Get()
	distinct, distinctLoaded = rows[0].GroupDistinct.Get()
	if !loaded || count != 2 || !distinctLoaded || distinct != 2 {
		t.Fatal("conditional target/correlated filter lost qualification")
	}
	assertDecimalAggregate(t, rows[0].GroupPriority, "5")
	return nil
}

func assertAggregateFailures(t *testing.T, tx *database.Tx, counter *queryfixture.QueryCounter, base models.UserQuery, users []models.User) error {
	t.Helper()
	a := models.UserAggregates()
	limits := query.DefaultRelationLimits()
	limits.MaxRows = 1
	if rows, err := base.WithRelationLimits(limits).With(a.OrderCount).Load(t.Context(), counter, users); !errors.Is(err, fault.Invalid) || rows != nil {
		t.Error("aggregate grouped result budget ignored")
	}
	limits.MaxRows = 2
	if rows, err := base.WithRelationLimits(limits).With(a.OrderCount).Load(t.Context(), counter, []models.User{users[0], users[0], users[0]}); !errors.Is(err, fault.Invalid) || rows != nil {
		t.Error("aggregate duplicate-parent attachment budget ignored")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	counter.Queries.Store(0)
	if rows, err := base.With(a.OrderCount).Load(ctx, counter, users); !errors.Is(err, context.Canceled) || rows != nil || counter.Queries.Load() != 0 {
		t.Error("canceled aggregate performed I/O")
	}
	bad := a.OrderCount.Using(query.Related(models.UserRelations().Orders.With(models.OrderRelations().Buyer), query.Count[models.Order]()))
	if rows, err := base.With(bad).All(t.Context(), counter); !errors.Is(err, fault.Invalid) || rows != nil || counter.Queries.Load() != 0 {
		t.Error("aggregate silently discarded requested child loads")
	}
	singular := a.OrderCount.Using(query.Related(models.UserRelations().SingleOrder, query.Count[models.Order]()))
	if rows, err := base.With(singular).Load(t.Context(), counter, users); !errors.Is(err, database.TooManyRows) || rows != nil {
		t.Error("singular aggregate ignored relation cardinality")
	}
	filteredSingular := singular.Using(query.Related(models.UserRelations().SingleOrder, query.Count[models.Order]().Filter(models.OrderFields().TotalCents.Lt(0))))
	if rows, err := base.With(filteredSingular).Load(t.Context(), counter, users); !errors.Is(err, database.TooManyRows) || rows != nil {
		t.Error("empty filtered measure concealed singular cardinality violation")
	}
	ambiguous := query.ManyToMany(models.UserFields().ID, models.MembershipFields().UserID, models.MembershipFields().Lookup, models.GroupFields().Name).
		Bind("Groups", models.QueryUsers().Query, models.QueryGroups().Query, models.QueryMemberships().Query,
			func(m models.User) relation.Through[models.Group, models.Membership] { return m.Groups },
			func(m models.User, r relation.Through[models.Group, models.Membership]) models.User {
				m.Groups = r
				return m
			}).WherePivot(models.MembershipFields().ID.Eq("a"))
	if rows, err := base.With(a.GroupCount.Using(query.Related(ambiguous, query.Count[models.Group]()))).Load(t.Context(), counter, users); !errors.Is(err, database.TooManyRows) || rows != nil {
		t.Error("many-to-many aggregate accepted ambiguous join")
	}
	if rows, err := base.With(a.GroupCount.Using(query.Related(ambiguous, query.Count[models.Group]().Filter(models.GroupFields().Code.Eq("missing"))))).Load(t.Context(), counter, users); !errors.Is(err, database.TooManyRows) || rows != nil {
		t.Error("empty filtered measure concealed ambiguous through join")
	}
	if _, err := tx.Exec(t.Context(), `UPDATE measurements SET score = 'Infinity' WHERE label = $1`, "z"); err != nil {
		return err
	}
	if rows, err := base.With(a.OrderCount, a.MeasurementScore).Load(t.Context(), counter, users); err == nil || rows != nil {
		t.Error("malformed aggregate value published partial parents")
	}
	for _, user := range users {
		if user.OrderCount.IsLoaded() || user.MeasurementScore.IsLoaded() {
			t.Error("failed aggregate mutated caller")
		}
	}
	if _, err := models.QueryUsers().Count(t.Context(), counter); err != nil {
		return err
	}
	return nil
}
