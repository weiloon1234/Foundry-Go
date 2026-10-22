package goway_test

import (
	"context"
	"database/sql/driver"
	"fmt"

	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/model"
)

func (g newGolden) run() {
	check(orm.Check(models.Orders, models.Users, models.LedgerEntries))
	ctx := context.Background()
	db := g.open(ctx)
	defer db.Close(ctx)
	id := must(model.ParseID[models.Order](orderID))
	buyer := must(model.ParseID[models.User](buyerID))
	q := models.Orders.Query()
	f := models.Orders
	window := q.Where(f.TotalCents.Gt(100)).OrderBy(f.TotalCents.Desc()).Limit(5).Offset(2)
	fmt.Fprintln(&g.output, "read")
	g.show(must(window.All(ctx, db)))
	fmt.Fprintln(&g.output, "count")
	g.show(must(window.Count(ctx, db)))
	fmt.Fprintln(&g.output, "exists")
	g.show(must(window.Exists(ctx, db)))
	fmt.Fprintln(&g.output, "find")
	g.show(must(models.Orders.Find(ctx, db, id)))
	fmt.Fprintln(&g.output, "paginate")
	g.show(must(q.Where(f.TotalCents.Gt(100)).OrderBy(f.TotalCents.Desc()).Paginate(ctx, db, orm.Page(2, 5))))
	fmt.Fprintln(&g.output, "for-update")
	check(db.Transaction(ctx, func(tx *database.Tx) error {
		items, err := q.Where(f.ID.Eq(id)).ForUpdate().All(ctx, tx)
		if err == nil {
			g.show(items)
		}
		return err
	}))
	fmt.Fprintln(&g.output, "for-share")
	check(db.Transaction(ctx, func(tx *database.Tx) error {
		items, err := q.Where(f.ID.Eq(id)).ForShare().All(ctx, tx)
		if err == nil {
			g.show(items)
		}
		return err
	}))
	fmt.Fprintln(&g.output, "insert")
	g.show(must(models.Orders.Insert(ctx, db, f.ID.Set(id), f.BuyerID.Set(buyer), f.TotalCents.Set(500))))
	fmt.Fprintln(&g.output, "update")
	g.show(must(models.Orders.Update(ctx, db, id, f.TotalCents.Set(700))))
	fmt.Fprintln(&g.output, "update-all")
	g.show(must(q.Where(f.TotalCents.Gt(100)).UpdateAll(ctx, db, f.TotalCents.Set(0))))
	fmt.Fprintln(&g.output, "delete")
	g.show(must(models.Orders.Delete(ctx, db, id)))
	fmt.Fprintln(&g.output, "delete-all")
	g.show(must(q.Where(f.TotalCents.Gt(100)).DeleteAll(ctx, db)))
	fmt.Fprintln(&g.output, "user")
	uf := models.Users
	g.show(must(uf.Query().Where(uf.Email.Contains("a_%"), uf.Nickname.IsNull(), uf.Status.Eq(models.StatusActive)).All(ctx, db)))
	fmt.Fprintln(&g.output, "ledger")
	g.show(must(models.LedgerEntries.Query().Where(models.LedgerEntries.Amount.Gte(must(decimal.Parse("12.50")))).All(ctx, db)))

	fmt.Fprintln(&g.output, "first")
	g.show(must(q.First(ctx, db)))
	fmt.Fprintln(&g.output, "require")
	g.show(must(q.Require(ctx, db)))
	fmt.Fprintln(&g.output, "locked-first")
	check(db.Transaction(ctx, func(tx *database.Tx) error {
		items, err := q.ForUpdate().First(ctx, tx)
		if err == nil {
			g.show(items)
		}
		return err
	}))
	fmt.Fprintln(&g.output, "locked-require")
	check(db.Transaction(ctx, func(tx *database.Tx) error {
		items, err := q.ForShare().Require(ctx, tx)
		if err == nil {
			g.show(items)
		}
		return err
	}))
	fmt.Fprintln(&g.output, "comparisons")
	g.show(must(q.Where(f.TotalCents.Ne(9), f.TotalCents.In(1, 2), f.TotalCents.NotIn(3, 4), f.TotalCents.Between(1, 100), f.TotalCents.Lt(90), f.TotalCents.Lte(80)).All(ctx, db)))
	fmt.Fprintln(&g.output, "text-and-boolean")
	g.show(must(models.Users.Query().Where(uf.Nickname.IsNotNull(), orm.Or(uf.Email.StartsWith("a_%"), uf.Email.EndsWith("test")), orm.Not(uf.Email.Like("x%")), orm.And(uf.Email.ILike("A%"), uf.Age.Gt(0))).OrderBy(uf.Nickname.Asc().NullsFirst()).All(ctx, db)))
	fmt.Fprintln(&g.output, "simple-paginate")
	g.show(must(q.SimplePaginate(ctx, db, orm.Page(1, 2))))
	fmt.Fprintln(&g.output, "insert-generated-key")
	g.generatedKey = true
	g.show(must(f.Insert(ctx, db, f.BuyerID.Set(buyer), f.TotalCents.Set(5))))
	g.generatedKey = false
	fmt.Fprintln(&g.output, "insert-defaults")
	g.show(must(models.WriteRecords.Insert(ctx, db, models.WriteRecords.Name.Set("default"))))
	fmt.Fprintln(&g.output, "set-null")
	g.show(must(models.Users.Update(ctx, db, buyer, uf.Nickname.SetNull())))
	fmt.Fprintln(&g.output, "insert-many")
	g.show(must(f.InsertMany(ctx, db, [][]orm.Assignment[models.Order]{{f.ID.Set(id), f.BuyerID.Set(buyer), f.TotalCents.Set(5)}, {f.ID.Set(id), f.BuyerID.Set(buyer), f.TotalCents.Set(6)}})))
	fmt.Fprintln(&g.output, "increment")
	g.show(must(q.Where(f.ID.Eq(id)).UpdateAll(ctx, db, f.TotalCents.Increment(5))))

	fmt.Fprintln(&g.output, "chunk-keyset")
	g.chunkBatches = [][][]driver.Value{
		{{orderID, buyerID, int64(500)}, {"01890f47-1a00-7000-8000-000000000003", buyerID, int64(600)}},
		{{"01890f47-1a00-7000-8000-000000000004", buyerID, int64(700)}},
	}
	check(q.Where(f.TotalCents.Gt(100)).OrderBy(f.TotalCents.Asc()).Limit(3).Offset(1).Chunk(ctx, db, 2, func(items []models.Order) error { g.show(items); return nil }))
	if len(g.chunkBatches) != 0 {
		panic("chunk did not consume canned windows")
	}
	g.chunkBatches = nil
	fmt.Fprintln(&g.output, "each-chunked")
	check(q.Limit(1).EachChunked(ctx, db, 2, func(item models.Order) error { g.show(item); return nil }))
	g.stage1b(ctx, db)
}
