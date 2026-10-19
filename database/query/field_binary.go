package query

import (
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/value"
)

// BinaryField exposes equality, membership, ordering, projection and mutations
// for a concrete byte-slice type. Binary values are not comparable Go keys and
// deliberately do not implement KeyField or model identity declarations.
type BinaryField[M any, V ~[]byte] struct{ valueField[M, V] }

func NewBinaryField[M any, V ~[]byte](table, column string, c codec.Codec[V]) BinaryField[M, V] {
	return BinaryField[M, V]{valueField[M, V]{fieldRef{Table: table, Column: column}, c}}
}

// NullableBinaryField distinguishes SQL NULL from a present empty byte buffer.
// Eq, In and Set accept non-null values; use IsNull/IsNotNull and SetNull for NULL.
type NullableBinaryField[M any, V ~[]byte] struct{ BinaryField[M, V] }

func NewNullableBinaryField[M any, V ~[]byte](table, column string, c codec.Codec[V]) NullableBinaryField[M, V] {
	return NullableBinaryField[M, V]{NewBinaryField[M, V](table, column, c)}
}

func (f NullableBinaryField[M, V]) IsNull() Predicate[M] { return f.compare(isNull) }

func (f NullableBinaryField[M, V]) IsNotNull() Predicate[M] { return f.compare(isNotNull) }

func (f NullableBinaryField[M, V]) SetNull() ConflictUpdate[M] {
	return ConflictUpdate[M]{field: f.ref, null: true}
}

func (f BinaryField[M, V]) Group() Group[M] { return Group[M]{node: f.ref} }

func (f BinaryField[M, V]) InCorrelatedQuery(source CorrelatedValueSource[M, V]) Predicate[M] {
	return f.inSubquery(correlatedValueOf(source).subquery)
}

func (f BinaryField[M, V]) InNullableCorrelatedQuery(source CorrelatedValueSource[M, value.Nullable[V]]) Predicate[M] {
	return f.inSubquery(correlatedValueOf(source).subquery)
}

func (f BinaryField[M, V]) Eq(value V) Predicate[M] { return f.compare(equal, value) }

func (f BinaryField[M, V]) Ne(value V) Predicate[M] { return f.compare(notEqual, value) }

func (f BinaryField[M, V]) In(values ...V) Predicate[M] { return f.compare(in, values...) }

func (f BinaryField[M, V]) Asc() Order[M] { return Order[M]{field: f.ref} }

func (f BinaryField[M, V]) Desc() Order[M] { return Order[M]{field: f.ref, descending: true} }

func (f BinaryField[M, V]) compare(op operator, values ...V) Predicate[M] {
	return Predicate[M]{expression: typedComparison(f.ref, op, f.codec, values)}
}

func (f BinaryField[M, V]) conflictColumn() conflictColumn[M] {
	return conflictColumn[M]{field: f.ref}
}

func (f BinaryField[M, V]) Incoming() ConflictUpdate[M] {
	return ConflictUpdate[M]{field: f.ref, incoming: true}
}

func (f BinaryField[M, V]) Set(v V) ConflictUpdate[M] {
	return literalConflict[M](f.ref, captureAssignment(f.codec, v))
}

func (f BinaryField[M, V]) InQuery(source ValueQuerySource[V]) Predicate[M] {
	return f.inSubquery(valueSource(source).subquery)
}

func (f BinaryField[M, V]) InNullableQuery(source ValueQuerySource[value.Nullable[V]]) Predicate[M] {
	return f.inSubquery(valueSource(source).subquery)
}

func (f BinaryField[M, V]) inSubquery(q subquery) Predicate[M] {
	return Predicate[M]{expression: subqueryPredicate{Query: q, Operand: f.ref}}
}

func (f BinaryField[M, V]) NotIn(values ...V) Predicate[M] { return f.compare(notIn, values...) }
