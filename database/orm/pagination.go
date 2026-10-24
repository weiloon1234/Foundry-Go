package orm

import (
	"context"
	"unsafe"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/value"
)

// PageRequest retains the existing pagination validation contract. Example: Page(2,20).
type PageRequest = querycore.PageRequest

// PageResult retains the existing pagination response contract. Example: page.Items.
type PageResult[M any] = querycore.Page[M]

// Page selects a one-based page. Example: Orders.Query().Paginate(ctx,db,Page(2,20)).
func Page(number, size int) PageRequest { return PageRequest{Number: number, Size: size} }

// Paginate reads a matching total and a stable page. Example: Orders.Query().Paginate(ctx,db,Page(1,20)).
func (q Query[M]) Paginate(ctx context.Context, ex database.Executor, request PageRequest) (PageResult[M], error) {
	return paginate[M](ctx, ex, q.state, request)
}
func pageStates(q queryState, r PageRequest) (queryState, queryState, error) {
	offset, err := querycore.PageOffset(r)
	if err != nil {
		return queryState{}, queryState{}, err
	}
	if err := validateQuery(q); err != nil {
		return queryState{}, queryState{}, err
	}
	if q.ast.Limit.IsSet() || q.ast.Offset != 0 {
		return queryState{}, queryState{}, invalid("pagination requires an unpaginated query")
	}
	q.ast, err = q.ast.StableOrder()
	if err != nil {
		return queryState{}, queryState{}, err
	}
	count := q
	count.ast.Orders = nil
	q.ast.Limit = value.Set(r.Size)
	q.ast.Offset = offset
	if _, err = compileRead(q, querycore.ReadModels); err != nil {
		return queryState{}, queryState{}, err
	}
	return count, q, nil
}
func paginate[M any](ctx context.Context, ex database.Executor, q queryState, r PageRequest) (PageResult[M], error) {
	if err := executionContext(ctx, ex); err != nil {
		return PageResult[M]{}, err
	}
	q.ast.ScopeContext = ctx
	base, window, err := pageStates(q, r)
	if err != nil {
		return PageResult[M]{}, err
	}
	total, err := count(ctx, ex, base)
	if err != nil {
		return PageResult[M]{}, err
	}
	if total < 0 {
		return PageResult[M]{}, invalid("database returned a negative page count")
	}
	items, err := all[M](ctx, ex, window)
	if err != nil {
		return PageResult[M]{}, err
	}
	pages := querycore.PageCount(total, r.Size)
	return PageResult[M]{Items: items, Number: r.Number, Size: r.Size, Total: total, Pages: pages}, nil
}

// SimplePage retains the existing page-without-total response contract.
type SimplePage[M any] = querycore.SimplePage[M]

// SimplePaginate fetches one extra row to determine whether another page exists.
func (q Query[M]) SimplePaginate(ctx context.Context, ex database.Executor, request PageRequest) (SimplePage[M], error) {
	return simplePaginate[M](ctx, ex, q.state, request)
}
func simplePaginate[M any](ctx context.Context, ex database.Executor, q queryState, r PageRequest) (SimplePage[M], error) {
	if err := executionContext(ctx, ex); err != nil {
		return SimplePage[M]{}, err
	}
	q.ast.ScopeContext = ctx
	_, window, err := pageStates(q, r)
	if err != nil {
		return SimplePage[M]{}, err
	}
	window.ast.Limit = value.Set(r.Size + 1)
	items, err := allRows[M](ctx, ex, window)
	if err != nil {
		return SimplePage[M]{}, err
	}
	more := len(items) > r.Size
	if more {
		clear(items[r.Size:])
		items = items[:r.Size:r.Size]
	}
	if err := loadPlans(ctx, ex, unsafe.Pointer(unsafe.SliceData(items)), len(items), window.relations); err != nil {
		return SimplePage[M]{}, err
	}
	return SimplePage[M]{Items: items, Number: r.Number, Size: r.Size, HasMore: more}, nil
}
