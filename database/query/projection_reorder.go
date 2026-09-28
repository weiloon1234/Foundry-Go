package query

import "github.com/weiloon1234/Foundry-Go/fault"

// ReorderUnwindowed replaces outer ordering for an interactive report. It
// requires an unpaginated source and rejects outer DISTINCT ON, whose ordering
// chooses winners. Materialize such a query first to sort its completed result.
// Nested input ordering/windows, joins, predicates, grouping and soft-delete
// visibility are preserved. Supply a unique final tie-breaker for stable pages.
func (q ProjectionQuery[S, P]) ReorderUnwindowed(orders ...ProjectionOrder[S]) ProjectionQuery[S, P] {
	if q.source.node.limit.IsSet() || q.source.node.offset != 0 || q.source.node.distinct.kind == distinctOn || len(orders) == 0 {
		q.source.err = fault.New(fault.Invalid, "report ordering requires an unwindowed source without outer DISTINCT ON and explicit orders")
		return q
	}
	q.source.node.orders = nil
	return q.OrderBy(orders...)
}
