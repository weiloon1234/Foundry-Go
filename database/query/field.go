package query

import "github.com/weiloon1234/Foundry-Go/database/codec"

// ScalarField exposes equality, membership, and ordering for a concrete model
// and value type. Constructors are explicit declaration boundaries intended for
// generated code; they do not verify a physical database schema.
type ScalarField[M any, V comparable] struct{ valueField[M, V] }

// valueField owns operations that do not require a comparable Go key.
type valueField[M, V any] struct {
	ref   fieldRef
	codec codec.Codec[V]
}

// KeyField is a generated field carrying both model owner and comparable key
// type. Nullable fields retain their non-null key type for relation compatibility.
type KeyField[M any, K comparable] interface{ relationKey() ScalarField[M, K] }

func (f ScalarField[M, V]) relationKey() ScalarField[M, V] { return f }

func NewScalarField[M any, V comparable](table, column string, c codec.Codec[V]) ScalarField[M, V] {
	return ScalarField[M, V]{valueField[M, V]{fieldRef{table, column}, c}}
}
func (f valueField[M, V]) Eq(value V) Predicate[M] { return f.compare(equal, value) }
func (f valueField[M, V]) Ne(value V) Predicate[M] { return f.compare(notEqual, value) }

// In copies the values. An empty membership set represents no matching rows.
func (f valueField[M, V]) In(values ...V) Predicate[M] { return f.compare(in, values...) }
func (f valueField[M, V]) Asc() Order[M]               { return Order[M]{field: f.ref} }
func (f valueField[M, V]) Desc() Order[M]              { return Order[M]{field: f.ref, descending: true} }
func (f valueField[M, V]) compare(op operator, values ...V) Predicate[M] {
	return Predicate[M]{expression: typedComparison(f.ref, op, f.codec, values)}
}

// OrderedField adds range comparisons for types selected by the generator's
// codec metadata, including numbers and temporal values.
type OrderedField[M any, V comparable] struct{ ScalarField[M, V] }

func NewOrderedField[M any, V comparable](table, column string, c codec.Codec[V]) OrderedField[M, V] {
	return OrderedField[M, V]{NewScalarField[M, V](table, column, c)}
}
func (f OrderedField[M, V]) Lt(value V) Predicate[M]  { return f.compare(less, value) }
func (f OrderedField[M, V]) Lte(value V) Predicate[M] { return f.compare(lessOrEqual, value) }
func (f OrderedField[M, V]) Gt(value V) Predicate[M]  { return f.compare(greater, value) }
func (f OrderedField[M, V]) Gte(value V) Predicate[M] { return f.compare(greaterOrEqual, value) }

// TextField adds text operations while preserving named string types.
type TextField[M any, V ~string] struct{ OrderedField[M, V] }

func NewTextField[M any, V ~string](table, column string, c codec.Codec[V]) TextField[M, V] {
	return TextField[M, V]{NewOrderedField[M, V](table, column, c)}
}

// Like accepts a SQL LIKE pattern. Contains represents literal substring matching;
// the compiler owns escaping without changing the application's value.
func (f TextField[M, V]) Like(pattern V) Predicate[M]  { return f.compare(like, pattern) }
func (f TextField[M, V]) Contains(text V) Predicate[M] { return f.compare(contains, text) }

// NullableField adds explicit null predicates. Eq still accepts a concrete V;
// null comparisons must use IsNull/IsNotNull, never Eq(nil).
type NullableField[M any, V comparable] struct{ ScalarField[M, V] }

func NewNullableField[M any, V comparable](table, column string, c codec.Codec[V]) NullableField[M, V] {
	return NullableField[M, V]{NewScalarField[M, V](table, column, c)}
}
func (f NullableField[M, V]) IsNull() Predicate[M]    { return f.compare(isNull) }
func (f NullableField[M, V]) IsNotNull() Predicate[M] { return f.compare(isNotNull) }

type NullableOrderedField[M any, V comparable] struct{ NullableField[M, V] }

func NewNullableOrderedField[M any, V comparable](table, column string, c codec.Codec[V]) NullableOrderedField[M, V] {
	return NullableOrderedField[M, V]{NewNullableField[M, V](table, column, c)}
}
func (f NullableOrderedField[M, V]) Lt(value V) Predicate[M]  { return f.compare(less, value) }
func (f NullableOrderedField[M, V]) Lte(value V) Predicate[M] { return f.compare(lessOrEqual, value) }
func (f NullableOrderedField[M, V]) Gt(value V) Predicate[M]  { return f.compare(greater, value) }
func (f NullableOrderedField[M, V]) Gte(value V) Predicate[M] {
	return f.compare(greaterOrEqual, value)
}

type NullableTextField[M any, V ~string] struct{ NullableOrderedField[M, V] }

func NewNullableTextField[M any, V ~string](table, column string, c codec.Codec[V]) NullableTextField[M, V] {
	return NullableTextField[M, V]{NewNullableOrderedField[M, V](table, column, c)}
}
func (f NullableTextField[M, V]) Like(pattern V) Predicate[M]  { return f.compare(like, pattern) }
func (f NullableTextField[M, V]) Contains(text V) Predicate[M] { return f.compare(contains, text) }
