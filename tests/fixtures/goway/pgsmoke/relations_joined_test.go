package pgsmoke_test

import (
	"context"
	"errors"
	j "foundry.test/goway/models/joined"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/testkit/dbassert"
	"github.com/weiloon1234/Foundry-Go/testkit/postgres"
	"github.com/weiloon1234/Foundry-Go/value"
	"testing"
)

func TestJoinedRelationFamilies(t *testing.T) {
	detector, err := database.NewStatementDetector(database.StatementDetectorConfig{Threshold: 2, MaxShapes: 32})
	must(t, err)
	statements := 0
	db := postgres.Isolate(t).Open(t, database.WithStatementDetector(detector), database.WithQueryObserver(func(_ context.Context, e database.QueryEvent) {
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
		`INSERT INTO joined_targets VALUES (10,1,'ten',7,NULL),(11,1,'eleven',7,NULL),(12,2,'twelve',8,NULL),(13,1,'deleted',99,now())`,
		`INSERT INTO joined_pivots VALUES (100,1,10,'parent','a',NULL),(101,2,10,'parent','b',NULL),(102,1,10,'parent','c',NULL),(103,1,11,'target','d',NULL),(104,1,12,'parent','deleted',now()),(105,NULL,10,'parent','null source',NULL),(106,1,NULL,'parent','null target',NULL),(107,1,999,'parent','missing target',NULL)`} {
		_, err := db.Exec(ctx, sql)
		must(t, err)
	}
	scopeCtx, scope := detector.Begin(ctx)
	statements = 0
	r := j.Peers.With(j.TargetParent).WithPivot(j.PivotParent)
	parents, err := orm.All(scopeCtx, db, orm.WithRelationLimits(j.Parents.Query().Where(orm.KeyIn(j.Parents.ID, 1, 2, 3)).OrderBy(orm.KeyAsc(j.Parents.ID)).With(r), orm.RelationLimits{BatchSize: 2, MaxRows: 100, MaxDepth: 3}))
	must(t, err)
	// parent + two joined batches + one complete target branch + one pivot branch.
	if statements != 5 || len(parents) != 3 || parents[0].Peers.Len() != 3 || parents[1].Peers.Len() != 1 || parents[2].Peers.Len() != 0 {
		t.Fatal("joined count/pivot rows", statements, len(parents))
	}
	for i, p := range parents {
		for _, link := range p.Peers.All() {
			owner, _ := link.Pivot.ParentID.Get()
			if owner != int64(i+1) || !link.Model.Parent.IsLoaded() || !link.Pivot.Parent.IsLoaded() {
				t.Fatal("pivot ownership/nesting")
			}
			if link.Pivot.ID == 104 || link.Pivot.Role == "deleted" {
				t.Fatal("deleted pivot escaped")
			}
		}
	}
	dbassert.AssertStatementShapes(t, scope)
	first, _ := parents[0].Peers.Get()
	second, _ := parents[1].Peers.Get()
	if first[0].Model.ID != second[0].Model.ID || first[0].Pivot.Role == second[0].Pivot.Role {
		t.Fatal("shared target pivot contaminated")
	}
	statements = 0
	filtered, err := orm.LoadFrom(ctx, db, j.Parents.Query().With(j.Peers.Where(orm.TextEq(j.Targets.Name, "ten")).WherePivot(orm.TextEq(j.Pivots.Role, "c"))), []j.Parent{{ID: 1}})
	must(t, err)
	if statements != 1 || filtered[0].Peers.Len() != 1 {
		t.Fatal("constraints")
	}
	statements = 0
	deleted, err := orm.LoadFrom(ctx, db, j.Parents.Query().With(j.Peers.OnlyTrashedPivot()), []j.Parent{{ID: 1}})
	must(t, err)
	if statements != 1 || deleted[0].Peers.Len() != 1 {
		t.Fatal("pivot-only trash scope")
	}
	statements = 0
	through, err := orm.LoadFrom(ctx, db, j.Parents.Query().With(j.TargetsThrough), []j.Parent{{ID: 1}, {ID: 2}})
	must(t, err)
	if statements != 1 || through[0].Targets.Len() != 3 || through[1].Targets.Len() != 1 {
		t.Fatal("through paths or batch count", statements)
	}
	if result, err := orm.LoadFrom(ctx, db, j.Parents.Query().With(j.ChosenThrough), []j.Parent{{ID: 1}}); result != nil || !errors.Is(err, database.TooManyRows) {
		t.Fatal("singular through cardinality", err)
	}
	for _, tc := range []struct {
		r  orm.HasOne[j.Parent, j.Target]
		id int64
	}{{j.Chosen.LatestOfMany(), 11}, {j.Chosen.OldestOfMany(), 10}, {j.Chosen.OfMany(orm.NumberDesc(j.Targets.Rank)), 10}, {j.ChosenThrough.OfMany(orm.NumberDesc(j.Targets.Rank)), 10}} {
		statements = 0
		got, err := orm.LoadFrom(ctx, db, j.Parents.Query().With(tc.r), []j.Parent{{ID: 1}})
		must(t, err)
		v, _ := got[0].Chosen.Get()
		m, ok := v.Get()
		if statements != 1 || !ok || m.ID != tc.id {
			t.Fatal("one-of-many/tie", statements, m.ID)
		}
	}
	statements = 0
	morph, err := orm.LoadFrom(ctx, db, j.Parents.Query().With(j.MorphChildren), []j.Parent{{ID: 1}, {ID: 2}, {ID: 3}})
	must(t, err)
	if statements != 1 || morph[0].MorphChildren.Len() != 4 || morph[1].MorphChildren.Len() != 1 || morph[2].MorphChildren.Len() != 0 {
		t.Fatal("morph source discriminator", statements, morph[0].MorphChildren.Len())
	}
	one, err := orm.LoadFrom(ctx, db, j.Parents.Query().With(j.MorphChild), []j.Parent{{ID: 2}})
	must(t, err)
	v, _ := one[0].MorphChild.Get()
	if !v.IsSet() {
		t.Fatal("morph-one")
	}
	statements = 0
	inverse, err := orm.LoadFrom(ctx, db, j.Pivots.Query().With(j.MorphParent, j.MorphTarget), []j.Pivot{{ID: 1, ParentID: value.Of(int64(1)), Kind: "parent"}, {ID: 2, ParentID: value.Of(int64(10)), Kind: "target"}, {ID: 3, ParentID: value.Of(int64(1)), Kind: "unknown"}})
	must(t, err)
	if statements != 2 {
		t.Fatal("morph group batches", statements)
	}
	a, _ := inverse[0].MorphParent.Get()
	b, _ := inverse[1].MorphTarget.Get()
	if !a.IsSet() || !b.IsSet() {
		t.Fatal("concrete morph targets")
	}
	a, _ = inverse[2].MorphParent.Get()
	b, _ = inverse[2].MorphTarget.Get()
	if a.IsSet() || b.IsSet() {
		t.Fatal("foreign discriminator")
	}
	statements = 0
	_, err = orm.LoadFrom(ctx, db, j.Pivots.Query().With(j.MorphParent, j.MorphTarget), []j.Pivot{{ID: 3, Kind: "unknown"}})
	must(t, err)
	if statements != 0 {
		t.Fatal("empty morph issued SQL")
	}
	forward, err := orm.LoadFrom(ctx, db, j.Parents.Query().With(j.MorphPeers), []j.Parent{{ID: 1}})
	must(t, err)
	if forward[0].Peers.Len() != 2 {
		t.Fatal("morph pivot filter")
	}
	back, err := orm.LoadFrom(ctx, db, j.Targets.Query().With(j.Inverse), []j.Target{{ID: 10}})
	must(t, err)
	if back[0].Inverse.Len() != 3 {
		t.Fatal("morphed-by-many")
	}
	// Both joined families support nested EXISTS without extra statements.
	statements = 0
	matches, err := orm.All(ctx, db, j.Parents.Query().Where(orm.Has(j.Peers, orm.KeyEq(j.Targets.ID, 11))))
	must(t, err)
	if len(matches) != 1 || matches[0].ID != 1 || statements != 1 {
		t.Fatal("pivot exists", statements)
	}
	statements = 0
	matches, err = orm.All(ctx, db, j.Parents.Query().Where(orm.Has(j.TargetsThrough, orm.KeyEq(j.Targets.ID, 11))))
	must(t, err)
	if len(matches) != 1 || matches[0].ID != 1 || statements != 1 {
		t.Fatal("through exists", statements)
	}
	_, err = db.Exec(ctx, `INSERT INTO joined_pivots VALUES (200,1,7,'parent','ambiguous',NULL)`)
	must(t, err)
	ambiguous := orm.ManyToManyOf(j.Parents.Peers, orm.KeyExpr(j.Parents.ID), orm.NullableNumberExpr(j.Pivots.ParentID), orm.NullableNumberExpr(j.Pivots.TargetID), orm.NumberExpr(j.Targets.Rank))
	if result, err := orm.LoadFrom(ctx, db, j.Parents.Query().With(ambiguous), []j.Parent{{ID: 1}}); result != nil || !errors.Is(err, database.TooManyRows) {
		t.Fatal("pivot matched multiple targets", err)
	}
}
