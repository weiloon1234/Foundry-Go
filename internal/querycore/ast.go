package querycore

import (
	"database/sql/driver"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"github.com/weiloon1234/Foundry-Go/internal/sqlname"
)

type FieldRef struct{ Table, Column string }

func (f FieldRef) Validate(table string) error {
	if !sqlname.Table(f.Table) || !sqlname.Valid(f.Column) {
		return safeerror.NewFault(fault.Invalid, "query field has an invalid table or column declaration")
	}
	if f.Table != table {
		return safeerror.NewFault(fault.Invalid, "query field belongs to a different table declaration")
	}
	return nil
}

type Operator uint8

const (
	Equal Operator = iota + 1
	NotEqual
	Less
	LessOrEqual
	Greater
	GreaterOrEqual
	Like
	Contains
	In
	IsNull
	IsNotNull
	IsDistinctFrom
	IsNotDistinctFrom
	InsensitiveContains
	NotIn
	StartsWith
	EndsWith
	InsensitiveStartsWith
	InsensitiveEndsWith
	InsensitiveLike
)

// escapedPattern reports operators whose literal text the compiler escapes.
func EscapedPattern(op Operator) bool {
	switch op {
	case Contains, InsensitiveContains, StartsWith, EndsWith, InsensitiveStartsWith, InsensitiveEndsWith:
		return true
	}
	return false
}

type Expression interface{ ExpressionNode() }

// rowComparison is a lexicographic row-value comparison such as
// (a, id) > ($1, $2). Keyset traversal uses it only for NOT NULL keys sorted
// in one direction, where it matches the expanded predicate and lets
// PostgreSQL use one composite index range.
type RowComparison struct {
	Operands []ValueExpression
	Operator Operator
	Values   *RowValuesState
}

func (RowComparison) ExpressionNode() {}

type Comparison struct {
	// Text is supplied by text-capable builders, never inferred from a string codec.
	Text     bool
	Operand  ValueExpression
	Operator Operator
	Values   *ComparisonValuesState
	Bind     func(any) (driver.Value, error)
	// kind is the operand codec's scalar representation. Membership tests over
	// supported scalar kinds bind one array parameter instead of one per value.
	Kind codec.ParameterType
}

func (Comparison) ExpressionNode() {}

// binaryComparison compares SQL operands without treating either as a literal binding.
type BinaryComparison struct {
	Text        bool
	Left, Right ValueExpression
	Operator    Operator
}

func (BinaryComparison) ExpressionNode() {}

type Junction struct {
	Any      bool
	Children []Expression
}

func (Junction) ExpressionNode() {}

type Negation struct{ Child Expression }

func (Negation) ExpressionNode() {}

type ComputedOrder struct{ Expression ValueExpression }

// These limits bound SQL compilation work and PostgreSQL's parameter protocol.
const (
	MaxExpressionDepth = 128
	MaxExpressionNodes = 10000
	MaxParameters      = 65535
	// MaxMembershipValues bounds one In/NotIn list. Supported scalar codecs bind
	// the whole list as one array parameter, so it is not limited by
	// MaxParameters; other codecs still bind one parameter per value.
	MaxMembershipValues = 1 << 20
	// MaxScalarSQLBytes bounds expanded scalar SQL during one compilation.
	// Encoded parameter contents are separate from this SQL-text work budget.
	MaxScalarSQLBytes = 1 << 20
)

func ValidateExpression(e Expression, table string, depth int, nodes *int) error {
	return ValidateExpressionFields(e, func(f FieldRef) error { return f.Validate(table) }, depth, nodes)
}

func ValidateExpressionFields(e Expression, field func(FieldRef) error, depth int, nodes *int) error {
	return ValidateExpressionValues(e, func(v ValueExpression) error {
		return ValidateRowValue(v, field, depth, nodes)
	}, depth, nodes)
}

func ValidateExpressionValues(e Expression, operand func(ValueExpression) error, depth int, nodes *int) error {
	*nodes++
	if depth > MaxExpressionDepth || *nodes > MaxExpressionNodes {
		return safeerror.NewFault(fault.Invalid, "query expression exceeds its resource bound")
	}
	switch e := e.(type) {
	case RawFragment:
		return e.Validate()
	case Comparison:
		if err := operand(e.Operand); err != nil {
			return err
		}
		switch e.Operator {
		case IsNull, IsNotNull:
			if len(ComparisonValues(e.Values)) == 0 {
				return nil
			}
		case In, NotIn:
			if len(ComparisonValues(e.Values)) <= MaxMembershipValues && e.Bind != nil {
				return nil
			}
		case Equal, NotEqual, Less, LessOrEqual, Greater, GreaterOrEqual, Like, Contains, InsensitiveContains, StartsWith, EndsWith, InsensitiveStartsWith, InsensitiveEndsWith, InsensitiveLike:
			if len(ComparisonValues(e.Values)) == 1 && e.Bind != nil {
				return nil
			}
		}
	case BinaryComparison:
		if _, ok := BinaryOperator(e.Operator); !ok {
			return safeerror.NewFault(fault.Invalid, "invalid value comparison operator")
		}
		if err := operand(e.Left); err != nil {
			return err
		}
		return operand(e.Right)
	case SubqueryPredicate:
		if e.Query.Err != nil {
			return e.Query.Err
		}
		if e.Operand != nil {
			return operand(e.Operand)
		}
		return nil
	case RowComparison:
		if (e.Operator != Less && e.Operator != Greater) || len(e.Operands) < 2 || len(e.Operands) != len(RowValues(e.Values)) || len(e.Operands) > MaxCursorFields {
			return safeerror.NewFault(fault.Invalid, "invalid row comparison")
		}
		for i, item := range e.Operands {
			if !driver.IsValue(RowValues(e.Values)[i]) || RowValues(e.Values)[i] == nil {
				return safeerror.NewFault(fault.Invalid, "row comparison requires non-NULL database values")
			}
			if err := operand(item); err != nil {
				return err
			}
		}
		return nil
	case Junction:
		for _, child := range e.Children {
			if err := ValidateExpressionValues(child, operand, depth+1, nodes); err != nil {
				return err
			}
		}
		return nil
	case Negation:
		return ValidateExpressionValues(e.Child, operand, depth+1, nodes)
	case ScopeNode:
		// Validated against its model table when resolved during compilation.
		return nil
	}
	return safeerror.NewFault(fault.Invalid, "query contains an invalid predicate declaration")
}
