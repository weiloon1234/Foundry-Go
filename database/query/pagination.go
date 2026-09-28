package query

import (
	"context"
	"math"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
)

// MaxPageSize bounds returned rows for numbered, simple and cursor pagination.
const MaxPageSize = 1000

// PageRequest selects a one-based page. Size must be between 1 and MaxPageSize.
// Invalid requests fail rather than being silently clamped.
type PageRequest struct{ Number, Size int }

// Page contains complete results and an unpaginated matching count. Count and
// rows are separate queries; use a repeatable-read transaction for one snapshot.
type Page[M any] struct {
	Items        []M
	Number, Size int
	Total, Pages int64
}

// SimplePage contains complete results without a total count. HasMore indicates
// an extra row existed after this page in the read's snapshot. Number > 1 permits
// backward navigation; neither direction guarantees rows survive a later request.
type SimplePage[R any] struct {
	Items        []R
	Number, Size int
	HasMore      bool
}

func validPageSize(size int) bool { return size > 0 && size <= MaxPageSize }

func (r PageRequest) offset() (int, error) {
	if r.Number < 1 || !validPageSize(r.Size) || r.Number-1 > math.MaxInt/r.Size {
		return 0, fault.New(fault.Invalid, "invalid page number, size or offset overflow")
	}
	return (r.Number - 1) * r.Size, nil
}

// Validate checks number, size and offset representability without database I/O.
// Transport adapters reuse this contract rather than computing offsets.
func (r PageRequest) Validate() error { _, err := r.offset(); return err }

func pageCount(total int64, size int) int64 {
	pages := total / int64(size)
	if total%int64(size) != 0 {
		pages++
	}
	return pages
}

// Validate checks page metadata and the bounded item count. It does not assert
// that Total and Items came from one snapshot: counting and reading may observe
// different database states. Empty and beyond-last pages remain valid.
func (p Page[R]) Validate() error {
	if err := (PageRequest{Number: p.Number, Size: p.Size}).Validate(); err != nil {
		return err
	}
	if p.Total < 0 || p.Pages != pageCount(p.Total, p.Size) || len(p.Items) > p.Size {
		return fault.New(fault.Invalid, "invalid numbered page metadata")
	}
	return nil
}

// Validate checks a bounded simple page without fabricating totals. HasMore
// requires a full page because its hidden lookahead row was removed by the read.
func (p SimplePage[R]) Validate() error {
	if err := (PageRequest{Number: p.Number, Size: p.Size}).Validate(); err != nil {
		return err
	}
	if len(p.Items) > p.Size || p.HasMore && len(p.Items) != p.Size {
		return fault.New(fault.Invalid, "invalid simple page metadata")
	}
	return nil
}

// paginationBase preserves filters and order, appending the primary key as the
// final ascending tie-breaker when absent. Existing windows are ambiguous here.
func (q Query[M]) paginationBase() (Query[M], error) {
	q, err := q.stableModelOrder()
	if err != nil {
		return Query[M]{}, err
	}
	if q.limit.IsSet() || q.offset != 0 {
		return Query[M]{}, fault.New(fault.Invalid, "pagination requires an unpaginated query")
	}
	return q, nil
}

// stableModelOrder is shared by numbered/cursor pages and bounded chunk reads.
func (q Query[M]) stableModelOrder() (Query[M], error) {
	if err := q.Validate(); err != nil {
		return Query[M]{}, err
	}
	if q.definition == nil {
		return Query[M]{}, fault.New(fault.Invalid, "ordered model reads require model metadata")
	}
	seen := make(map[string]bool, len(q.orders))
	for _, order := range q.orders {
		// A computed key does not prove primary-key uniqueness. Preserve it and
		// append the actual primary field as the final tie-breaker below.
		if order.computed != nil {
			continue
		}
		if seen[order.field.column] {
			return Query[M]{}, fault.New(fault.Invalid, "ordered model reads cannot repeat an ordered column")
		}
		seen[order.field.column] = true
	}
	if !seen[q.definition.primary] {
		q = q.OrderBy(Order[M]{field: fieldRef{q.table, q.definition.primary}})
	}
	return q, nil
}

// Paginate reads one bounded page with an unpaginated total. It rejects existing
// Limit/Offset clauses, preserves filters, and stabilizes ordering with the key.
func (q Query[M]) Paginate(ctx context.Context, executor database.Executor, request PageRequest) (Page[M], error) {
	if err := executionContext(ctx, executor); err != nil {
		return Page[M]{}, err
	}
	offset, err := request.offset()
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
	countQuery.orders = nil
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
	offset, err := request.offset()
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
