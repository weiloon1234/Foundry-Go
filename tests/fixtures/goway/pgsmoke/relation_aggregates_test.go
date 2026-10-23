package pgsmoke_test

import (
	"context"
	"errors"
	"testing"

	j "foundry.test/goway/models/aggregates"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/relation"
	"github.com/weiloon1234/Foundry-Go/testkit/postgres"
)

func TestStage4dRelationAggregatePostgres(t *testing.T) {
	statements := 0
	db := postgres.Isolate(t).Open(t, database.WithQueryObserver(func(_ context.Context, e database.QueryEvent) {
		if e.Operation == "query" {
			statements++
		}
	}))
	ctx := t.Context()
	for _, sql := range []string{
		`CREATE TABLE joined_parents(id bigint PRIMARY KEY,name text NOT NULL,deleted_at timestamptz)`,
		`CREATE TABLE joined_targets(id bigint PRIMARY KEY,parent_id bigint NOT NULL,name text NOT NULL,rank bigint NOT NULL,deleted_at timestamptz)`,
		`CREATE TABLE joined_pivots(id bigint PRIMARY KEY,parent_id bigint,target_id bigint,kind text NOT NULL,role text NOT NULL,deleted_at timestamptz)`,
		`INSERT INTO joined_parents VALUES (1,'one',NULL),(2,'two',NULL),(3,'empty',NULL)`,
		`INSERT INTO joined_targets VALUES (10,1,'ten',7,NULL),(11,1,'eleven',9,NULL),(12,2,'twelve',8,NULL),(13,1,'deleted',99,now())`,
		`INSERT INTO joined_pivots VALUES (100,1,10,'parent','a',NULL),(101,2,10,'parent','b',NULL),(102,1,10,'parent','c',NULL),(103,1,11,'target','d',NULL),(104,1,12,'parent','deleted',now()),(105,NULL,10,'parent','null source',NULL),(106,1,NULL,'parent','null target',NULL),(107,1,999,'parent','missing target',NULL)`,
	} {
		_, err := db.Exec(ctx, sql)
		must(t, err)
	}
	statements = 0
	parents, err := orm.WithRelationLimits(j.Parents.Query().OrderBy(j.Parents.ID.Asc()).With(j.ParentAggregates.Count, j.ParentAggregates.Total), orm.RelationLimits{BatchSize: 2, MaxRows: 100, MaxDepth: 3}).All(ctx, db)
	must(t, err)
	if statements != 5 || len(parents) != 3 {
		t.Fatal("per-slot batches", statements, len(parents))
	}
	for i, p := range parents {
		n, ok := p.Count.Get()
		if !ok || n != []int64{2, 1, 0}[i] {
			t.Fatal("count", i, n, ok)
		}
		v, ok := p.Total.Get()
		if !ok {
			t.Fatal("unloaded total")
		}
		if i == 2 {
			if !v.IsNull() {
				t.Fatal("empty sum")
			}
		} else {
			x, ok := v.Get()
			if !ok || x.String() != []string{"16", "8"}[i] {
				t.Fatal("sum", i, v)
			}
		}
	}
	statements = 0
	_, err = orm.LoadMissing(ctx, db, parents, j.ParentAggregates.Count, j.ParentAggregates.Total)
	must(t, err)
	if statements != 0 {
		t.Fatal("loaded zero/NULL queried again")
	}
	parents[1].Count = relation.Value[int64]{}
	_, err = orm.LoadMissing(ctx, db, parents, j.ParentAggregates.Count)
	must(t, err)
	if statements != 1 {
		t.Fatal("partial LoadMissing", statements)
	}
	statements = 0
	peers, err := orm.Load(ctx, db, parents, j.ParentAggregates.Peers, j.ParentAggregates.Pivots)
	must(t, err)
	if statements != 2 {
		t.Fatal("target and pivot slots must remain separate", statements)
	}
	for i, p := range peers {
		n, _ := p.Count.Get()
		m, _ := p.OtherCount.Get()
		if n != []int64{3, 1, 0}[i] || m != n {
			t.Fatal("edge counts", i, n, m)
		}
	}
	distinct := j.Parents.Count.Using(expr.Related(j.Peers, expr.CountDistinctField(j.Targets.ID)))
	peers, err = orm.Load(ctx, db, parents, distinct)
	must(t, err)
	if n, _ := peers[0].Count.Get(); n != 2 {
		t.Fatal("distinct targets", n)
	}
	filtered := j.ParentAggregates.Count.Using(expr.Related(j.DirectTargets, expr.Count[j.Target]().Filter(expr.OrderRow(expr.Value(j.Targets.Rank)).Gt(8))))
	peers, err = orm.Load(ctx, db, parents, filtered)
	must(t, err)
	if n, _ := peers[0].Count.Get(); n != 1 {
		t.Fatal("aggregate FILTER", n)
	}
	if _, err := orm.Load(ctx, db, parents, j.Parents.Count.Using(expr.Related(j.Chosen, expr.Count[j.Target]().Filter(expr.Value(j.Targets.Rank).Eq(-1))))); !errors.Is(err, database.TooManyRows) {
		t.Fatal("FILTER concealed singular cardinality", err)
	}
	statements = 0
	peers, err = orm.Load(ctx, db, parents, j.Parents.Count.Using(expr.Related(j.MorphPeers, expr.Count[j.Target]())))
	must(t, err)
	if n, _ := peers[0].Count.Get(); n != 2 || statements != 1 {
		t.Fatal("morph pivot scope", n, statements)
	}
	peers, err = orm.Load(ctx, db, parents, j.Parents.Count.Using(expr.Related(j.TargetsThrough, expr.Count[j.Target]())))
	must(t, err)
	if n, _ := peers[0].Count.Get(); n != 3 {
		t.Fatal("through paths", n)
	}
	nested := j.Targets.Count.Using(expr.Related(j.TargetParent, expr.Count[j.Parent]()))
	statements = 0
	peers, err = orm.Load(ctx, db, parents, j.DirectTargets.With(nested))
	must(t, err)
	if statements != 2 {
		t.Fatal("nested aggregate batch", statements)
	}
	for _, p := range peers {
		for _, target := range p.Targets.All() {
			if n, ok := target.Count.Get(); !ok || n != 1 {
				t.Fatal("nested hydration")
			}
		}
	}
	rollback := errors.New("rollback aggregate fixture")
	err = db.Transaction(ctx, func(tx *database.Tx) error {
		_, err := j.Targets.Insert(ctx, tx, j.Targets.ID.Set(14), j.Targets.ParentID.Set(1), j.Targets.Name.Set("new"), j.Targets.Rank.Set(1))
		if err != nil {
			return err
		}
		statements = 0
		out, err := j.Parents.Query().Where(j.Parents.ID.Eq(1)).With(j.ParentAggregates.Count).ForUpdate().All(ctx, tx)
		if err != nil {
			return err
		}
		if n, _ := out[0].Count.Get(); n != 3 || statements != 2 {
			t.Fatal("transaction visibility/locked count", n, statements)
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatal(err)
	}
	peers, err = orm.Load(ctx, db, parents, j.ParentAggregates.Count)
	must(t, err)
	if n, _ := peers[0].Count.Get(); n != 2 {
		t.Fatal("rollback did not restore aggregate", n)
	}
}
