package query

import (
	"context"
	"database/sql/driver"
	"math"
	"slices"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Complete model records keep their original selection expressions through
// aliases/CTEs/joins. Append the stored primary expression as a tie-breaker while
// preserving user order and nested query windows. DTOs retain streaming reads.
// When the result is ordered only by its primary key over plain columns, later
// batches continue after the last delivered key; other orders use OFFSET.
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
	keyed := q.lifecycle.primaryKey != nil && primaryKeyset(q.node, primary)
	predicates := q.node.predicates
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
		if more && keyed {
			key, err := q.lifecycle.primaryKey(items[len(items)-1])
			if err != nil {
				return err
			}
			if key == nil {
				return fault.New(fault.Invalid, "retrieval batch primary key cannot be NULL")
			}
			op := greater
			if q.node.orders[0].descending {
				op = less
			}
			after := comparison{operand: primary, operator: op, values: []any{key}, bind: func(raw any) (driver.Value, error) { return raw, nil }}
			q.node.predicates = append(slices.Clip(predicates), after)
			q.node.offset = 0
		} else if more {
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

// primaryKeyset allows key continuation only when the primary key is the sole
// order, keys are unique in the result (no joins can repeat them) and a WHERE
// bound cannot change which rows or values are selected: every selection is a
// plain column and nothing groups or deduplicates rows.
func primaryKeyset(node selectNode, primary valueExpression) bool {
	key, ok := primary.(fieldRef)
	if !ok || len(node.orders) != 1 || node.orders[0].nulls != nullsDefault || len(node.joins) != 0 || len(node.groupBy) != 0 || len(node.having) != 0 || node.distinct.kind != noDistinct {
		return false
	}
	if field, ok := node.orders[0].expression.(fieldRef); !ok || field != key {
		return false
	}
	for _, selection := range node.selections {
		if _, ok := selection.expression.(fieldRef); !ok {
			return false
		}
	}
	return true
}
