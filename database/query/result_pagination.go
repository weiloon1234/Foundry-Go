package query

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
)

func (q readResult[R]) pageWindow(request PageRequest, lookahead bool) (readResult[R], error) {
	offset, err := request.offset()
	if err != nil {
		return readResult[R]{}, err
	}
	if q.node.limit.IsSet() || q.node.offset != 0 || len(q.node.orders) == 0 {
		return readResult[R]{}, fault.New(fault.Invalid, "result pagination requires explicit ordering and no outer Limit/Offset")
	}
	size := request.Size
	if lookahead {
		size++
	}
	q.node.limit = value.Set(size)
	q.node.offset = offset
	if _, err := q.Compile(); err != nil {
		return readResult[R]{}, err
	}
	return q, nil
}

func (q readResult[R]) Paginate(ctx context.Context, executor database.Executor, request PageRequest) (Page[R], error) {
	if err := executionContext(ctx, executor); err != nil {
		return Page[R]{}, err
	}
	window, err := q.pageWindow(request, false)
	if err != nil {
		return Page[R]{}, err
	}
	// Preserve the entire selection, including grouping, distinctness and orders
	// that choose DISTINCT ON winners. Only the new outer window is omitted.
	return readPage(request, func() (int64, error) { return q.Count(ctx, executor) },
		func() ([]R, error) { return window.All(ctx, executor) })
}

func (q readResult[R]) SimplePaginate(ctx context.Context, executor database.Executor, request PageRequest) (SimplePage[R], error) {
	if err := executionContext(ctx, executor); err != nil {
		return SimplePage[R]{}, err
	}
	window, err := q.pageWindow(request, true)
	if err != nil {
		return SimplePage[R]{}, err
	}
	items, err := window.All(ctx, executor)
	if err != nil {
		return SimplePage[R]{}, err
	}
	items, more := trimPageLookahead(items, request.Size)
	return SimplePage[R]{Items: items, Number: request.Number, Size: request.Size, HasMore: more}, nil
}

// Paginate returns a numbered page and total of complete projected results.
// Explicit outer ordering is required; choose keys that uniquely order results.
// Existing outer Limit/Offset clauses are rejected. Nested input windows remain.
func (q ProjectionQuery[S, P]) Paginate(ctx context.Context, executor database.Executor, request PageRequest) (Page[P], error) {
	return q.reader().Paginate(ctx, executor, request)
}

// SimplePaginate reads a projected page with a lookahead row and no count.
// It has the same explicit ordering/window requirements as Paginate.
func (q ProjectionQuery[S, P]) SimplePaginate(ctx context.Context, executor database.Executor, request PageRequest) (SimplePage[P], error) {
	return q.reader().SimplePaginate(ctx, executor, request)
}

// Paginate returns a numbered page and total of the ordered combined result.
func (q SetQuery[R]) Paginate(ctx context.Context, executor database.Executor, request PageRequest) (Page[R], error) {
	return q.reader().Paginate(ctx, executor, request)
}

// SimplePaginate reads the ordered combined result with a lookahead and no count.
func (q SetQuery[R]) SimplePaginate(ctx context.Context, executor database.Executor, request PageRequest) (SimplePage[R], error) {
	return q.reader().SimplePaginate(ctx, executor, request)
}

// Paginate returns complete selected values and a total, using explicit ordering.
func (q ValueQuery[S, V]) Paginate(ctx context.Context, executor database.Executor, request PageRequest) (Page[V], error) {
	return q.query.Paginate(ctx, executor, request)
}

// SimplePaginate returns ordered values with a lookahead and no count.
func (q ValueQuery[S, V]) SimplePaginate(ctx context.Context, executor database.Executor, request PageRequest) (SimplePage[V], error) {
	return q.query.SimplePaginate(ctx, executor, request)
}

// Paginate returns ordered combined values with a total count.
func (q ValueSetQuery[V]) Paginate(ctx context.Context, executor database.Executor, request PageRequest) (Page[V], error) {
	return q.query.Paginate(ctx, executor, request)
}

// SimplePaginate returns ordered combined values with a lookahead and no count.
func (q ValueSetQuery[V]) SimplePaginate(ctx context.Context, executor database.Executor, request PageRequest) (SimplePage[V], error) {
	return q.query.SimplePaginate(ctx, executor, request)
}
