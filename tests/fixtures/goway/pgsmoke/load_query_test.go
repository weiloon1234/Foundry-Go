package pgsmoke_test

import (
	"context"
	"errors"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/testkit/postgres"
	"testing"
)

func TestLoadFromQueryBudgetsPostgres(t *testing.T) {
	queries := 0
	db := postgres.Isolate(t).Open(t, database.WithQueryObserver(func(_ context.Context, e database.QueryEvent) {
		if e.Operation == "query" {
			queries++
		}
	}))
	_, err := db.Exec(t.Context(), `CREATE TABLE relation_nodes(id bigint PRIMARY KEY,parent_id bigint,label text NOT NULL,deleted_at timestamptz)`)
	must(t, err)
	_, err = db.Exec(t.Context(), `INSERT INTO relation_nodes VALUES(1,NULL,'parent',NULL),(2,NULL,'parent',NULL),(10,1,'child',NULL),(11,1,'child',NULL),(12,2,'child',NULL),(20,10,'grand',NULL)`)
	must(t, err)
	n := eagerTable()
	rel := orm.HasManyOf(n.Children, n.ID, n.ParentID)
	parents := []eagerNode{{ID: 1}, {ID: 2}}
	for _, missing := range []bool{false, true} {
		run := orm.LoadFrom[eagerNode]
		if missing {
			run = orm.LoadMissingFrom[eagerNode]
		}
		q := orm.WithRelationLimits(n.Query().With(rel.With(rel)), orm.RelationLimits{BatchSize: 1, MaxRows: 3, MaxDepth: 2})
		got, err := run(t.Context(), db, q, parents)
		if got != nil || !errors.Is(err, fault.Invalid) || parents[0].Children.IsLoaded() {
			t.Fatal("partial load", got, err)
		}
		queries = 0
		q = orm.WithRelationLimits(q, orm.RelationLimits{BatchSize: 1, MaxRows: 10, MaxDepth: 1})
		got, err = run(t.Context(), db, q, parents)
		if got != nil || !errors.Is(err, fault.Invalid) || queries != 0 {
			t.Fatal("depth validation sent SQL", err, queries)
		}
		q = orm.WithRelationLimits(q, orm.RelationLimits{BatchSize: 1, MaxRows: 10, MaxDepth: 2})
		got, err = run(t.Context(), db, q, parents)
		must(t, err)
		if len(got) != 2 || got[0].Children.Len() != 2 || got[1].Children.Len() != 1 || queries != 5 {
			t.Fatal("load issued a parent statement or lost batching", queries)
		}
		before := queries
		_, err = orm.LoadMissingFrom(t.Context(), db, q, got)
		must(t, err)
		if queries != before {
			t.Fatal("loaded slots queried again")
		}
	}
}
