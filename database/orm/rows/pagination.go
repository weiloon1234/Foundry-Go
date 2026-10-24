package rows

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

// Paginate reads a matching total and ordered page; use a repeatable-read transaction for one snapshot. Example: rows.Paginate(ctx,tx,q,orm.Page(1,20)).
func Paginate[S, R any](ctx context.Context, ex database.Executor, q Query[S, R], request orm.PageRequest) (orm.PageResult[R], error) {
	return Read(q).Paginate(ctx, ex, request)
}

// SimplePaginate reads size+1 results, discarding the lookahead without counting. Example: rows.SimplePaginate(ctx,db,q,orm.Page(2,20)).
func SimplePaginate[S, R any](ctx context.Context, ex database.Executor, q Query[S, R], request orm.PageRequest) (orm.SimplePage[R], error) {
	return Read(q).SimplePaginate(ctx, ex, request)
}
