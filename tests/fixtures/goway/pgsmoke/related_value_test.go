package pgsmoke_test

import (
	"context"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/database/relation"
	"github.com/weiloon1234/Foundry-Go/testkit/postgres"
)

type annotatedNode struct {
	ID, ParentID, Amount int64
	Children             relation.Many[annotatedNode]
}
type annotatedTable struct {
	orm.Table[annotatedNode, int64]
	ParentID, Amount orm.Number[annotatedNode, int64]
	Children         orm.Many[annotatedNode, annotatedNode]
}

func TestRelatedValueHydratesModelsAndReleasesSession(t *testing.T) {
	db := postgres.Isolate(t).Open(t)
	ctx := t.Context()
	_, err := db.Exec(ctx, `CREATE TABLE annotated_nodes(id bigint PRIMARY KEY,parent_id bigint NOT NULL,amount bigint NOT NULL)`)
	must(t, err)
	_, err = db.Exec(ctx, `INSERT INTO annotated_nodes VALUES(1,0,0),(2,1,3),(3,1,5)`)
	must(t, err)
	calls, factories := 0, 0
	table := orm.Define("annotated_nodes", func(b orm.Builder[annotatedNode], n *annotatedNode) annotatedTable {
		return annotatedTable{orm.TableOf(b, &n.ID), orm.NumberOf(b, &n.ParentID), orm.NumberOf(b, &n.Amount), orm.ManyOf(b, &n.Children)}
	}, orm.WithRetrievalHooks(func() orm.RetrievalHooks[annotatedNode] {
		factories++
		return orm.RetrievalHooks[annotatedNode]{Retrieved: func(ctx context.Context, ex database.Executor, n annotatedNode) error {
			calls++
			_, err := ex.Exec(ctx, "SELECT 1")
			return err
		}}
	}))
	rel := orm.HasManyOf(table.Children, table.Key(), table.ParentID)
	count := expr.RelatedValue(rel, expr.Count[annotatedNode]())
	sum := expr.RelatedValue(rel, expr.Sum(table.Amount))
	q := expr.OrderBy(expr.Where(table.Query().With(rel), expr.OrderRow(count).Gte(1)), count.Desc())
	must(t, db.Session(ctx, func(session *database.Session) error {
		got, err := rows.WithValue(q, sum).All(ctx, session)
		if err != nil {
			return err
		}
		if len(got) != 1 || got[0].Model.ID != 1 {
			t.Fatal(got)
		}
		total, present := got[0].Value.Get()
		if !present || total.String() != "8" {
			t.Fatal(got[0].Value)
		}
		children, loaded := got[0].Model.Children.Get()
		if !loaded || len(children) != 2 || calls != 3 || factories != 2 {
			t.Fatal(children, loaded, calls, factories)
		}
		empty, err := rows.WithValue(table.Query().Where(table.Key().Eq(3)), sum).All(ctx, session)
		if err != nil {
			return err
		}
		if len(empty) != 1 || !empty[0].Value.IsNull() {
			t.Fatal(empty)
		}
		zero, err := rows.WithValue(table.Query().Where(table.Key().Eq(3)), count).All(ctx, session)
		if err != nil {
			return err
		}
		if len(zero) != 1 || zero[0].Value != 0 {
			t.Fatal(zero)
		}
		_, err = session.Exec(ctx, "SELECT 1")
		return err
	}))
}
