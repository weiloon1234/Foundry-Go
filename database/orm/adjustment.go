package orm

import (
	"database/sql/driver"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"math"

	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
)

// ErrBoundExceeded means a guarded per-key adjustment rejected a visible row.
// A missing or hidden key returns database.NotFound instead.
var ErrBoundExceeded = safeerror.New("numeric adjustment bound exceeded")

// Bound constrains the result of an atomic numeric adjustment. Construct bounds
// with AtLeast or AtMost; the zero value is invalid. The database evaluates
// column +/- amount before comparing the bound. int64/float64 overflow returns
// a database out-of-range error; it never silently passes the guard.
type Bound[V NumberValue] struct {
	_       [0]*V
	value   *querycore.InputState
	minimum bool
	ok      bool
}

// AtLeast requires the decremented value to remain at or above floor.
func AtLeast[V NumberValue](floor V) Bound[V] {
	return Bound[V]{value: querycore.RetainInput(floor), minimum: true, ok: true}
}

// AtMost requires the incremented value to remain at or below ceiling.
func AtMost[V NumberValue](ceiling V) Bound[V] {
	return Bound[V]{value: querycore.RetainInput(ceiling), ok: true}
}

// NumberIncrement adds a strictly positive amount in the UPDATE statement.
// Example: NumberIncrement(Orders.TotalCents, 100, orm.AtMost[int64](1000)).
func NumberIncrement[M any, V NumberValue, R columnRole](c numberColumn[M, V, R], amount V, bound ...Bound[V]) Assignment[M] {
	return adjustment[M](c.number, amount, false, bound)
}

// NumberDecrement subtracts a strictly positive amount in the UPDATE statement.
// Example: NumberDecrement(Orders.TotalCents, 50, orm.AtLeast[int64](0)).
func NumberDecrement[M any, V NumberValue, R columnRole](c numberColumn[M, V, R], amount V, bound ...Bound[V]) Assignment[M] {
	return adjustment[M](c.number, amount, true, bound)
}

// NullableNumberIncrement adds a strictly positive amount; SQL NULL remains NULL.
// Example: NullableNumberIncrement(LedgerEntries.Balance, amount, orm.AtMost(ceiling)).
func NullableNumberIncrement[M any, V NumberValue](c NullableNumber[M, V], amount V, bound ...Bound[V]) Assignment[M] {
	return adjustment[M](c.nullableNumber.base, amount, false, bound)
}

// NullableNumberDecrement subtracts a strictly positive amount; SQL NULL remains NULL.
// Example: NullableNumberDecrement(LedgerEntries.Balance, amount, orm.AtLeast(floor)).
func NullableNumberDecrement[M any, V NumberValue](c NullableNumber[M, V], amount V, bound ...Bound[V]) Assignment[M] {
	return adjustment[M](c.nullableNumber.base, amount, true, bound)
}

func adjustment[M any, V NumberValue](c columnData[V], amount V, subtract bool, bounds []Bound[V]) Assignment[M] {
	if c.core == nil || c.core.owner == nil || c.bind == nil {
		return Assignment[M]{err: invalid("uninitialized numeric column")}
	}
	a := Assignment[M]{owner: c.core.owner}
	fail := func(message string) Assignment[M] { a.err = invalid(c.core.metadata.Name + ": " + message); return a }
	bound, err := c.bind(amount)
	positive := false
	if d, ok := any(amount).(decimal.Decimal); ok {
		positive = d.Sign() > 0
	} else {
		switch v := bound.(type) {
		case int64:
			positive = v > 0
		case float64:
			positive = v > 0 && !math.IsNaN(v) && !math.IsInf(v, 0)
		}
	}
	if err != nil || !positive {
		return fail("adjustment amount must be strictly positive and finite")
	}
	field := querycore.FieldRef{Table: c.core.owner.model.Table, Column: c.core.metadata.Name}
	a.adjust = &querycore.Adjustment{Field: field, Subtract: subtract, Bind: func() (driver.Value, error) { return bound, nil }}
	if len(bounds) > 1 {
		return fail("at most one adjustment bound is allowed")
	}
	if len(bounds) == 1 {
		limit := bounds[0]
		if !limit.ok {
			return fail("uninitialized adjustment bound; use AtLeast or AtMost")
		}
		if limit.minimum != subtract {
			return fail("use AtLeast with Decrement and AtMost with Increment")
		}
		ceiling, err := c.bind(querycore.StoredInput(limit.value).(V))
		if err != nil {
			return fail("invalid adjustment bound")
		}
		op, comparison := querycore.AddOperation, querycore.LessOrEqual
		if subtract {
			op, comparison = querycore.SubtractOperation, querycore.GreaterOrEqual
		}
		kind := c.core.kind
		operand := querycore.OperationNode{Kind: op, Result: kind, Arguments: []querycore.OperationArgument{
			{Value: field, Kind: kind}, {Value: querycore.ParameterNode{Kind: kind, Value: querycore.RetainLiteral(bound)}, Kind: kind},
		}}
		a.guard = querycore.BinaryComparison{Left: operand, Right: querycore.ParameterNode{Kind: kind, Value: querycore.RetainLiteral(ceiling)}, Operator: comparison}
	}
	return a
}

func boundExceeded() error {
	return safeerror.WrapFailure(fault.Conflict, "numeric adjustment rejected by bound", ErrBoundExceeded)
}
