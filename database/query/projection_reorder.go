package query

import (
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

// ReorderUnwindowed replaces outer ordering for an interactive report. It
// requires an unpaginated source and rejects outer DISTINCT ON, whose ordering
// chooses winners. Materialize such a query first to sort its completed result.
// Nested input ordering/windows, joins, predicates, grouping and soft-delete
// visibility are preserved. Supply a unique final tie-breaker for stable pages.
func (q ProjectionQuery[S, P]) ReorderUnwindowed(orders ...ProjectionOrder[S]) ProjectionQuery[S, P] {
	if q.source.node.Limit.IsSet() || q.source.node.Offset != 0 || q.source.node.Distinct.Kind == distinctOn || len(orders) == 0 {
		q.source.err = safeerror.NewFault(fault.Invalid, "report ordering requires an unwindowed source without outer DISTINCT ON and explicit orders")
		return q
	}
	q.source.node.Orders = nil
	return q.OrderBy(orders...)
}
