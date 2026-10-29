package goway_test

import (
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
	ctx := g.context()
	db := g.open(ctx)
	defer db.Close(ctx)
	id := must(model.ParseID[models.Order](orderID))
	buyer := must(model.ParseID[models.User](buyerID))
	q := models.Orders.Query()
	f := models.Orders
	window := q.Where(orm.NumberGt(f.TotalCents, 100)).OrderBy(orm.NumberDesc(f.TotalCents)).Limit(5).Offset(2)
	fmt.Fprintln(&g.output, "read")
	g.show(must(orm.All(ctx, db, window)))
	fmt.Fprintln(&g.output, "count")
	g.show(must(orm.Count(ctx, db, window)))
	fmt.Fprintln(&g.output, "exists")
	g.show(must(orm.Exists(ctx, db, window)))
	fmt.Fprintln(&g.output, "find")
	g.show(must(orm.Find(ctx, db, models.Orders.Table, id)))
	fmt.Fprintln(&g.output, "paginate")
	g.show(must(orm.Paginate(ctx, db, q.Where(orm.NumberGt(f.TotalCents, 100)).OrderBy(orm.NumberDesc(f.TotalCents)), orm.Page(2, 5))))
	fmt.Fprintln(&g.output, "for-update")
	check(db.Transaction(ctx, func(tx *database.Tx) error {
		items, err := orm.AllLocked(ctx, tx, orm.Lock(q.Where(orm.KeyEq(f.ID, id))))
		if err == nil {
			g.show(items)
		}
		return err
	}))
	fmt.Fprintln(&g.output, "for-share")
	check(db.Transaction(ctx, func(tx *database.Tx) error {
		items, err := orm.AllLocked(ctx, tx, orm.ForShare(q.Where(orm.KeyEq(f.ID, id))))
		if err == nil {
			g.show(items)
		}
		return err
	}))
	fmt.Fprintln(&g.output, "insert")
	g.show(must(orm.Insert(ctx, db, models.Orders.Table, orm.KeySet(f.ID, id), orm.ColumnSet(f.BuyerID, buyer), orm.NumberSet(f.TotalCents, 500))))
	fmt.Fprintln(&g.output, "update")
	g.show(must(orm.Update(ctx, db, models.Orders.Table.Query(), models.Orders.Table.Key(), id, orm.NumberSet(f.TotalCents, 700))))
	fmt.Fprintln(&g.output, "update-all")
	g.show(must(orm.UpdateAll(ctx, db, q.Where(orm.NumberGt(f.TotalCents, 100)), orm.NumberSet(f.TotalCents, 0))))
	fmt.Fprintln(&g.output, "delete")
	g.show(must(orm.Delete(ctx, db, models.Orders.Table.Query(), models.Orders.Table.Key(), id)))
	fmt.Fprintln(&g.output, "delete-all")
	g.show(must(orm.DeleteAll(ctx, db, q.Where(orm.NumberGt(f.TotalCents, 100)))))
	fmt.Fprintln(&g.output, "user")
	uf := models.Users
	g.show(must(orm.All(ctx, db, uf.Query().Where(orm.TextContains(uf.Email, "a_%"), orm.NullableTextIsNull(uf.Nickname), orm.EnumEq(uf.Status, models.StatusActive)))))
	fmt.Fprintln(&g.output, "ledger")
	g.show(must(orm.All(ctx, db, models.LedgerEntries.Query().Where(orm.NumberGte(models.LedgerEntries.Amount, must(decimal.Parse("12.50")))))))

	fmt.Fprintln(&g.output, "first")
	g.show(must(orm.First(ctx, db, q)))
	fmt.Fprintln(&g.output, "require")
	g.show(must(orm.Require(ctx, db, q)))
	fmt.Fprintln(&g.output, "locked-first")
	check(db.Transaction(ctx, func(tx *database.Tx) error {
		items, err := orm.FirstLocked(ctx, tx, orm.Lock(q))
		if err == nil {
			g.show(items)
		}
		return err
	}))
	fmt.Fprintln(&g.output, "locked-require")
	check(db.Transaction(ctx, func(tx *database.Tx) error {
		items, err := orm.RequireLocked(ctx, tx, orm.ForShare(q))
		if err == nil {
			g.show(items)
		}
		return err
	}))
	fmt.Fprintln(&g.output, "comparisons")
	g.show(must(orm.All(ctx, db, q.Where(orm.NumberNe(f.TotalCents, 9), orm.NumberIn(f.TotalCents, 1, 2), orm.NumberNotIn(f.TotalCents, 3, 4), orm.NumberBetween(f.TotalCents, 1, 100), orm.NumberLt(f.TotalCents, 90), orm.NumberLte(f.TotalCents, 80)))))
	fmt.Fprintln(&g.output, "text-and-boolean")
	g.show(must(orm.All(ctx, db, models.Users.Query().Where(orm.NullableTextIsNotNull(uf.Nickname), orm.Or(orm.TextStartsWith(uf.Email, "a_%"), orm.TextEndsWith(uf.Email, "test")), orm.Not(orm.TextLike(uf.Email, "x%")), orm.And(orm.TextILike(uf.Email, "A%"), orm.NumberGt(uf.Age, 0))).OrderBy(orm.NullableTextAsc(uf.Nickname).NullsFirst()))))
	fmt.Fprintln(&g.output, "simple-paginate")
	g.show(must(orm.SimplePaginate(ctx, db, q, orm.Page(1, 2))))
	fmt.Fprintln(&g.output, "insert-generated-key")
	g.generatedKey = true
	g.show(must(orm.Insert(ctx, db, f.Table, orm.ColumnSet(f.BuyerID, buyer), orm.NumberSet(f.TotalCents, 5))))
	g.generatedKey = false
	fmt.Fprintln(&g.output, "insert-defaults")
	g.show(must(orm.Insert(ctx, db, models.WriteRecords.Table, orm.TextSet(models.WriteRecords.Name, "default"))))
	fmt.Fprintln(&g.output, "set-null")
	g.show(must(orm.Update(ctx, db, models.Users.Table.Query(), models.Users.Table.Key(), buyer, orm.NullableTextSetNull(uf.Nickname))))
	fmt.Fprintln(&g.output, "insert-many")
	g.show(must(orm.InsertMany(ctx, db, f.Table, [][]orm.Assignment[models.Order]{{orm.KeySet(f.ID, id), orm.ColumnSet(f.BuyerID, buyer), orm.NumberSet(f.TotalCents, 5)}, {orm.KeySet(f.ID, id), orm.ColumnSet(f.BuyerID, buyer), orm.NumberSet(f.TotalCents, 6)}})))
	fmt.Fprintln(&g.output, "increment")
	g.show(must(orm.UpdateAll(ctx, db, q.Where(orm.KeyEq(f.ID, id)), orm.NumberIncrement(f.TotalCents, 5))))

	fmt.Fprintln(&g.output, "chunk-keyset")
	g.chunkBatches = [][][]driver.Value{
		{{orderID, buyerID, int64(500)}, {"01890f47-1a00-7000-8000-000000000003", buyerID, int64(600)}},
		{{"01890f47-1a00-7000-8000-000000000004", buyerID, int64(700)}},
	}
	check(orm.Chunk(ctx, db, q.Where(orm.NumberGt(f.TotalCents, 100)).OrderBy(orm.NumberAsc(f.TotalCents)).Limit(3).Offset(1), 2, func(items []models.Order) error { g.show(items); return nil }))
	if len(g.chunkBatches) != 0 {
		panic("chunk did not consume canned windows")
	}
	g.chunkBatches = nil
	fmt.Fprintln(&g.output, "each-chunked")
	check(orm.EachChunked(ctx, db, q.Limit(1), 2, func(item models.Order) error { g.show(item); return nil }))
	g.stage1b(ctx, db)
}
