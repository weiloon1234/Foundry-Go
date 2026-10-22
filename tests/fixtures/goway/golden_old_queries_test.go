package goway_test

import (
	"context"
	"database/sql/driver"
	"fmt"

	models "foundry.test/goway/oldmodels"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/model"
)

func (g oldGolden) run() {
	ctx := context.Background()
	db := g.open(ctx)
	defer db.Close(ctx)
	id := must(model.ParseID[models.Order](orderID))
	buyer := must(model.ParseID[models.User](buyerID))
	q := models.QueryOrders()
	f := models.OrderFields()
	window := q.Where(f.TotalCents.Gt(100)).OrderBy(f.TotalCents.Desc()).Limit(5).Offset(2)
	fmt.Fprintln(&g.output, "read")
	g.show(must(window.All(ctx, db)))
	fmt.Fprintln(&g.output, "count")
	g.show(must(window.Count(ctx, db)))
	fmt.Fprintln(&g.output, "exists")
	g.show(must(window.Exists(ctx, db)))
	fmt.Fprintln(&g.output, "find")
	g.show(must(q.Find(ctx, db, id)))
	fmt.Fprintln(&g.output, "paginate")
	g.show(must(q.Where(f.TotalCents.Gt(100)).OrderBy(f.TotalCents.Desc()).Paginate(ctx, db, query.PageRequest{Number: 2, Size: 5})))
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
	g.show(must(q.Create(ctx, db, models.OrderDraft{}.SetID(id).SetBuyerID(buyer).SetTotalCents(500))))
	fmt.Fprintln(&g.output, "update")
	g.show(must(q.Update(ctx, db, id, models.OrderDraft{}.SetTotalCents(700))))
	fmt.Fprintln(&g.output, "update-all")
	g.show(must(q.Where(f.TotalCents.Gt(100)).UpdateAll(ctx, db, models.OrderDraft{}.SetTotalCents(0))))
	fmt.Fprintln(&g.output, "delete")
	g.show(must(q.Delete(ctx, db, id)))
	fmt.Fprintln(&g.output, "delete-all")
	g.show(must(q.Where(f.TotalCents.Gt(100)).DeleteAll(ctx, db)))
	fmt.Fprintln(&g.output, "user")
	uf := models.UserFields()
	g.show(must(models.QueryUsers().Where(uf.Email.Contains("a_%"), uf.Nickname.IsNull(), uf.Status.Eq(models.StatusActive)).All(ctx, db)))
	fmt.Fprintln(&g.output, "ledger")
	g.show(must(models.QueryLedgerEntries().Where(models.LedgerEntryFields().Amount.Gte(must(decimal.Parse("12.50")))).All(ctx, db)))

	fmt.Fprintln(&g.output, "first")
	g.show(must(q.First(ctx, db)))
	fmt.Fprintln(&g.output, "require")
	g.show(must(q.RequireFirst(ctx, db)))
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
		items, err := q.ForShare().RequireFirst(ctx, tx)
		if err == nil {
			g.show(items)
		}
		return err
	}))
	fmt.Fprintln(&g.output, "comparisons")
	g.show(must(q.Where(f.TotalCents.Ne(9), f.TotalCents.In(1, 2), f.TotalCents.NotIn(3, 4), f.TotalCents.Between(1, 100), f.TotalCents.Lt(90), f.TotalCents.Lte(80)).All(ctx, db)))
	fmt.Fprintln(&g.output, "text-and-boolean")
	g.show(must(models.QueryUsers().Where(uf.Nickname.IsNotNull(), query.Or(uf.Email.StartsWith("a_%"), uf.Email.EndsWith("test")), uf.Email.Like("x%").Not(), query.And(uf.Email.ILike("A%"), uf.Age.Gt(0))).OrderBy(uf.Nickname.Asc().NullsFirst()).All(ctx, db)))
	fmt.Fprintln(&g.output, "simple-paginate")
	g.show(must(q.SimplePaginate(ctx, db, query.PageRequest{Number: 1, Size: 2})))
	fmt.Fprintln(&g.output, "insert-generated-key")
	g.generatedKey = true
	g.show(must(q.Create(ctx, db, models.OrderDraft{}.SetBuyerID(buyer).SetTotalCents(5))))
	g.generatedKey = false
	fmt.Fprintln(&g.output, "insert-defaults")
	g.show(must(models.QueryWriteRecords().Create(ctx, db, models.WriteRecordDraft{}.SetName("default"))))
	fmt.Fprintln(&g.output, "set-null")
	g.show(must(models.QueryUsers().Update(ctx, db, buyer, models.UserDraft{}.ClearNickname())))
	fmt.Fprintln(&g.output, "insert-many")
	g.show(must(q.CreateMany(ctx, db, []models.OrderDraft{models.OrderDraft{}.SetID(id).SetBuyerID(buyer).SetTotalCents(5), models.OrderDraft{}.SetID(id).SetBuyerID(buyer).SetTotalCents(6)})))
	fmt.Fprintln(&g.output, "increment")
	g.show(must(q.Where(f.ID.Eq(id)).Increment(ctx, db, f.TotalCents.By(5))))

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
