package querycore

import (
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

type AggregateKind uint8

const (
	CountAll AggregateKind = iota + 1
	CountValues
	CountDistinct
	SumValues
	AverageValues
	MinimumValue
	MaximumValue
	ExistsValues
)

type AggregateCast uint8

const (
	NativeNumber AggregateCast = iota
	ExactNumberCast
	FloatNumberCast
	IntervalValueCast
)

type AggregateNode struct {
	Kind   AggregateKind
	Field  FieldRef
	Cast   AggregateCast
	Filter *AggregateFilter
}

func (a AggregateNode) Validate(field func(FieldRef) error) error {
	switch a.Kind {
	case CountAll, ExistsValues:
		if a.Field != (FieldRef{}) || a.Cast != NativeNumber {
			return safeerror.NewFault(fault.Invalid, "row aggregate has an unexpected field or cast")
		}
		return nil
	case CountValues, CountDistinct, MinimumValue, MaximumValue:
		if a.Cast != NativeNumber {
			return safeerror.NewFault(fault.Invalid, "aggregate has an unsupported cast")
		}
	case SumValues, AverageValues:
		if a.Cast != ExactNumberCast && a.Cast != FloatNumberCast && a.Cast != IntervalValueCast {
			return safeerror.NewFault(fault.Invalid, "aggregate summary requires a result representation")
		}
	default:
		return safeerror.NewFault(fault.Invalid, "invalid aggregate declaration")
	}
	return field(a.Field)
}

func (c *Compiler) Aggregate(a AggregateNode) (string, error) {
	return c.AggregateAt(a, c.AggregateField, nil, false, false)
}

func (c *Compiler) AggregateAt(a AggregateNode, field func(FieldRef) error, grouped map[FieldRef]bool, grouping, window bool) (string, error) {
	if err := a.Validate(field); err != nil {
		return "", err
	}
	filterField := c.DeclaredField
	if window {
		filterField = field
	}
	if err := a.ValidateFilter(filterField, &c.ExpressionNodes); err != nil {
		return "", err
	}
	f := Qualified(a.Field)
	if a.Cast == ExactNumberCast {
		f = "CAST(" + f + " AS numeric)"
	}
	if a.Cast == FloatNumberCast {
		f = "CAST(" + f + " AS double precision)"
	}
	if a.Cast == IntervalValueCast {
		f = "CAST(" + f + " AS interval)"
	}
	name := map[AggregateKind]string{CountValues: "COUNT", CountDistinct: "COUNT", SumValues: "SUM", AverageValues: "AVG", MinimumValue: "MIN", MaximumValue: "MAX"}[a.Kind]
	if a.Kind == CountDistinct {
		f = "DISTINCT " + f
	}
	if a.Kind == CountAll || a.Kind == ExistsValues {
		name, f = "COUNT", "*"
	}
	filter, err := c.AggregateFilterSQL(a, grouped, grouping, window)
	if err != nil {
		return "", err
	}
	result := name + "(" + f + ")" + filter
	if a.Kind == ExistsValues {
		result = "(" + result + " > 0)"
	}
	return result, nil
}
