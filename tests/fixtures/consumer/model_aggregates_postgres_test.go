package consumer_test

import (
	"context"
	"errors"
	"math"
	"testing"

	"foundry.test/consumer/internal/queryfixture"
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	rowset "github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/database/relation"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/fault"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestPostgresORMRelationAggregates(t *testing.T) {
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
			u, err := models.Users.Insert(t.Context(), tx, models.Users.Email.Set("aggregate-fixture"), models.Users.Age.Set(i+20), models.Users.Status.Set(models.StatusActive), models.Users.Level.Set(models.LevelBasic))
			if err != nil {
				return err
			}
			users = append(users, u)
		}
		for _, order := range []struct {
			owner  int
			amount int64
		}{{0, math.MaxInt64}, {0, math.MaxInt64}, {1, 1}, {1, 2}} {
			if _, err := models.Orders.Insert(t.Context(), tx, models.Orders.BuyerID.Set(users[order.owner].ID), models.Orders.TotalCents.Set(order.amount)); err != nil {
				return err
			}
		}
		for _, code := range []models.GroupCode{"a", "b"} {
			if _, err := models.Groups.Insert(t.Context(), tx, models.Groups.Code.Set(code), models.Groups.Name.Set("same"), models.Groups.OwnerID.Set(users[0].ID)); err != nil {
				return err
			}
		}
		for i, code := range []models.GroupCode{"a", "a", "b", "missing", ""} {
			draft := []orm.Assignment[models.Membership]{models.Memberships.ID.Set(string(rune('a' + i))), models.Memberships.UserID.Set(users[0].ID), models.Memberships.Lookup.Set("same"), models.Memberships.Priority.Set(i + 1), models.Memberships.Level.Set(models.LevelBasic)}
			if code != "" {
				draft = append(draft, models.Memberships.GroupCode.Set(code))
			}
			if _, err := models.Memberships.Insert(t.Context(), tx, draft...); err != nil {
				return err
			}
		}
		for i, amount := range []string{"1.25", "2.50", "", ""} {
			owner := 0
			if i == 3 {
				owner = 1
			}
			draft := []orm.Assignment[models.Measurement]{models.Measurements.UserID.Set(users[owner].ID), models.Measurements.Label.Set([]string{"z", "a", "m", "empty"}[i])}
			if amount != "" {
				v, err := decimal.Parse(amount)
				if err != nil {
					return err
				}
				draft = append(draft, models.Measurements.Amount.Set(v), models.Measurements.Score.Set(float64(i+1)/10))
			}
			if _, err := models.Measurements.Insert(t.Context(), tx, draft...); err != nil {
				return err
			}
		}
		counter := &queryfixture.QueryCounter{Executor: tx}
		limits := orm.DefaultRelationLimits()
		limits.BatchSize = 2
		base := orm.WithRelationLimits(models.Users.Query().OrderBy(models.Users.Age.Asc()), limits)
		aggregates := models.UserValues
		loaded, err := base.With(models.UserValues.OrderCount, aggregates.OrderTotal, aggregates.OrderAverage, aggregates.OrderMinimum, aggregates.OrderMaximum).All(t.Context(), counter)
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
		if _, err := orm.LoadMissingFrom(t.Context(), counter, base.With(aggregates.OrderCount, aggregates.OrderTotal), loaded); err != nil {
			return err
		}
		if counter.Queries.Load() != 0 {
			t.Error("LoadMissing reloaded computed aggregate values")
		}
		counter.Queries.Store(0)
		dupes, err := orm.LoadFrom(t.Context(), counter, base.With(aggregates.OrderCount), []models.User{users[0], users[0]})
		if err != nil {
			return err
		}
		if counter.Queries.Load() != 1 || len(dupes) != 2 {
			t.Error("aggregate source keys were not deduplicated")
		}
		scoped := aggregates.OrderCount.Using(expr.Related(models.UserLinks.Orders.Where(models.Orders.TotalCents.Gt(1)), expr.Count[models.Order]()))
		filtered, err := base.Where(models.Users.Age.Lt(22)).With(scoped).All(t.Context(), counter)
		if err != nil {
			return err
		}
		n, _ := filtered[1].OrderCount.Get()
		if len(filtered) != 2 || n != 1 {
			t.Error("parent or related scope ignored")
		}
		orders, err := models.Orders.Query().With(models.OrderLinks.Buyer.With(aggregates.OrderCount)).All(t.Context(), counter)
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
		pivotScope := models.UserLinks.Groups.Where(models.Groups.Code.Eq("a")).WherePivot(models.Memberships.Priority.Gt(1))
		scopedPriority := aggregates.GroupPriority.Using(expr.Related(orm.Pivot(pivotScope), expr.Sum(models.Memberships.Priority)))
		one, err := orm.LoadFrom(t.Context(), counter, base.With(scopedPriority), users[:1])
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
	a, r := models.UserValues, models.UserLinks
	g, p := models.Groups, models.Memberships
	link := rowset.Correlate(rowset.From(models.Groups.Query()), rowset.From(models.Orders.Query()))
	group := rowset.OuterScope(link, rowset.ModelScope(models.Groups.Query()))
	order := rowset.InnerScope(link, rowset.ModelScope(models.Orders.Query()))
	ownersWithOrders := link.Where(expr.Equal(expr.At(order, models.Orders.BuyerID), expr.At(group, models.Groups.OwnerID))).Exists()
	rows, err := orm.LoadFrom(t.Context(), tx, models.Users.Query().With(
		a.GroupCount.Using(expr.Related(r.Groups, expr.Count[models.Group]().Filter(expr.ModelPredicate(g.Code.Eq("a"))))),
		a.GroupDistinct.Using(expr.Related(r.Groups, expr.CountDistinctField(g.Code).Filter(ownersWithOrders))),
		a.GroupPriority.Using(expr.Related(orm.Pivot(r.Groups), expr.Sum(p.Priority).Filter(expr.ModelPredicate(p.Priority.Gt(1))))),
	), users)
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
	ownerMatch := expr.When(expr.Equal(expr.At(order, models.Orders.BuyerID), expr.At(group, models.Groups.OwnerID)), expr.At(group, models.Groups.Code)).Else(expr.Row(expr.At(group, models.Groups.Code)).Value().Param("missing"))
	conditionalOwners := link.Where(ownerMatch.Ne("missing")).Exists()
	rows, err = orm.LoadFrom(t.Context(), tx, models.Users.Query().With(
		a.GroupCount.Using(expr.Related(r.Groups, expr.Count[models.Group]().Filter(expr.When(expr.ModelPredicate(g.Code.Eq("a")), expr.Value(g.Code)).Else(expr.Param(g.Code, "other")).Eq("a")))),
		a.GroupDistinct.Using(expr.Related(r.Groups, expr.CountDistinctField(g.Code).Filter(conditionalOwners))),
		a.GroupPriority.Using(expr.Related(orm.Pivot(r.Groups), expr.Sum(p.Priority).Filter(expr.When(expr.ModelPredicate(p.Priority.Gt(1)), expr.Param(p.Priority, 1)).Else(expr.Param(p.Priority, 0)).Eq(1)))),
	), users)
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

func assertAggregateFailures(t *testing.T, tx *database.Tx, counter *queryfixture.QueryCounter, base orm.Query[models.User], users []models.User) error {
	t.Helper()
	a := models.UserValues
	limits := orm.DefaultRelationLimits()
	limits.MaxRows = 1
	if rows, err := orm.LoadFrom(t.Context(), counter, orm.WithRelationLimits(base, limits).With(a.OrderCount), users); !errors.Is(err, fault.Invalid) || rows != nil {
		t.Error("aggregate grouped result budget ignored")
	}
	limits.MaxRows = 2
	if rows, err := orm.LoadFrom(t.Context(), counter, orm.WithRelationLimits(base, limits).With(a.OrderCount), []models.User{users[0], users[0], users[0]}); !errors.Is(err, fault.Invalid) || rows != nil {
		t.Error("aggregate duplicate-parent attachment budget ignored")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	counter.Queries.Store(0)
	if rows, err := orm.LoadFrom(ctx, counter, base.With(a.OrderCount), users); !errors.Is(err, context.Canceled) || rows != nil || counter.Queries.Load() != 0 {
		t.Error("canceled aggregate performed I/O")
	}
	bad := a.OrderCount.Using(expr.Related(models.UserLinks.Orders.With(models.OrderLinks.Buyer), expr.Count[models.Order]()))
	if rows, err := base.With(bad).All(t.Context(), counter); !errors.Is(err, fault.Invalid) || rows != nil || counter.Queries.Load() != 0 {
		t.Error("aggregate silently discarded requested child loads")
	}
	singular := a.OrderCount.Using(expr.Related(models.UserLinks.SingleOrder, expr.Count[models.Order]()))
	if rows, err := orm.LoadFrom(t.Context(), counter, base.With(singular), users); !errors.Is(err, database.TooManyRows) || rows != nil {
		t.Error("singular aggregate ignored relation cardinality")
	}
	filteredSingular := singular.Using(expr.Related(models.UserLinks.SingleOrder, expr.Count[models.Order]().Filter(expr.ModelPredicate(models.Orders.TotalCents.Lt(0)))))
	if rows, err := orm.LoadFrom(t.Context(), counter, base.With(filteredSingular), users); !errors.Is(err, database.TooManyRows) || rows != nil {
		t.Error("empty filtered measure concealed singular cardinality violation")
	}
	ambiguous := orm.ManyToManyOf(models.Users.Groups, models.Users.ID, models.Memberships.UserID, models.Memberships.Lookup, models.Groups.Name).WherePivot(models.Memberships.ID.Eq("a"))
	if rows, err := orm.LoadFrom(t.Context(), counter, base.With(a.GroupCount.Using(expr.Related(ambiguous, expr.Count[models.Group]()))), users); !errors.Is(err, database.TooManyRows) || rows != nil {
		t.Error("many-to-many aggregate accepted ambiguous join")
	}
	if rows, err := orm.LoadFrom(t.Context(), counter, base.With(a.GroupCount.Using(expr.Related(ambiguous, expr.Count[models.Group]().Filter(expr.ModelPredicate(models.Groups.Code.Eq("missing")))))), users); !errors.Is(err, database.TooManyRows) || rows != nil {
		t.Error("empty filtered measure concealed ambiguous through join")
	}
	if _, err := tx.Exec(t.Context(), `UPDATE measurements SET score = 'Infinity' WHERE label = $1`, "z"); err != nil {
		return err
	}
	if rows, err := orm.LoadFrom(t.Context(), counter, base.With(a.OrderCount, a.MeasurementScore), users); err == nil || rows != nil {
		t.Error("malformed aggregate value published partial parents")
	}
	for _, user := range users {
		if user.OrderCount.IsLoaded() || user.MeasurementScore.IsLoaded() {
			t.Error("failed aggregate mutated caller")
		}
	}
	if _, err := models.Users.Query().Count(t.Context(), counter); err != nil {
		return err
	}
	return nil
}
