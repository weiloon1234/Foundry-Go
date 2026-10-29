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
var _ = q.Where(name.Eq("yes"), input.Eq("stored")).ForUpdate().NoWait().All(ctx, db)
var _ = q.First(ctx, db)
var _ = q.Require(ctx, db)
var _ = q.Count(ctx, db)
var _ = q.Exists(ctx, db)
var _ = q.Each(ctx, db, 1)
var _ = q.Chunk(ctx, db, 1, 2)
var _ = q.EachChunked(ctx, db, 1, 2)
var _ = q.Paginate(ctx, db, 1)
var _ = q.SimplePaginate(ctx, db, 1)
var _ = q.UpdateAll(ctx, db, assignments...)
var _ = q.DeleteAll(ctx, db)
var _ = q.ForceDeleteAll(ctx, db, 1)
var _ = q.ForShare()
var _ = q.ForNoKeyUpdate()
var _ = q.ForKeyShare()
var _ = lq.First(ctx, db)
var _ = lq.Require(ctx, db)
var _ = lq.Each(ctx, db, 1)
var _ = table.Find(ctx, db, 1)
var _ = table.Require(ctx, db, 1)
var _ = table.Insert(ctx, db, assignments...)
var _ = table.InsertMany(ctx, db, assignments)
var _ = table.Restore(ctx, db, 1)
var _ = table.Raw(ctx, db, "select", assignments...)
var _ = table.Update(ctx, db, 1, assignments...)
var _ = table.Delete(ctx, db, 1)
var _ = table.ForceDelete(ctx, db, 2, 1)
var _ = table.Table.Find(ctx, db, 1)
var _ = table.Query().Where(name.In("a", "b")).All(ctx, db)
var _ = o.ConflictKey(name)
var _ = o.ConflictSet(input, 1)
var _ = o.Concrete(name)
var _ = input.Set(42)
