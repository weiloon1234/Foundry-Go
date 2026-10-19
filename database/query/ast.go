package query

import (
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/value"
)

type fieldRef = querycore.FieldRef

type operator = querycore.Operator

const equal = querycore.Equal

const notEqual = querycore.NotEqual

const less = querycore.Less

const lessOrEqual = querycore.LessOrEqual

const greater = querycore.Greater

const greaterOrEqual = querycore.GreaterOrEqual

const like = querycore.Like

const contains = querycore.Contains

const in = querycore.In

const isNull = querycore.IsNull

const isNotNull = querycore.IsNotNull

const isDistinctFrom = querycore.IsDistinctFrom

const isNotDistinctFrom = querycore.IsNotDistinctFrom

const insensitiveContains = querycore.InsensitiveContains

const notIn = querycore.NotIn

const startsWith = querycore.StartsWith

const endsWith = querycore.EndsWith

const insensitiveStartsWith = querycore.InsensitiveStartsWith

const insensitiveEndsWith = querycore.InsensitiveEndsWith

const insensitiveLike = querycore.InsensitiveLike

func escapedPattern(op operator) bool { return querycore.EscapedPattern(op) }

type expression = querycore.Expression

type rowComparison = querycore.RowComparison

type comparison = querycore.Comparison

type binaryComparison = querycore.BinaryComparison

type junction = querycore.Junction

type negation = querycore.Negation

type computedOrder = querycore.ComputedOrder

const MaxExpressionDepth = querycore.MaxExpressionDepth

const MaxExpressionNodes = querycore.MaxExpressionNodes

const MaxParameters = querycore.MaxParameters

const MaxMembershipValues = querycore.MaxMembershipValues

const MaxScalarSQLBytes = querycore.MaxScalarSQLBytes

func validateExpression(e expression, table string, depth int, nodes *int) error {
	return querycore.ValidateExpression(e, table, depth, nodes)
}

func validateExpressionFields(e expression, field func(fieldRef) error, depth int, nodes *int) error {
	return querycore.ValidateExpressionFields(e, field, depth, nodes)
}

func validateExpressionValues(e expression, operand func(valueExpression) error, depth int, nodes *int) error {
	return querycore.ValidateExpressionValues(e, operand, depth, nodes)
}

// Predicate belongs to exactly one model. Its zero value is invalid. Predicates
// retain immutable expression structure; they never interpolate SQL strings.
type Predicate[M any] struct {
	_          [0]*M
	expression expression
}

// And requires all predicates to match. An empty conjunction is TRUE, so a
// dynamically built filter list may be empty.
func And[M any](predicates ...Predicate[M]) Predicate[M] { return combine(false, predicates) }

// Or requires at least one predicate to match. An empty disjunction is FALSE.
func Or[M any](predicates ...Predicate[M]) Predicate[M] { return combine(true, predicates) }

// True is the neutral predicate for composing dynamic filters with And.
func True[M any]() Predicate[M] { return Predicate[M]{expression: junction{}} }

// False matches no rows; it is the neutral predicate for Or.
func False[M any]() Predicate[M] { return Predicate[M]{expression: junction{Any: true}} }

func combine[M any](any bool, predicates []Predicate[M]) Predicate[M] {
	children := make([]expression, len(predicates))
	for i, p := range predicates {
		children[i] = p.expression
	}
	return Predicate[M]{expression: junction{Any: any, Children: children}}
}

// Not negates this expression without changing it.
func (p Predicate[M]) Not() Predicate[M] {
	return Predicate[M]{expression: negation{Child: p.expression}}
}

// Order belongs to one model and orders a declared field or typed row value.
// Aggregate/window values use the separate selected-expression order boundary.
type Order[M any] struct {
	_          [0]*M
	field      fieldRef
	computed   *computedOrder
	descending bool
	nulls      nullPlacement
}

func (o Order[M]) value() valueExpression {
	return (queryOrder{Field: o.field, Computed: o.computed, Descending: o.descending, Nulls: o.nulls}).Value()
}

func rowOrder[M any](v valueExpression, descending bool) Order[M] {
	if field, ok := v.(fieldRef); ok {
		return Order[M]{field: field, descending: descending}
	}
	return Order[M]{computed: &computedOrder{Expression: v}, descending: descending}
}

// Query is an immutable query for one model. Deriving it copies owned slices.
// Model execution requires generated metadata and a complete row decoder.
type Query[M any] struct {
	queryCore      queryCore
	definition     *Definition[M]
	relations      []Relation[M]
	relationLimits *RelationLimits
}

// For describes a table without model metadata. It supports composition and
// validation; model execution requires ForModel. Applications use QueryUsers
// (or the corresponding generated function) rather than repeating declarations.
func For[M any](table string) Query[M] { return Query[M]{queryCore: queryCore{Table: table}} }

func (q Query[M]) Table() string { return q.queryCore.Table }

func (q Query[M]) Where(predicates ...Predicate[M]) Query[M] {
	q.queryCore.Predicates = appendPredicates(q.queryCore.Predicates, predicates)
	return q
}

func (q Query[M]) OrderBy(orders ...Order[M]) Query[M] {
	q.queryCore.Orders = appendQueryOrders(q.queryCore.Orders, orders)
	return q
}

// Limit bounds returned rows; zero selects no rows and a negative limit fails.
func (q Query[M]) Limit(count int) Query[M] { q.queryCore.Limit = value.Set(count); return q }

// Offset skips rows in the selected order. Use stable ordering for pagination.
func (q Query[M]) Offset(count int) Query[M] { q.queryCore.Offset = count; return q }

// Validate checks declaration boundaries and rejects zero expressions/fields.
// Model-owner/value mismatches are already compiler errors in generated APIs.
func (q Query[M]) Validate() error {
	return q.validateAt(0, q.loadLimits())
}

func (q Query[M]) validateCore() error {
	if err := q.queryCore.Validate(); err != nil {
		return err
	}
	if q.definition != nil {
		return q.definition.validated()
	}
	return nil
}
