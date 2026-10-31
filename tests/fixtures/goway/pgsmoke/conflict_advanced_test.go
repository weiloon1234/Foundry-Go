package pgsmoke_test

import (
	"context"
	"errors"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	r "github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/testkit/postgres"
	"github.com/weiloon1234/Foundry-Go/value"
)

type advancedCounter struct {
	ID      int64
	Tenant  int64
	Count   int64 `foundry:"default=database"`
	Enabled bool  `foundry:"default=database"`
	Note    value.Nullable[int64]
}
type advancedCounters struct {
	orm.Table[advancedCounter, int64]
	Tenant, Count orm.Number[advancedCounter, int64]
	Enabled       orm.Column[advancedCounter, bool]
	Note          orm.NullableNumber[advancedCounter, int64]
}
type advancedTenantKey struct{}
type advancedLookup struct{}

// Lane B compiles/vets; the lead executes in a retained isolated namespace.
func TestAdvancedConflictIndexDefaultsCorrelationAndTenancy(t *testing.T) {
	db := postgres.Isolate(t).Open(t)
	ctx := context.WithValue(t.Context(), advancedTenantKey{}, int64(1))
	_, err := db.Exec(ctx, `CREATE TABLE advanced_counters(id bigint PRIMARY KEY,tenant bigint NOT NULL,count bigint NOT NULL DEFAULT 3,enabled boolean NOT NULL DEFAULT true,note bigint)`)
	must(t, err)
	_, err = db.Exec(ctx, `CREATE UNIQUE INDEX advanced_counter_partial ON advanced_counters ((id + id)) WHERE enabled = true`)
	must(t, err)
	calls := 0
	f := orm.Define("advanced_counters", func(b orm.Builder[advancedCounter], m *advancedCounter) advancedCounters {
		return advancedCounters{orm.TableOf(b, &m.ID), orm.NumberOf(b, &m.Tenant), orm.NumberOf(b, &m.Count), orm.ColumnOf(b, &m.Enabled), orm.NullableNumberOf(b, &m.Note)}
	}, orm.WithHooks(func() orm.Hooks[advancedCounter] { calls++; return orm.Hooks[advancedCounter]{} }), orm.WithGlobalScopes(func(t advancedCounters) []orm.GlobalScope[advancedCounter] {
		return []orm.GlobalScope[advancedCounter]{orm.Scope(t.Table, "tenant", func(ctx context.Context) (orm.Predicate[advancedCounter], error) {
			tenant, ok := ctx.Value(advancedTenantKey{}).(int64)
			if !ok {
				return orm.Predicate[advancedCounter]{}, errors.New("tenant required")
			}
			return orm.NumberEq(t.Tenant, tenant), nil
		})}
	}))
	records := r.ConflictRowsOf(f.Query())
	stored, proposed := expr.At(records.Stored(), orm.NumberExpr(f.Count)), expr.At(records.Proposed(), orm.NumberExpr(f.Count))
	id := expr.Value(orm.KeyExpr(f.Key()))
	policy := r.OnConflictKeys(expr.Group(expr.Add(id, id))).TargetWhere(orm.ColumnEq(f.Enabled, true)).DoUpdate(r.SetConflictValue(orm.NumberExpr(f.Count), expr.Add(stored, proposed)), r.SetConflictValue(orm.NullableNumberExpr(f.Note), expr.At(records.Proposed(), orm.NullableNumberExpr(f.Note))))
	for _, want := range []int64{3, 6} {
		item, e := orm.Upsert(ctx, db, f.Query(), policy, orm.KeySet(f.Key(), 1), orm.NumberSet(f.Tenant, 1))
		must(t, e)
		got, ok := item.Get()
		if !ok || got.Count != want || !got.Note.IsNull() {
			t.Fatal(got, want)
		}
	}
	conditional := r.ConflictWhereRows(policy, expr.Greater(proposed, stored))
	item, err := orm.Upsert(ctx, db, f.Query(), conditional, orm.KeySet(f.Key(), 1), orm.NumberSet(f.Tenant, 1))
	must(t, err)
	if item.IsSet() {
		t.Fatal("false condition returned a row")
	}
	_, err = orm.Upsert(context.WithValue(ctx, advancedTenantKey{}, int64(2)), db, f.Query(), policy, orm.KeySet(f.Key(), 1), orm.NumberSet(f.Tenant, 2))
	if !errors.Is(err, fault.Conflict) {
		t.Fatal("tenant suppression accepted", err)
	}
	_, err = orm.Upsert(ctx, db, f.Query(), policy, orm.KeySet(f.Key(), 2), orm.NumberSet(f.Tenant, 1), orm.NumberSet(f.Count, 7))
	must(t, err)
	lookup := r.As[advancedLookup](f.Query().Where(orm.KeyEq(f.Key(), 2)), "lookup")
	c := r.Correlate(records, lookup)
	outer := expr.At(r.OuterScope(c, records.Stored()), orm.NumberExpr(f.Count))
	inner := expr.At(r.InnerScope(c, r.Scope(lookup)), orm.NumberExpr(f.Count))
	selected := r.SelectCorrelatedScalar(c, expr.AsSelected(expr.Row(inner))).Where(expr.Greater(inner, outer)).Limit(1)
	scalarPolicy := orm.OnConflict(orm.ConflictKey(orm.KeyExpr(f.Key()))).DoUpdate(r.SetConflictValue(orm.NullableNumberExpr(f.Note), r.CorrelatedScalarRowQuery(selected)))
	item, err = orm.Upsert(ctx, db, f.Query(), scalarPolicy, orm.KeySet(f.Key(), 1), orm.NumberSet(f.Tenant, 1))
	must(t, err)
	got, ok := item.Get()
	note, hasNote := got.Note.Get()
	if !ok || !hasNote || note != 7 || got.Count != 6 || got.Tenant != 1 || calls != 0 {
		t.Fatal(got, calls)
	}
	_, err = orm.Upsert(t.Context(), db, f.Query(), scalarPolicy, orm.KeySet(f.Key(), 1), orm.NumberSet(f.Tenant, 1))
	if err == nil {
		t.Fatal("missing tenant accepted")
	}
	persisted, e := orm.RequireKey(ctx, db, f.Table, 1)
	must(t, e)
	if persisted.Count != 6 || persisted.Tenant != 1 {
		t.Fatal("scope failure changed persisted row", persisted)
	}
}
