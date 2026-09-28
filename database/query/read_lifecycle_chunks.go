package query

import (
	"context"
	"math"
	"slices"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Complete model records keep their original selection expressions through
// aliases/CTEs/joins. Append the stored primary expression as a tie-breaker while
// preserving user order and nested query windows. DTOs retain streaming reads.
func (q readResult[R]) retrievalChunks(ctx context.Context, executor database.Executor, yield func(R) error) error {
	if q.lifecycle == nil || q.lifecycle.primaryIndex < 0 || q.lifecycle.primaryIndex >= len(q.node.selections) {
		return fault.New(fault.Invalid, "retrieval batches require complete model primary metadata")
	}
	q.node.orders = slices.Clone(q.node.orders)
	primary := q.node.selections[q.lifecycle.primaryIndex].expression
	// Complete-model selections retain field references. Compare only those
	// comparable nodes; arbitrary computed expressions can contain slices.
	present := false
	if key, ok := primary.(fieldRef); ok {
		for _, order := range q.node.orders {
			if field, ok := order.expression.(fieldRef); ok && field == key {
				present = true
				break
			}
		}
	}
	if !present {
		q.node.orders = append(q.node.orders, orderNode{expression: primary})
	}
	if _, err := q.Compile(); err != nil {
		return err
	}
	remaining := q.node.limit
	for {
		size := DefaultChunkSize
		if n, bounded := remaining.Get(); bounded {
			size = min(size, n)
		}
		if size == 0 {
			return nil
		}
		q.node.limit = value.Set(size)
		items, err := q.All(ctx, executor)
		if err != nil {
			return err
		}
		if len(items) > size {
			return fault.New(fault.Invalid, "database exceeded the retrieval batch limit")
		}
		if n, bounded := remaining.Get(); bounded {
			remaining = value.Set(n - len(items))
		}
		more := len(items) == size
		if n, bounded := remaining.Get(); bounded && n == 0 {
			more = false
		}
		if more {
			if q.node.offset > math.MaxInt-len(items) {
				return fault.New(fault.Invalid, "retrieval batch offset overflow")
			}
			q.node.offset += len(items)
		}
		if err := eachBatch(ctx, yield)(items); err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if !more {
			return nil
		}
	}
}
