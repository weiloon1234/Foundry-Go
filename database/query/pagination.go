package query

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
)

// MaxPageSize bounds returned rows for numbered, simple and cursor pagination.
const MaxPageSize = querycore.MaxPageSize

// PageRequest selects a one-based page with the shared validation contract.
type PageRequest = querycore.PageRequest

// Page contains complete results and an unpaginated matching count.
type Page[M any] = querycore.Page[M]

// SimplePage contains results and lookahead metadata without a total count.
type SimplePage[R any] = querycore.SimplePage[R]

func validPageSize(size int) bool { return querycore.ValidPageSize(size) }

func pageCount(total int64, size int) int64 { return querycore.PageCount(total, size) }

// paginationBase preserves filters and order, appending the primary key as the
// final ascending tie-breaker when absent. Existing windows are ambiguous here.
func (q Query[M]) paginationBase() (Query[M], error) {
	q, err := q.stableModelOrder()
	if err != nil {
		return Query[M]{}, err
	}
	if q.queryCore.Limit.IsSet() || q.queryCore.Offset != 0 {
		return Query[M]{}, fault.New(fault.Invalid, "pagination requires an unpaginated query")
	}
	return q, nil
}

// stableModelOrder is shared by numbered/cursor pages and bounded chunk reads.
func (q Query[M]) stableModelOrder() (Query[M], error) {
	if err := q.Validate(); err != nil {
		return Query[M]{}, err
	}
	core, err := q.queryCore.StableOrder()
	if err != nil {
		return Query[M]{}, err
	}
	q.queryCore = core
	return q, nil
}

// Paginate reads one bounded page with an unpaginated total. It rejects existing
// Limit/Offset clauses, preserves filters, and stabilizes ordering with the key.
func (q Query[M]) Paginate(ctx context.Context, executor database.Executor, request PageRequest) (Page[M], error) {
	if err := executionContext(ctx, executor); err != nil {
		return Page[M]{}, err
	}
	q = q.inContext(ctx)
	offset, err := querycore.PageOffset(request)
	if err != nil {
		return Page[M]{}, err
	}
	q, err = q.paginationBase()
	if err != nil {
		return Page[M]{}, err
	}
	window := q.Limit(request.Size).Offset(offset)
	// Validate the complete read before the count can touch the database.
	if _, err := window.Compile(); err != nil {
		return Page[M]{}, err
	}
	countQuery := q
	countQuery.queryCore.Orders = nil
	return readPage(request, func() (int64, error) { return countQuery.Count(ctx, executor) },
		func() ([]M, error) { return window.All(ctx, executor) })
}

// readPage owns the shared total/row failure and metadata contract. Callers
// validate the request and the complete window before supplying execution.
func readPage[R any](request PageRequest, count func() (int64, error), read func() ([]R, error)) (Page[R], error) {
	total, err := count()
	if err != nil {
		return Page[R]{}, err
	}
	if total < 0 {
		return Page[R]{}, fault.New(fault.Invalid, "database returned a negative page count")
	}
	items, err := read()
	if err != nil {
		return Page[R]{}, err
	}
	pages := pageCount(total, request.Size)
	return Page[R]{Items: items, Number: request.Number, Size: request.Size, Total: total, Pages: pages}, nil
}

// SimplePaginate reads size plus one rows without counting, preserving filters
// and appending the model key for stable ordering. Eager loads apply only to the
// returned page; the extra row is decoded but its relations are not loaded.
func (q Query[M]) SimplePaginate(ctx context.Context, executor database.Executor, request PageRequest) (SimplePage[M], error) {
	if err := executionContext(ctx, executor); err != nil {
		return SimplePage[M]{}, err
	}
	q = q.inContext(ctx)
	offset, err := querycore.PageOffset(request)
	if err != nil {
		return SimplePage[M]{}, err
	}
	q, err = q.paginationBase()
	if err != nil {
		return SimplePage[M]{}, err
	}
	items, err := q.Limit(request.Size+1).Offset(offset).allRows(ctx, executor)
	if err != nil {
		return SimplePage[M]{}, err
	}
	items, more, err := q.loadPage(ctx, executor, items, request.Size)
	if err != nil {
		return SimplePage[M]{}, err
	}
	return SimplePage[M]{Items: items, Number: request.Number, Size: request.Size, HasMore: more}, nil
}

func trimPageLookahead[R any](items []R, size int) ([]R, bool) {
	more := len(items) > size
	if more {
		// Do not retain references from the hidden row in the backing array.
		clear(items[size:])
		items = items[:size:size]
	}
	return items, more
}

func (q Query[M]) loadPage(ctx context.Context, executor database.Executor, items []M, size int) ([]M, bool, error) {
	items, more := trimPageLookahead(items, size)
	if len(q.relations) != 0 {
		var err error
		items, err = q.Load(ctx, executor, items)
		if err != nil {
			return nil, false, err
		}
	}
	return items, more, nil
}
