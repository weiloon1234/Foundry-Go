package rows

import (
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/enum"
	"github.com/weiloon1234/Foundry-Go/internal/ormcore"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

// FieldOf binds a custom row value with an explicit codec. Example: rows.FieldOf(b,&r.Code,orm.Codec(custom)).
func FieldOf[R, V any](b Builder[R], ptr *V, option orm.ColumnOption[V]) Field[R, V] {
	return bindField(b, ptr, orm.FoundryCodec(option, sqlowner.Metadata()))
}

// NullableFieldOf binds a nullable custom value. Example: rows.NullableFieldOf(b,&r.Code,orm.Codec(custom)).
func NullableFieldOf[R, V any](b Builder[R], ptr *value.Nullable[V], option orm.ColumnOption[V]) Field[R, value.Nullable[V]] {
	inner := orm.FoundryCodec(option, sqlowner.Metadata())
	var nullable codec.Codec[value.Nullable[V]]
	if inner.Validate() == nil {
		nullable = codec.Nullable(inner)
	}
	return bindField(b, ptr, nullable)
}

// IDOf binds a model-owned identifier. Example: rows.IDOf(b,&r.ID).
func IDOf[R, M any](b Builder[R], ptr *model.ID[M]) Field[R, model.ID[M]] {
	return bindField(b, ptr, codec.ID[M]())
}

// TextOf binds a string or defined string type. Example: rows.TextOf(b,&r.Label).
func TextOf[R any, V ~string](b Builder[R], ptr *V) Field[R, V] {
	return bindField(b, ptr, codec.String[V]())
}

// NumberOf chooses the numeric codec at startup. Example: rows.NumberOf(b,&r.Amount).
func NumberOf[R any, V orm.NumberValue](b Builder[R], ptr *V) Field[R, V] {
	return bindField(b, ptr, ormcore.NumberCodec[V]())
}

// EnumOf validates and uses the generated descriptor. Example: rows.EnumOf(b,&r.Status).
func EnumOf[R any, E interface {
	enum.Scalar
	EnumDescriptor() enum.Descriptor[E]
}](b Builder[R], ptr *E) Field[R, E] {
	codec, check := ormcore.EnumCodec[E]()
	f := bindField(b, ptr, codec)
	f.core.check = check
	return f
}

// TimeOf chooses the temporal codec at startup. Example: rows.TimeOf(b,&r.CreatedAt).
func TimeOf[R any, V orm.TimeValue](b Builder[R], ptr *V) Field[R, V] {
	return bindField(b, ptr, ormcore.TimeCodec[V]())
}

// BytesOf binds an owned byte buffer. Example: rows.BytesOf(b,&r.Payload).
func BytesOf[R any, V ~[]byte](b Builder[R], ptr *V) Field[R, V] {
	return bindField(b, ptr, codec.Bytes[V]())
}

// NullableIDOf binds a nullable model identifier. Example: rows.NullableIDOf(b,&r.OwnerID).
func NullableIDOf[R, M any](b Builder[R], ptr *value.Nullable[model.ID[M]]) Field[R, value.Nullable[model.ID[M]]] {
	return bindField(b, ptr, codec.Nullable(codec.ID[M]()))
}

// NullableTextOf binds nullable text. Example: rows.NullableTextOf(b,&r.Nickname).
func NullableTextOf[R any, V ~string](b Builder[R], ptr *value.Nullable[V]) Field[R, value.Nullable[V]] {
	return bindField(b, ptr, codec.Nullable(codec.String[V]()))
}

// NullableNumberOf chooses a nullable numeric codec. Example: rows.NullableNumberOf(b,&r.Balance).
func NullableNumberOf[R any, V orm.NumberValue](b Builder[R], ptr *value.Nullable[V]) Field[R, value.Nullable[V]] {
	return bindField(b, ptr, codec.Nullable(ormcore.NumberCodec[V]()))
}

// NullableEnumOf binds nullable enums with their generated descriptor. Example: rows.NullableEnumOf(b,&r.Status).
func NullableEnumOf[R any, E interface {
	enum.Scalar
	EnumDescriptor() enum.Descriptor[E]
}](b Builder[R], ptr *value.Nullable[E]) Field[R, value.Nullable[E]] {
	base, check := ormcore.EnumCodec[E]()
	f := bindField(b, ptr, codec.Nullable(base))
	f.core.check = check
	return f
}

// NullableTimeOf chooses a nullable temporal codec. Example: rows.NullableTimeOf(b,&r.DeletedAt).
func NullableTimeOf[R any, V orm.TimeValue](b Builder[R], ptr *value.Nullable[V]) Field[R, value.Nullable[V]] {
	return bindField(b, ptr, codec.Nullable(ormcore.TimeCodec[V]()))
}

// NullableBytesOf binds nullable owned buffers. Example: rows.NullableBytesOf(b,&r.Payload).
func NullableBytesOf[R any, V ~[]byte](b Builder[R], ptr *value.Nullable[V]) Field[R, value.Nullable[V]] {
	return bindField(b, ptr, codec.Nullable(codec.Bytes[V]()))
}
