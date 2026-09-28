package query

import (
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
)

type valueExpression interface{ valueNode() }

func (fieldRef) valueNode()      {}
func (aggregateNode) valueNode() {}

type selectItem struct {
	expression valueExpression
	alias      string
}

// Expression is a SQL value with a compiler-checked input scope and result type.
// It retains its codec for typed composition; SQL names remain private metadata.
type Expression[S, V any] struct {
	_     [0]*S
	node  valueExpression
	codec codec.Codec[V]
}

// Value selects the concrete field type. Nullable field wrappers override this
// method so SQL NULL cannot be projected into an ordinary non-nullable field.
func (f valueField[M, V]) Value() Expression[M, V] {
	return Expression[M, V]{node: f.ref, codec: f.codec}
}
func (f NullableField[M, V]) Value() Expression[M, value.Nullable[V]] {
	return Expression[M, value.Nullable[V]]{node: f.ref, codec: codec.Nullable(f.codec)}
}
func (a Aggregate[M, V]) Value() Expression[M, V] {
	return Expression[M, V]{node: a.node, codec: a.codec}
}

// Nullable explicitly widens a non-nullable expression's result type without
// changing its SQL value. It does not substitute a value for SQL NULL.
func Nullable[S, V any](e Expression[S, V]) Expression[S, value.Nullable[V]] {
	return Expression[S, value.Nullable[V]]{node: e.node, codec: codec.Nullable(e.codec)}
}

// Group is a model/scope-owned row key for GroupBy, DistinctOn and PartitionBy.
// Use a generated field's or computed RowExpression's Group method.
type Group[S any] struct {
	_    [0]*S
	node valueExpression
}

func (f valueField[M, V]) Group() Group[M] { return Group[M]{node: f.ref} }

func columnSelections(table string, columns []Column) []selectItem {
	result := make([]selectItem, len(columns))
	for i, c := range columns {
		result[i] = selectItem{expression: fieldRef{table, c.Name}}
	}
	return result
}
func (c *compiler) selectedExpression(e valueExpression, grouped map[fieldRef]bool, grouping bool) (string, error) {
	if err := c.validateSelectedExpression(e, grouped, grouping); err != nil {
		return "", err
	}
	key, err := c.matchingKey(e, false)
	if err != nil {
		return "", err
	}
	if key != nil {
		return c.keySQL(key, grouped, grouping)
	}
	return c.renderSelectedExpression(e, grouped, grouping)
}

func (c *compiler) renderSelectedExpression(e valueExpression, grouped map[fieldRef]bool, grouping bool) (string, error) {
	switch e := e.(type) {
	case fieldRef:
		if c.indexTarget {
			return quoted(e.column), nil
		}
		return qualified(e), nil
	case aggregateNode:
		return c.aggregate(e)
	case scalarSubquery:
		text, err := c.subquerySQL(e.query, true, grouped, grouping)
		return "(" + text + ")", err
	case windowNode:
		return c.windowSQL(e, grouped, grouping)
	case parameterNode, conditionalNode, caseNode, operationNode:
		return c.computedSQL(e, grouped, grouping)
	default:
		return "", fault.New(fault.Invalid, "invalid selected expression")
	}
}

// Validation is pure: nested SELECTs bind only at their SQL output position.
func (c *compiler) validateSelectedExpression(e valueExpression, grouped map[fieldRef]bool, grouping bool) error {
	c.valueDepth++
	defer func() { c.valueDepth-- }()
	if c.valueDepth > MaxExpressionDepth {
		return fault.New(fault.Invalid, "selected value exceeds its depth bound")
	}
	if _, field := e.(fieldRef); !field || c.valueDepth > 1 {
		c.expressionNodes++
		if c.expressionNodes > MaxExpressionNodes {
			return fault.New(fault.Invalid, "selected value exceeds its resource bound")
		}
	}
	if _, field := e.(fieldRef); grouping && !field {
		key, err := c.matchingKey(e, true)
		if err != nil {
			return err
		}
		if key != nil {
			return nil
		}
	}
	handled, err := visitComputedValue(e, func(v valueExpression) error { return c.validateSelectedExpression(v, grouped, grouping) }, func(p expression) error {
		return validateExpressionValues(p, func(v valueExpression) error { return c.validateSelectedExpression(v, grouped, grouping) }, 0, &c.expressionNodes)
	})
	if handled {
		return err
	}
	switch e := e.(type) {
	case fieldRef:
		if err := c.declaredField(e); err != nil {
			return err
		}
		outer := c.outer != nil && c.outer.sources[e.table] != nil
		if grouping && !outer && !grouped[e] {
			return fault.New(fault.Invalid, "selected column is not grouped")
		}
		return nil
	case aggregateNode:
		if c.directSelf {
			return fault.New(fault.Invalid, "aggregate functions are not allowed at a recursive self-reference SELECT level")
		}
		if err := e.validate(c.aggregateField); err != nil {
			return err
		}
		return e.validateFilter(c.declaredField, &c.expressionNodes)
	case scalarSubquery:
		return e.query.err
	case windowNode:
		return c.validateWindow(e, grouped, grouping)
	default:
		return fault.New(fault.Invalid, "invalid selected expression")
	}
}

func (c *compiler) validateHavingValue(v valueExpression, grouped map[fieldRef]bool) error {
	previous := c.inWindow
	c.inWindow = true
	defer func() { c.inWindow = previous }()
	return c.validateSelectedExpression(v, grouped, true)
}
