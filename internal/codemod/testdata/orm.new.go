package consumer

import o "github.com/weiloon1234/Foundry-Go/database/orm"

type Model struct{}
type Tables struct{ o.Table[Model, int] }

var table Tables
var q o.Query[Model]
var lq o.LockedQuery[Model]
var name o.Text[Model, string]
var input o.InputText[Model, int, string]
var ctx, db int
var assignments []int
var _ = func(foundryBound o.LockedQuery[Model]) func(foundryArg0 int, foundryArg1 int) int {
	return func(foundryArg0 int, foundryArg1 int) int {
		return o.AllLocked(foundryArg0, foundryArg1, foundryBound)
	}
}(o.Lock(q.Where(o.TextEq(name, "yes"), o.InputTextEq(input, "stored"))).NoWait())(ctx, db)
var _ = o.First(ctx, db, q)
var _ = o.Require(ctx, db, q)
var _ = o.Count(ctx, db, q)
var _ = o.Exists(ctx, db, q)
var _ = o.Each(ctx, db, q, 1)
var _ = o.Chunk(ctx, db, q, 1, 2)
var _ = o.EachChunked(ctx, db, q, 1, 2)
var _ = o.Paginate(ctx, db, q, 1)
var _ = o.SimplePaginate(ctx, db, q, 1)
var _ = o.UpdateAll(ctx, db, q, assignments...)
var _ = o.DeleteAll(ctx, db, q)
var _ = o.ForceDeleteAll(ctx, db, q, 1)
var _ = o.ForShare(q)
var _ = o.ForNoKeyUpdate(q)
var _ = o.ForKeyShare(q)
var _ = o.FirstLocked(ctx, db, lq)
var _ = o.RequireLocked(ctx, db, lq)
var _ = o.EachLocked(ctx, db, lq, 1)
var _ = o.Find(ctx, db, table.Table, 1)
var _ = o.RequireKey(ctx, db, table.Table, 1)
var _ = o.Insert(ctx, db, table.Table, assignments...)
var _ = o.InsertMany(ctx, db, table.Table, assignments)
var _ = o.Restore(ctx, db, table.Table, 1)
var _ = o.Raw(ctx, db, table.Table, "select", assignments...)
var _ = o.Update(ctx, db, table.Query(), table.Key(), 1, assignments...)
var _ = o.Delete(ctx, db, table.Query(), table.Key(), 1)
var _ = o.ForceDelete(ctx, db, table.Query(), 2, table.Key(), 1)
var _ = o.Find(ctx, db, table.Table, 1)
var _ = func(foundryBound o.Query[Model]) func(foundryArg0 int, foundryArg1 int) int {
	return func(foundryArg0 int, foundryArg1 int) int {
		return o.All(foundryArg0, foundryArg1, foundryBound)
	}
}(table.Query().Where(o.TextIn(name, "a", "b")))(ctx, db)
var _ = o.ConflictKey(o.TextExpr(name))
var _ = o.ConflictSet(o.InputTextExpr(input), 1)
var _ = o.Concrete(name)
var _ = o.InputTextSet(input, 42)
