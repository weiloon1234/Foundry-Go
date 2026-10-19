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
	if q.lifecycle == nil || q.lifecycle.primaryIndex < 0 || q.lifecycle.primaryIndex >= len(q.node.Selections) {
		return fault.New(fault.Invalid, "retrieval batches require complete model primary metadata")
	}
	q.node.Orders = slices.Clone(q.node.Orders)
	primary := q.node.Selections[q.lifecycle.primaryIndex].Expression
	// Complete-model selections retain field references. Compare only those
	// comparable nodes; arbitrary computed expressions can contain slices.
	present := false
	if key, ok := primary.(fieldRef); ok {
		for _, order := range q.node.Orders {
			if field, ok := order.Expression.(fieldRef); ok && field == key {
				present = true
				break
			}
		}
	}
	if !present {
		q.node.Orders = append(q.node.Orders, orderNode{Expression: primary})
	}
	if _, err := q.Compile(); err != nil {
		return err
	}
	keyed := q.lifecycle.primaryKey != nil && primaryKeyset(q.node, primary)
	predicates := q.node.Predicates
	remaining := q.node.Limit
	for {
		size := DefaultChunkSize
		if n, bounded := remaining.Get(); bounded {
			size = min(size, n)
		}
		if size == 0 {
			return nil
		}
		q.node.Limit = value.Set(size)
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
			if q.node.Orders[0].Descending {
				op = less
			}
			after := comparison{Operand: primary, Operator: op, Values: []any{key}, Bind: func(raw any) (driver.Value, error) { return raw, nil }}
			q.node.Predicates = append(slices.Clip(predicates), after)
			q.node.Offset = 0
		} else if more {
			if q.node.Offset > math.MaxInt-len(items) {
				return fault.New(fault.Invalid, "retrieval batch offset overflow")
			}
			q.node.Offset += len(items)
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
	if !ok || len(node.Orders) != 1 || node.Orders[0].Nulls != nullsDefault || len(node.Joins) != 0 || len(node.GroupBy) != 0 || len(node.Having) != 0 || node.Distinct.Kind != noDistinct {
		return false
	}
	if field, ok := node.Orders[0].Expression.(fieldRef); !ok || field != key {
		return false
	}
	for _, selection := range node.Selections {
		if _, ok := selection.Expression.(fieldRef); !ok {
			return false
		}
	}
	return true
}
