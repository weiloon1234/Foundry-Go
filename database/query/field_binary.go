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
	return BinaryField[M, V]{valueField[M, V]{fieldRef{table, column}, c}}
}

// NullableBinaryField distinguishes SQL NULL from a present empty byte buffer.
// Eq, In and Set accept non-null values; use IsNull/IsNotNull and SetNull for NULL.
type NullableBinaryField[M any, V ~[]byte] struct{ BinaryField[M, V] }

func NewNullableBinaryField[M any, V ~[]byte](table, column string, c codec.Codec[V]) NullableBinaryField[M, V] {
	return NullableBinaryField[M, V]{NewBinaryField[M, V](table, column, c)}
}

func (f NullableBinaryField[M, V]) IsNull() Predicate[M]    { return f.compare(isNull) }
func (f NullableBinaryField[M, V]) IsNotNull() Predicate[M] { return f.compare(isNotNull) }
func (f NullableBinaryField[M, V]) SetNull() ConflictUpdate[M] {
	return ConflictUpdate[M]{field: f.ref, null: true}
}
func (f NullableBinaryField[M, V]) Value() Expression[M, value.Nullable[V]] {
	return Expression[M, value.Nullable[V]]{node: f.ref, codec: codec.Nullable(f.codec)}
}
func (f NullableBinaryField[M, V]) rowValue() RowExpression[M, value.Nullable[V]] {
	return RowExpression[M, value.Nullable[V]]{f.Value()}
}
