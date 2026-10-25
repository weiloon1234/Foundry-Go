package goway_test

import (
	"context"
	"database/sql/driver"
	"foundry.test/goway/coremodels"
	n "foundry.test/goway/models/reads"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	rowset "github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"reflect"
	"strings"
	"testing"
)

func TestScopePropagationProjectionJoinsAggregatesAndRelationBypasses(t *testing.T) {
	var trace []string
	ctx := coremodels.TenantContext(coremodels.LifecycleContext(t.Context(), &trace), 7)
	capture := &goldenCapture{reply: func(sql string, _ []driver.NamedValue) ([]string, [][]driver.Value) {
		if strings.Contains(sql, "COUNT") {
			return []string{"name"}, [][]driver.Value{{int64(1)}}
		}
		return []string{"name"}, [][]driver.Value{{"member"}}
	}}
	db := capture.open(ctx)
	defer db.Close(context.Background())
	// Projection and scalar results carry model source predicates, never retrieval.
	_, err := rowset.SelectValue(n.Members.Query(), n.Members.Name).All(ctx, db)
	check(err)
	left := rowset.As[readSide](orm.WithScopeContext(n.Members.Query(), coremodels.TenantContext(ctx, 99)), "m")
	right := rowset.As[groupSide](n.Groups.Query(), "g")
	joined := rowset.InnerJoin(left, right, rowset.On(expr.At(rowset.Scope(left), n.Members.ID), expr.At(rowset.Scope(right), n.Groups.ID)))
	name := expr.At(rowset.LeftScope(joined, rowset.Scope(left)), n.Members.Name)
	_, err = rowset.Select(joined, readNames, rowset.Map(readNames.Name, name)).All(ctx, db)
	check(err)
	_, err = rowset.Select(left, readCounts, rowset.MapSelected(readCounts.Count, expr.Count[rowset.Alias[readSide, n.Member]]().Value())).All(ctx, db)
	check(err)
	if len(trace) != 0 || strings.Contains(capture.output.String(), "99") || strings.Count(capture.sql.String(), `"tenant_id" =`) != 4 {
		t.Fatal("projection lost scope or retrieved", trace, capture.output.String())
	}
	// A relation keeps its concrete methods after the package-level bypass.
	targetOff := orm.WithoutRelationGlobalScope(n.MemberGroups, n.GroupTenant(n.Groups)).Where(n.Groups.Name.Eq("group"))
	pivotOff := orm.WithoutPivotGlobalScope(n.MemberGroups, n.MembershipTenant(n.Memberships))
	allOff := orm.WithoutPivotGlobalScopes(orm.WithoutRelationGlobalScopes(n.MemberGroups))
	directOff := orm.WithoutRelationGlobalScopes(n.Manager).Where(n.Members.Name.Eq("manager"))
	for i, p := range []orm.Predicate[n.Member]{orm.Has(targetOff), orm.Has(pivotOff), orm.Has(allOff), orm.Has(directOff)} {
		s, err := orm.Compile(orm.WithScopeContext(n.Members.Query().Where(p), ctx))
		check(err)
		want := []int{2, 2, 1, 1}[i]
		if got := strings.Count(s.SQL(), `"tenant_id" =`); got != want {
			t.Fatal("wrong target/pivot removal", i, got, s.SQL())
		}
	}
	s, err := orm.Compile(orm.WithScopeContext(n.Members.Query().Where(orm.Has(n.MemberGroups)), ctx))
	check(err)
	if strings.Count(s.SQL(), `"tenant_id" =`) != 3 {
		t.Fatal("bypass mutated source descriptor", s.SQL())
	}
}

func TestJoinedRetrievalCallbacksVisitOnlyCurrentBatch(t *testing.T) {
	var targets, pivots []int64
	batches := int64(0)
	capture := &goldenCapture{reply: func(sql string, _ []driver.NamedValue) ([]string, [][]driver.Value) {
		if strings.HasPrefix(sql, `SELECT "foundry_target".`) {
			batches++
			return []string{"id", "tenant_id", "name", "id", "tenant_id", "member_id", "group_id"}, [][]driver.Value{
				{batches + 10, int64(7), "group", batches + 20, int64(7), batches, batches + 10},
			}
		}
		return []string{"id", "tenant_id", "name", "manager_id"}, [][]driver.Value{
			{int64(1), int64(7), "first", nil}, {int64(2), int64(7), "second", nil},
		}
	}}
	db, err := database.Prepare(database.Adapter{Connector: connector{capture}}, database.DefaultPoolConfig())
	check(err)
	t.Cleanup(func() { check(db.Close(context.Background())) })
	targetObserver, err := orm.NewRetrievalObserver[n.Group]("batch.target").Declare(func() orm.RetrievalHooks[n.Group] {
		return orm.RetrievalHooks[n.Group]{Retrieved: func(_ context.Context, _ database.Executor, m n.Group) error {
			targets = append(targets, m.ID)
			return nil
		}}
	})
	check(err)
	pivotObserver, err := orm.NewRetrievalObserver[n.Membership]("batch.pivot").Declare(func() orm.RetrievalHooks[n.Membership] {
		return orm.RetrievalHooks[n.Membership]{Retrieved: func(_ context.Context, _ database.Executor, m n.Membership) error {
			pivots = append(pivots, m.ID)
			return nil
		}}
	})
	check(err)
	set, err := lifecycle.NewObservers(targetObserver, pivotObserver)
	check(err)
	check(db.BindObservers(set))
	ctx := coremodels.TenantContext(t.Context(), 7)
	check(db.Start(ctx))
	limits := orm.DefaultRelationLimits()
	limits.BatchSize = 1
	items, err := orm.WithRelationLimits(n.Members.Query().With(n.MemberGroups), limits).All(ctx, db)
	check(err)
	if len(items) != 2 || batches != 2 || !reflect.DeepEqual(targets, []int64{11, 12}) || !reflect.DeepEqual(pivots, []int64{21, 22}) {
		t.Fatalf("batch callbacks repeated or lost rows: parents=%d batches=%d targets=%v pivots=%v", len(items), batches, targets, pivots)
	}
}
