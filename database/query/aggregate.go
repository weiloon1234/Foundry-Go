package query

import (
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
)

type aggregateKind uint8

const (
	countAll aggregateKind = iota + 1
	countValues
	countDistinct
	sumValues
	averageValues
	minimumValue
	maximumValue
	existsValues
)

type aggregateCast uint8

const (
	nativeNumber aggregateCast = iota
	exactNumberCast
	floatNumberCast
	intervalValueCast
)

type aggregateNode struct {
	kind   aggregateKind
	field  fieldRef
	cast   aggregateCast
	filter *aggregateFilter
}

// Aggregate carries the input model and exact result type. Generated fields
// expose only supported operations. Zero aggregates are invalid declarations.
type Aggregate[M, V any] struct {
	_     [0]*M
	node  aggregateNode
	codec codec.Codec[V]
}

// AggregateExpression accepts the supported aggregate capabilities while
// retaining their exact model and result type through relation declarations.
type AggregateExpression[M, V any] interface{ aggregateValue() Aggregate[M, V] }

func (a Aggregate[M, V]) aggregateValue() Aggregate[M, V] { return a }

// Count counts rows, including rows with NULL fields. An empty group is zero.
func Count[M any]() OrderedAggregate[M, int64] {
	return OrderedAggregate[M, int64]{Aggregate[M, int64]{node: aggregateNode{kind: countAll}, codec: codec.Signed[int64]()}}
}

// Exists reports whether the group contains any rows, including NULL fields.
func Exists[M any]() Aggregate[M, bool] {
	return Aggregate[M, bool]{node: aggregateNode{kind: existsValues}, codec: codec.Bool[bool]()}
}

// Count counts non-NULL values in this field. CountDistinct also removes duplicates.
func (f valueField[M, V]) Count() OrderedAggregate[M, int64] {
	return OrderedAggregate[M, int64]{Aggregate[M, int64]{node: aggregateNode{kind: countValues, field: f.ref}, codec: codec.Signed[int64]()}}
}
func (f valueField[M, V]) CountDistinct() OrderedAggregate[M, int64] {
	return OrderedAggregate[M, int64]{Aggregate[M, int64]{node: aggregateNode{kind: countDistinct, field: f.ref}, codec: codec.Signed[int64]()}}
}
func fieldExtremum[M any, V comparable](f ScalarField[M, V], kind aggregateKind) NullableOrderedAggregate[M, V] {
	return nullableOrderedAggregate[M](aggregateNode{kind: kind, field: f.ref}, f.codec)
}

func fieldSummary[M any, V comparable, R any](f ScalarField[M, V], kind aggregateKind, cast aggregateCast, c codec.Codec[R]) NullableOrderedAggregate[M, R] {
	return nullableOrderedAggregate[M](aggregateNode{kind: kind, field: f.ref, cast: cast}, c)
}

// Min preserves the field type and returns SQL NULL for an empty/all-NULL group.
func (f OrderedField[M, V]) Min() NullableOrderedAggregate[M, V] {
	return fieldExtremum(f.ScalarField, minimumValue)
}
func (f OrderedField[M, V]) Max() NullableOrderedAggregate[M, V] {
	return fieldExtremum(f.ScalarField, maximumValue)
}
func (f NullableOrderedField[M, V]) Min() NullableOrderedAggregate[M, V] {
	return fieldExtremum(f.ScalarField, minimumValue)
}
func (f NullableOrderedField[M, V]) Max() NullableOrderedAggregate[M, V] {
	return fieldExtremum(f.ScalarField, maximumValue)
}

func (a aggregateNode) validate(field func(fieldRef) error) error {
	switch a.kind {
	case countAll, existsValues:
		if a.field != (fieldRef{}) || a.cast != nativeNumber {
			return fault.New(fault.Invalid, "row aggregate has an unexpected field or cast")
		}
		return nil
	case countValues, countDistinct, minimumValue, maximumValue:
		if a.cast != nativeNumber {
			return fault.New(fault.Invalid, "aggregate has an unsupported cast")
		}
	case sumValues, averageValues:
		if a.cast != exactNumberCast && a.cast != floatNumberCast && a.cast != intervalValueCast {
			return fault.New(fault.Invalid, "aggregate summary requires a result representation")
		}
	default:
		return fault.New(fault.Invalid, "invalid aggregate declaration")
	}
	return field(a.field)
}
func (c *compiler) aggregate(a aggregateNode) (string, error) {
	return c.aggregateAt(a, c.aggregateField, nil, false, false)
}
func (c *compiler) aggregateAt(a aggregateNode, field func(fieldRef) error, grouped map[fieldRef]bool, grouping, window bool) (string, error) {
	if err := a.validate(field); err != nil {
		return "", err
	}
	filterField := c.declaredField
	if window {
		filterField = field
	}
	if err := a.validateFilter(filterField, &c.expressionNodes); err != nil {
		return "", err
	}
	f := qualified(a.field)
	if a.cast == exactNumberCast {
		f = "CAST(" + f + " AS numeric)"
	}
	if a.cast == floatNumberCast {
		f = "CAST(" + f + " AS double precision)"
	}
	if a.cast == intervalValueCast {
		f = "CAST(" + f + " AS interval)"
	}
	name := map[aggregateKind]string{countValues: "COUNT", countDistinct: "COUNT", sumValues: "SUM", averageValues: "AVG", minimumValue: "MIN", maximumValue: "MAX"}[a.kind]
	if a.kind == countDistinct {
		f = "DISTINCT " + f
	}
	if a.kind == countAll || a.kind == existsValues {
		name, f = "COUNT", "*"
	}
	filter, err := c.aggregateFilterSQL(a, grouped, grouping, window)
	if err != nil {
		return "", err
	}
	result := name + "(" + f + ")" + filter
	if a.kind == existsValues {
		result = "(" + result + " > 0)"
	}
	return result, nil
}
