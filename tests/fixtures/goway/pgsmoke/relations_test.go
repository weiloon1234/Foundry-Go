package pgsmoke_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/relation"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/testkit/dbassert"
	"github.com/weiloon1234/Foundry-Go/testkit/postgres"
	"github.com/weiloon1234/Foundry-Go/value"
)

type eagerNode struct {
	ID        int64
	ParentID  value.Nullable[int64]
	Label     string
	DeletedAt value.Nullable[temporal.DateTime]
	Parent    relation.One[eagerNode]
	Featured  relation.One[eagerNode]
	Children  relation.Many[eagerNode]
}
type eagerFields struct {
	orm.Table[eagerNode, int64]
	ID       orm.Key[eagerNode, int64]
	ParentID orm.NullableNumber[eagerNode, int64]
	Label    orm.Text[eagerNode, string]
	Parent   orm.One[eagerNode, eagerNode]
	Featured orm.One[eagerNode, eagerNode]
	Children orm.Many[eagerNode, eagerNode]
}

func eagerTable() eagerFields {
	return orm.Define("relation_nodes", func(b orm.Builder[eagerNode], m *eagerNode) eagerFields {
		table := orm.TableOf(b, &m.ID)
		orm.SoftDeleteOf(b, &m.DeletedAt)
		return eagerFields{table, table.Key(), orm.NullableNumberOf(b, &m.ParentID), orm.TextOf(b, &m.Label), orm.OneOf(b, &m.Parent), orm.OneOf(b, &m.Featured), orm.ManyOf(b, &m.Children)}
	})
}
func TestDirectEagerTreesAndExistence(t *testing.T) {
	detector, err := database.NewStatementDetector(database.StatementDetectorConfig{Threshold: 2, MaxShapes: 32})
	must(t, err)
	queries := 0
	db := postgres.Isolate(t).Open(t, database.WithStatementDetector(detector), database.WithQueryObserver(func(_ context.Context, e database.QueryEvent) {
		if e.Operation == "query" {
			queries++
		}
	}))
	ctx := t.Context()
	_, err = db.Exec(ctx, `CREATE TABLE relation_nodes(id bigint PRIMARY KEY,parent_id bigint,label text NOT NULL,deleted_at timestamptz)`)
	must(t, err)
	_, err = db.Exec(ctx, `INSERT INTO relation_nodes VALUES
 (1,NULL,'parent',NULL),(2,NULL,'parent',NULL),(3,NULL,'parent',NULL),
 (10,1,'keep',NULL),(11,1,'skip',NULL),(12,2,'only',NULL),(13,1,'keep',now()),(14,3,'keep',now()),
 (20,10,'grand',NULL),(21,12,'grand',NULL)`)
	must(t, err)
	n := eagerTable()
	must(t, orm.Check(n))
	children := orm.HasManyOf(n.Children, n.ID, n.ParentID)
	tree := children.Where(n.Label.In("keep", "only")).OrderBy(n.Label.Desc()).With(children)
	q := orm.WithRelationLimits(n.Query().Where(n.ID.In(1, 2, 3)).OrderBy(n.ID.Asc()).With(tree), orm.RelationLimits{BatchSize: 2, MaxRows: 100, MaxDepth: 3})
	scoped, scope := detector.Begin(ctx)
	parents, err := q.All(scoped, db)
	must(t, err)
	if len(parents) != 3 || queries != 4 {
		t.Fatalf("parents=%d statements=%d want=4", len(parents), queries)
	}
	for i, p := range parents {
		if !p.Children.IsLoaded() {
			t.Fatal("not loaded")
		}
		if i == 2 {
			if p.Children.Len() != 0 {
				t.Fatal("soft-deleted children escaped")
			}
			continue
		}
		if p.Children.Len() != 1 {
			t.Fatal("constraint/soft-delete mismatch", p.Children.Len())
		}
		for _, c := range p.Children.All() {
			if c.ID != []int64{10, 12}[i] || c.Children.Len() != 1 {
				t.Fatal("wrong nested child", c.ID)
			}
			for _, g := range c.Children.All() {
				if g.ID != []int64{20, 21}[i] {
					t.Fatal("wrong grandchild", g.ID)
				}
			}
		}
	}
	dbassert.AssertStatementShapes(t, scope)

	queries = 0
	hasOne := orm.HasOneOf(n.Featured, n.ID, n.ParentID).Where(n.Label.Eq("only"))
	one, err := n.Query().Where(n.ID.In(1, 2, 3)).OrderBy(n.ID.Asc()).With(hasOne).All(ctx, db)
	must(t, err)
	if queries != 2 {
		t.Fatal("has-one statement count", queries)
	}
	for i, p := range one {
		v, loaded := p.Featured.Get()
		if !loaded || v.IsSet() != (i == 1) {
			t.Fatal("has-one loaded state")
		}
	}
	bad, err := n.Query().Where(n.ID.Eq(1)).With(orm.HasOneOf(n.Featured, n.ID, n.ParentID)).All(ctx, db)
	if bad != nil || !errors.Is(err, database.TooManyRows) {
		t.Fatal("has-one cardinality", bad, err)
	}

	queries = 0
	parents[1].Children = relation.Many[eagerNode]{}
	missing, err := orm.LoadMissing(ctx, db, parents, tree)
	must(t, err)
	if queries != 2 || parents[1].Children.IsLoaded() || !missing[2].Children.IsLoaded() || missing[2].Children.Len() != 0 {
		t.Fatal("LoadMissing ownership/count/empty", queries)
	}
	for _, c := range missing[0].Children.All() {
		if c.ID != 10 || c.Children.Len() != 1 {
			t.Fatal("loaded branch replaced")
		}
	}
	for _, c := range missing[1].Children.All() {
		if c.ID != 12 || c.Children.Len() != 1 {
			t.Fatal("missing branch not loaded")
		}
	}
	before := queries
	_, err = orm.LoadMissing(ctx, db, missing, tree)
	must(t, err)
	if queries != before {
		t.Fatal("loaded-empty issued SQL")
	}

	for _, negative := range []bool{false, true} {
		queries = 0
		scoped, scope = detector.Begin(ctx)
		predicate := orm.Has(children, orm.Has(children, n.Label.Eq("grand")))
		expected := []int64{1, 2}
		if negative {
			predicate = orm.DoesntHave(children, orm.Has(children, n.Label.Eq("grand")))
			expected = []int64{3}
		}
		got, err := n.Query().Where(n.ID.In(1, 2, 3), predicate).OrderBy(n.ID.Asc()).All(scoped, db)
		must(t, err)
		ids := make([]int64, len(got))
		for i, m := range got {
			ids[i] = m.ID
			if m.Children.IsLoaded() {
				t.Fatal("existence eager-loaded")
			}
		}
		if queries != 1 || !reflect.DeepEqual(ids, expected) {
			t.Fatal("nested existence mismatch", ids, queries)
		}
		dbassert.AssertStatementShapes(t, scope)
	}
	live, err := n.Query().Where(n.ID.In(1, 2, 3), orm.Has(children, n.Label.Eq("keep"))).All(ctx, db)
	must(t, err)
	if len(live) != 1 || live[0].ID != 1 {
		t.Fatal("existence failed related soft-delete scope")
	}

	queries = 0
	overflow := orm.WithRelationLimits(q, orm.RelationLimits{BatchSize: 2, MaxRows: 1, MaxDepth: 3})
	if result, err := overflow.All(ctx, db); result != nil || !errors.Is(err, fault.Invalid) {
		t.Fatal("partial over-budget parents", result, err)
	}
	if queries != 2 {
		t.Fatal("budget sentinel query count", queries)
	}
	queries = 0
	tooDeep := orm.WithRelationLimits(q, orm.RelationLimits{BatchSize: 2, MaxRows: 100, MaxDepth: 1})
	if result, err := tooDeep.All(ctx, db); result != nil || !errors.Is(err, fault.Invalid) || queries != 0 {
		t.Fatal("depth validation issued SQL", result, err, queries)
	}
}
