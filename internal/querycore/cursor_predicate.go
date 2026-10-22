package querycore

import (
	"database/sql/driver"
	"slices"
)

// CursorPredicate expands lexicographic comparison with explicit SQL NULL
// semantics. DESC defaults NULLS FIRST, ASC NULLS LAST, including reversed pages.
// nullable reports whether an order key can be NULL; NOT NULL keys never add an
// IS NULL alternative. When every key is NOT NULL and all keys share one
// direction, the equivalent row comparison (a, id) > ($1, $2) is emitted so
// PostgreSQL can use one composite index range.
func CursorPredicate(orders []QueryOrder, keys []driver.Value, nullable func(QueryOrder) bool) Expression {
	if rowKeyset(orders, keys, nullable) {
		operands := make([]ValueExpression, len(orders))
		for i, order := range orders {
			operands[i] = order.Field
		}
		op := Greater
		if orders[0].Descending {
			op = Less
		}
		return RowComparison{Operands: operands, Operator: op, Values: slices.Clone(keys)}
	}
	var alternatives []Expression
	var prefix []Expression
	for i, order := range orders {
		key := keys[i]
		compare := func(op Operator) Expression {
			p := Comparison{Operand: order.Field, Operator: op}
			if op != IsNull && op != IsNotNull {
				p.Values = []any{key}
				p.Bind = func(raw any) (driver.Value, error) { return raw, nil }
			}
			return p
		}
		var after Expression
		if key == nil {
			if order.Descending {
				after = compare(IsNotNull)
			}
		} else {
			op := Greater
			if order.Descending {
				op = Less
			}
			after = compare(op)
			if !order.Descending && nullable(order) {
				after = Junction{Any: true, Children: []Expression{after, compare(IsNull)}}
			}
		}
		if after != nil {
			terms := append(slices.Clone(prefix), after)
			alternatives = append(alternatives, Junction{Children: terms})
		}
		if key == nil {
			prefix = append(prefix, compare(IsNull))
		} else {
			prefix = append(prefix, compare(Equal))
		}
	}
	if len(alternatives) == 0 {
		// All ascending keys can be NULL for a declared result identity. There
		// is then no following position. Reuse empty membership's FALSE node.
		return Comparison{Operand: orders[0].Field, Operator: In, Bind: func(v any) (driver.Value, error) { return v, nil }}
	}
	return Junction{Any: true, Children: alternatives}
}

// rowKeyset allows a row comparison only for two or more plain NOT NULL keys
// with present values, one shared direction and default NULL placement.
func rowKeyset(orders []QueryOrder, keys []driver.Value, nullable func(QueryOrder) bool) bool {
	if len(orders) < 2 || len(orders) != len(keys) {
		return false
	}
	for i, order := range orders {
		if order.Computed != nil || order.Nulls != NullsDefault || order.Descending != orders[0].Descending || keys[i] == nil || nullable(order) {
			return false
		}
	}
	return true
}
