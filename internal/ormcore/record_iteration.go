package ormcore

import (
	"context"
	"database/sql/driver"
	"math"
	"slices"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Iteration owns cancellation and panic/Goexit isolation for result delivery.
func Iteration(ctx context.Context, yield func() error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	var returned error
	if err := callback.Isolated("query iteration", func() error {
		returned = yield()
		return nil
	}); err != nil {
		return err
	}
	if returned != nil {
		return returned
	}
	return ctx.Err()
}

// RecordMayRetrieve consults only validated framework owners. A wrapper cannot
// claim that retrieval callbacks are absent from the rows it will return.
func RecordMayRetrieve(r Record, ex database.Executor) bool {
	if r.Callbacks == nil {
		return false
	}
	set, known := database.FoundryObservers(ex, sqlowner.Observers())
	return !known || r.Callbacks(ex, nil).Needed(set)
}

// RecordBatches shares complete-model iteration between both typed APIs. The
// reader closes and dispatches a whole batch before delivery. Key capture happens
// before delivery; non-keyable orders retain the original offset progression.
func RecordBatches(ctx context.Context, node querycore.SelectNode, primaryIndex int,
	compile func(querycore.SelectNode) error, read func(querycore.SelectNode) (int, error),
	key func() (driver.Value, error), yield func(int) error) error {
	if primaryIndex < 0 || primaryIndex >= len(node.Selections) {
		return Invalid("retrieval batches require complete model primary metadata")
	}
	node.Orders = slices.Clone(node.Orders)
	primary := node.Selections[primaryIndex].Expression
	// Complete-model selections retain field references. Compare only those
	// comparable nodes; arbitrary computed expressions can contain slices.
	present := false
	if key, ok := querycore.UnwrapSensitivity(primary).(querycore.FieldRef); ok {
		for _, order := range node.Orders {
			if field, ok := querycore.UnwrapSensitivity(order.Expression).(querycore.FieldRef); ok && field == key {
				present = true
				break
			}
		}
	}
	if !present {
		node.Orders = append(node.Orders, querycore.OrderNode{Expression: primary})
	}
	if err := compile(node); err != nil {
		return err
	}
	keyed := key != nil && primaryKeyset(node, primary)
	predicates := node.Predicates
	remaining := node.Limit
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		size := querycore.DefaultChunkSize
		if n, bounded := remaining.Get(); bounded {
			size = min(size, n)
		}
		if size == 0 {
			return nil
		}
		node.Limit = value.Set(size)
		count, err := read(node)
		if err != nil {
			return err
		}
		if count > size {
			return Invalid("database exceeded the retrieval batch limit")
		}
		if n, bounded := remaining.Get(); bounded {
			remaining = value.Set(n - count)
		}
		more := count == size
		if n, bounded := remaining.Get(); bounded && n == 0 {
			more = false
		}
		if more && keyed {
			key, err := key()
			if err != nil {
				return err
			}
			if key == nil {
				return Invalid("retrieval batch primary key cannot be NULL")
			}
			op := querycore.Greater
			if node.Orders[0].Descending {
				op = querycore.Less
			}
			after := querycore.Comparison{Operand: primary, Operator: op, Values: []any{key}, Bind: func(raw any) (driver.Value, error) { return raw, nil }}
			node.Predicates = append(slices.Clip(predicates), after)
			node.Offset = 0
		} else if more {
			if node.Offset > math.MaxInt-count {
				return Invalid("retrieval batch offset overflow")
			}
			node.Offset += count
		}
		if err := Iteration(ctx, func() error {
			for i := range count {
				if err := ctx.Err(); err != nil {
					return err
				}
				if err := yield(i); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
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
func primaryKeyset(node querycore.SelectNode, primary querycore.ValueExpression) bool {
	key, ok := querycore.UnwrapSensitivity(primary).(querycore.FieldRef)
	if !ok || len(node.Orders) != 1 || node.Orders[0].Nulls != querycore.NullsDefault || len(node.Joins) != 0 || len(node.GroupBy) != 0 || len(node.Having) != 0 || node.Distinct.Kind != querycore.NoDistinct {
		return false
	}
	if field, ok := querycore.UnwrapSensitivity(node.Orders[0].Expression).(querycore.FieldRef); !ok || field != key {
		return false
	}
	for _, selection := range node.Selections {
		if _, ok := querycore.UnwrapSensitivity(selection.Expression).(querycore.FieldRef); !ok {
			return false
		}
	}
	return uniqueRecordKey(node, key)
}

// A SELECT boundary does not prove primary-key uniqueness. Trace the selected
// key through plain aliases/CTEs; joins, sets and recursion can repeat models.
func uniqueRecordKey(node querycore.SelectNode, key querycore.FieldRef) bool {
	for depth := 0; depth < querycore.MaxExpressionDepth; depth++ {
		if key.Table != node.Source.Name() || len(node.Joins) != 0 || len(node.GroupBy) != 0 || len(node.Having) != 0 || node.Source.Set != nil || node.Source.Self != nil {
			return false
		}
		var inner *querycore.SelectNode
		switch {
		case node.Source.Query != nil:
			inner = node.Source.Query
		case node.Source.Cte != nil:
			if node.Source.Cte.Recursion != nil {
				return false
			}
			inner = &node.Source.Cte.Query
		default:
			return node.Source.Table != ""
		}
		found := false
		for _, selected := range inner.Selections {
			field, plain := querycore.UnwrapSensitivity(selected.Expression).(querycore.FieldRef)
			name := selected.Alias
			if name == "" && plain {
				name = field.Column
			}
			if name == key.Column {
				if !plain {
					return false
				}
				key, found = field, true
				break
			}
		}
		if !found {
			return false
		}
		node = *inner
	}
	return false
}
