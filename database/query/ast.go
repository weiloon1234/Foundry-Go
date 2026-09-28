// Package query builds and executes model-owned queries through one shared AST
// and PostgreSQL compiler. Generated declarations own codecs and row hydration.
package query

import (
	"database/sql/driver"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/sqlname"
	"github.com/weiloon1234/Foundry-Go/value"
)

type fieldRef struct{ table, column string }

func (f fieldRef) validate(table string) error {
	if !sqlname.Table(f.table) || !sqlname.Valid(f.column) {
		return fault.New(fault.Invalid, "query field has an invalid table or column declaration")
	}
	if f.table != table {
		return fault.New(fault.Invalid, "query field belongs to a different table declaration")
	}
	return nil
}

type operator uint8

const (
	equal operator = iota + 1
	notEqual
	less
	lessOrEqual
	greater
	greaterOrEqual
	like
	contains
	in
	isNull
	isNotNull
	isDistinctFrom
	isNotDistinctFrom
	insensitiveContains
)

type expression interface{ expressionNode() }
type comparison struct {
	operand  valueExpression
	operator operator
	values   []any
	bind     func(any) (driver.Value, error)
}

func (comparison) expressionNode() {}

// binaryComparison compares SQL operands without treating either as a literal binding.
type binaryComparison struct {
	left, right valueExpression
	operator    operator
}

func (binaryComparison) expressionNode() {}

type junction struct {
	any      bool
	children []expression
}

func (junction) expressionNode() {}

type negation struct{ child expression }

func (negation) expressionNode() {}

// Predicate belongs to exactly one model. Its zero value is invalid. Predicates
// retain immutable expression structure; they never interpolate SQL strings.
type Predicate[M any] struct {
	_          [0]*M
	expression expression
}

// And requires all predicates to match. An empty conjunction is invalid.
func And[M any](predicates ...Predicate[M]) Predicate[M] { return combine(false, predicates) }

// Or requires at least one predicate to match. An empty disjunction is invalid.
func Or[M any](predicates ...Predicate[M]) Predicate[M] { return combine(true, predicates) }
func combine[M any](any bool, predicates []Predicate[M]) Predicate[M] {
	children := make([]expression, len(predicates))
	for i, p := range predicates {
		children[i] = p.expression
	}
	return Predicate[M]{expression: junction{any, children}}
}

// Not negates this expression without changing it.
func (p Predicate[M]) Not() Predicate[M] { return Predicate[M]{expression: negation{p.expression}} }

// Order belongs to one model and orders a declared field or typed row value.
// Aggregate/window values use the separate selected-expression order boundary.
type Order[M any] struct {
	_          [0]*M
	field      fieldRef
	computed   *computedOrder
	descending bool
}

type computedOrder struct{ expression valueExpression }

func (o Order[M]) value() valueExpression {
	if o.computed != nil {
		if o.field != (fieldRef{}) {
			return parameterNode{err: fault.New(fault.Invalid, "ambiguous row ordering")}
		}
		return o.computed.expression
	}
	return o.field
}
func rowOrder[M any](v valueExpression, descending bool) Order[M] {
	if field, ok := v.(fieldRef); ok {
		return Order[M]{field: field, descending: descending}
	}
	return Order[M]{computed: &computedOrder{v}, descending: descending}
}

// Query is an immutable query for one model. Deriving it copies owned slices.
// Model execution requires generated metadata and a complete row decoder.
type Query[M any] struct {
	table           string
	predicates      []expression
	orders          []Order[M]
	definition      *Definition[M]
	limit           value.Optional[int]
	offset          int
	relations       []Relation[M]
	relationLimits  *RelationLimits
	softDeleteScope softDeleteScope
}

// For describes a table without model metadata. It supports composition and
// validation; model execution requires ForModel. Applications use QueryUsers
// (or the corresponding generated function) rather than repeating declarations.
func For[M any](table string) Query[M] { return Query[M]{table: table} }
func (q Query[M]) Table() string       { return q.table }
func (q Query[M]) Where(predicates ...Predicate[M]) Query[M] {
	q.predicates = appendPredicates(q.predicates, predicates)
	return q
}
func (q Query[M]) OrderBy(orders ...Order[M]) Query[M] {
	q.orders = append(append([]Order[M](nil), q.orders...), orders...)
	return q
}

// Limit bounds returned rows; zero selects no rows and a negative limit fails.
func (q Query[M]) Limit(count int) Query[M] { q.limit = value.Set(count); return q }

// Offset skips rows in the selected order. Use stable ordering for pagination.
func (q Query[M]) Offset(count int) Query[M] { q.offset = count; return q }

// Validate checks declaration boundaries and rejects zero expressions/fields.
// Model-owner/value mismatches are already compiler errors in generated APIs.
func (q Query[M]) Validate() error {
	return q.validateAt(0, q.loadLimits())
}
func (q Query[M]) validateCore() error {
	if err := q.validateSoftDeleteScope(); err != nil {
		return err
	}
	if !sqlname.Table(q.table) {
		return fault.New(fault.Invalid, "query has an invalid table declaration")
	}
	if limit, set := q.limit.Get(); (set && limit < 0) || q.offset < 0 {
		return fault.New(fault.Invalid, "query limit and offset cannot be negative")
	}
	if len(q.orders) > MaxExpressionNodes {
		return fault.New(fault.Invalid, "query ordering exceeds its resource bound")
	}
	nodes := 0
	for _, p := range q.effectivePredicates() {
		if err := validateExpression(p, q.table, 0, &nodes); err != nil {
			return err
		}
	}
	for _, order := range q.orders {
		if err := validateRowValue(order.value(), func(f fieldRef) error { return f.validate(q.table) }, 0, &nodes); err != nil {
			return err
		}
	}
	if q.definition != nil {
		return q.definition.Validate()
	}
	return nil
}

// These limits bound SQL compilation work and PostgreSQL's parameter protocol.
const (
	MaxExpressionDepth = 128
	MaxExpressionNodes = 10000
	MaxParameters      = 65535
	// MaxScalarSQLBytes bounds expanded scalar SQL during one compilation.
	// Encoded parameter contents are separate from this SQL-text work budget.
	MaxScalarSQLBytes = 1 << 20
)

func validateExpression(e expression, table string, depth int, nodes *int) error {
	return validateExpressionFields(e, func(f fieldRef) error { return f.validate(table) }, depth, nodes)
}

func validateExpressionFields(e expression, field func(fieldRef) error, depth int, nodes *int) error {
	return validateExpressionValues(e, func(v valueExpression) error {
		return validateRowValue(v, field, depth, nodes)
	}, depth, nodes)
}

func validateExpressionValues(e expression, operand func(valueExpression) error, depth int, nodes *int) error {
	*nodes++
	if depth > MaxExpressionDepth || *nodes > MaxExpressionNodes {
		return fault.New(fault.Invalid, "query expression exceeds its resource bound")
	}
	switch e := e.(type) {
	case comparison:
		if err := operand(e.operand); err != nil {
			return err
		}
		switch e.operator {
		case isNull, isNotNull:
			if len(e.values) == 0 {
				return nil
			}
		case in:
			if len(e.values) <= MaxParameters && e.bind != nil {
				return nil
			}
		case equal, notEqual, less, lessOrEqual, greater, greaterOrEqual, like, contains, insensitiveContains:
			if len(e.values) == 1 && e.bind != nil {
				return nil
			}
		}
	case binaryComparison:
		if _, ok := binaryOperator(e.operator); !ok {
			return fault.New(fault.Invalid, "invalid value comparison operator")
		}
		if err := operand(e.left); err != nil {
			return err
		}
		return operand(e.right)
	case subqueryPredicate:
		if e.query.err != nil {
			return e.query.err
		}
		if e.operand != nil {
			return operand(e.operand)
		}
		return nil
	case junction:
		if len(e.children) == 0 {
			return fault.New(fault.Invalid, "predicate junction requires at least one operand")
		}
		for _, child := range e.children {
			if err := validateExpressionValues(child, operand, depth+1, nodes); err != nil {
				return err
			}
		}
		return nil
	case negation:
		return validateExpressionValues(e.child, operand, depth+1, nodes)
	}
	return fault.New(fault.Invalid, "query contains an invalid predicate declaration")
}
