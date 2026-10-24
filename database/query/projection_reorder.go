package query

import (
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
)

// ReorderUnwindowed replaces outer ordering for an interactive report. It
// requires an unpaginated source and rejects outer DISTINCT ON, whose ordering
// chooses winners. Materialize such a query first to sort its completed result.
// Nested input ordering/windows, joins, predicates, grouping and soft-delete
// visibility are preserved. Supply a unique final tie-breaker for stable pages.
func (q ProjectionQuery[S, P]) ReorderUnwindowed(orders ...ProjectionOrder[S]) ProjectionQuery[S, P] {
	node, err := querycore.ReorderUnwindowed(q.source.node, len(orders))
	if err != nil {
		q.source.err = err
		return q
	}
	q.source.node = node
	return q.OrderBy(orders...)
}
