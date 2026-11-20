package orm

import (
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/enum"
	"github.com/weiloon1234/Foundry-Go/internal/ormcore"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

// InputColumn accepts I for writes and retains V for stored operations.
// Example: InputColumn[Member, EmailInput, StoredEmail].
type InputColumn[M, I any, V any] = ColumnHandle[M, V, I, V, RequiredScalarKind, InputRole]

// InputColumnOf binds a distinct input. A nil transform selects the model Mutate method.
// Example: InputColumnOf[Member, EmailInput](b, &member.Email, nil).
func InputColumnOf[M, I any, V any](b Builder[M], ptr *V, transform func(I) (V, error), opts ...ColumnOption[V]) InputColumn[M, I, V] {
	c := selectedCodec(opts)
	stored, input := bindInput(b.state, ptr, c, transform)
	bindNamedMutator[M, I, V](stored.core)
	return InputColumn[M, I, V]{stored: &stored, input: input}
}

// NullableInputColumn accepts I for writes and retains V for stored operations.
// Example: NullableInputColumn[Member, LabelInput, StoredLabel].
type NullableInputColumn[M, I any, V any] = ColumnHandle[M, V, I, value.Nullable[V], NullableScalarKind, InputRole]

// NullableInputColumnOf binds a distinct input. A nil transform selects the model Mutate method.
// Example: NullableInputColumnOf[Member, LabelInput](b, &member.Note, nil).
func NullableInputColumnOf[M, I any, V any](b Builder[M], ptr *value.Nullable[V], transform func(I) (V, error), opts ...ColumnOption[V]) NullableInputColumn[M, I, V] {
	c := selectedCodec(opts)
	stored, input := bindNullableInput(b.state, ptr, c, transform)
	bindNamedMutator[M, I, V](stored.base.core)
	return NullableInputColumn[M, I, V]{stored: &stored.base, input: input}
}

// InputText accepts I for writes and retains V for stored operations.
// Example: InputText[Member, EmailInput, StoredEmail].
type InputText[M, I any, V ~string] = ColumnHandle[M, V, I, V, RequiredTextKind, InputRole]

// InputTextOf binds a distinct input. A nil transform selects the model Mutate method.
// Example: InputTextOf[Member, EmailInput](b, &member.Email, nil).
func InputTextOf[M, I any, V ~string](b Builder[M], ptr *V, transform func(I) (V, error), opts ...ColumnOption[V]) InputText[M, I, V] {
	c := codec.String[V]()
	stored, input := bindInput(b.state, ptr, optionCodec(c, opts), transform)
	bindNamedMutator[M, I, V](stored.core)
	return InputText[M, I, V]{stored: &stored, input: input}
}

// NullableInputText accepts I for writes and retains V for stored operations.
// Example: NullableInputText[Member, LabelInput, StoredLabel].
type NullableInputText[M, I any, V ~string] = ColumnHandle[M, V, I, value.Nullable[V], NullableTextKind, InputRole]

// NullableInputTextOf binds a distinct input. A nil transform selects the model Mutate method.
// Example: NullableInputTextOf[Member, LabelInput](b, &member.Note, nil).
func NullableInputTextOf[M, I any, V ~string](b Builder[M], ptr *value.Nullable[V], transform func(I) (V, error), opts ...ColumnOption[V]) NullableInputText[M, I, V] {
	c := codec.String[V]()
	stored, input := bindNullableInput(b.state, ptr, optionCodec(c, opts), transform)
	bindNamedMutator[M, I, V](stored.base.core)
	return NullableInputText[M, I, V]{stored: &stored.base, input: input}
}

// InputNumber accepts I for writes and retains V for stored operations.
// Example: InputNumber[Invoice, AmountInput, int64].
type InputNumber[M, I any, V NumberValue] = ColumnHandle[M, V, I, V, RequiredNumberKind, InputRole]

// InputNumberOf binds a distinct input. A nil transform selects the model Mutate method.
// Example: InputNumberOf[Invoice, AmountInput](b, &invoice.Amount, nil).
func InputNumberOf[M, I any, V NumberValue](b Builder[M], ptr *V, transform func(I) (V, error), opts ...ColumnOption[V]) InputNumber[M, I, V] {
	c := ormcore.NumberCodec[V]()
	stored, input := bindInput(b.state, ptr, optionCodec(c, opts), transform)
	bindNamedMutator[M, I, V](stored.core)
	return InputNumber[M, I, V]{stored: &stored, input: input}
}

// NullableInputNumber accepts I for writes and retains V for stored operations.
// Example: NullableInputNumber[Invoice, AmountInput, int64].
type NullableInputNumber[M, I any, V NumberValue] = ColumnHandle[M, V, I, value.Nullable[V], NullableNumberKind, InputRole]

// NullableInputNumberOf binds a distinct input. A nil transform selects the model Mutate method.
// Example: NullableInputNumberOf[Invoice, AmountInput](b, &invoice.Amount, nil).
func NullableInputNumberOf[M, I any, V NumberValue](b Builder[M], ptr *value.Nullable[V], transform func(I) (V, error), opts ...ColumnOption[V]) NullableInputNumber[M, I, V] {
	c := ormcore.NumberCodec[V]()
	stored, input := bindNullableInput(b.state, ptr, optionCodec(c, opts), transform)
	bindNamedMutator[M, I, V](stored.base.core)
	return NullableInputNumber[M, I, V]{stored: &stored.base, input: input}
}

// InputTime accepts I for writes and retains V for stored operations.
// Example: InputTime[Event, TimestampInput, time.Time].
type InputTime[M, I any, V TimeValue] = ColumnHandle[M, V, I, V, RequiredTimeKind, InputRole]

// InputTimeOf binds a distinct input. A nil transform selects the model Mutate method.
// Example: InputTimeOf[Event, TimestampInput](b, &event.StartsAt, nil).
func InputTimeOf[M, I any, V TimeValue](b Builder[M], ptr *V, transform func(I) (V, error), opts ...ColumnOption[V]) InputTime[M, I, V] {
	c := ormcore.TimeCodec[V]()
	stored, input := bindInput(b.state, ptr, optionCodec(c, opts), transform)
	bindNamedMutator[M, I, V](stored.core)
	return InputTime[M, I, V]{stored: &stored, input: input}
}

// NullableInputTime accepts I for writes and retains V for stored operations.
// Example: NullableInputTime[Event, TimestampInput, time.Time].
type NullableInputTime[M, I any, V TimeValue] = ColumnHandle[M, V, I, value.Nullable[V], NullableTimeKind, InputRole]

// NullableInputTimeOf binds a distinct input. A nil transform selects the model Mutate method.
// Example: NullableInputTimeOf[Event, TimestampInput](b, &event.StartsAt, nil).
func NullableInputTimeOf[M, I any, V TimeValue](b Builder[M], ptr *value.Nullable[V], transform func(I) (V, error), opts ...ColumnOption[V]) NullableInputTime[M, I, V] {
	c := ormcore.TimeCodec[V]()
	stored, input := bindNullableInput(b.state, ptr, optionCodec(c, opts), transform)
	bindNamedMutator[M, I, V](stored.base.core)
	return NullableInputTime[M, I, V]{stored: &stored.base, input: input}
}

// InputEnum accepts I for writes and retains E for stored operations.
// Example: InputEnum[Member, StatusInput, Status].
type InputEnum[M, I any, E interface {
	enum.Scalar
	EnumDescriptor() enum.Descriptor[E]
}] = ColumnHandle[M, E, I, E, RequiredEnumKind, InputRole]

// InputEnumOf binds a distinct input. A nil transform selects the model Mutate method.
// Example: InputEnumOf[Member, StatusInput](b, &member.Status, nil).
func InputEnumOf[M, I any, E interface {
	enum.Scalar
	EnumDescriptor() enum.Descriptor[E]
}](b Builder[M], ptr *E, transform func(I) (E, error), opts ...ColumnOption[E]) InputEnum[M, I, E] {
	c, check := enumCodec[E]()
	stored, input := bindInput(b.state, ptr, optionCodec(c, opts), transform)
	bindNamedMutator[M, I, E](stored.core)
	stored.core.check = check
	checkEnumOptions(b.state, stored.core, opts)
	return InputEnum[M, I, E]{stored: &stored, input: input}
}

// NullableInputEnum accepts I for writes and retains E for stored operations.
// Example: NullableInputEnum[Member, StatusInput, Status].
type NullableInputEnum[M, I any, E interface {
	enum.Scalar
	EnumDescriptor() enum.Descriptor[E]
}] = ColumnHandle[M, E, I, value.Nullable[E], NullableEnumKind, InputRole]

// NullableInputEnumOf binds a distinct input. A nil transform selects the model Mutate method.
// Example: NullableInputEnumOf[Member, StatusInput](b, &member.Status, nil).
func NullableInputEnumOf[M, I any, E interface {
	enum.Scalar
	EnumDescriptor() enum.Descriptor[E]
}](b Builder[M], ptr *value.Nullable[E], transform func(I) (E, error), opts ...ColumnOption[E]) NullableInputEnum[M, I, E] {
	c, check := enumCodec[E]()
	stored, input := bindNullableInput(b.state, ptr, optionCodec(c, opts), transform)
	bindNamedMutator[M, I, E](stored.base.core)
	stored.base.core.check = check
	checkEnumOptions(b.state, stored.base.core, opts)
	return NullableInputEnum[M, I, E]{stored: &stored.base, input: input}
}

// InputJSON accepts I for writes and retains value.JSON[T] for stored operations.
// Example: InputJSON[Member, PreferencesInput, Preferences].
type InputJSON[M, I any, T any] = ColumnHandle[M, value.JSON[T], I, value.JSON[T], RequiredJSONKind, InputRole]

// InputJSONOf binds a distinct input. A nil transform selects the model Mutate method.
// Example: InputJSONOf[Member, PreferencesInput](b, &member.Settings, nil).
func InputJSONOf[M, I any, T any](b Builder[M], ptr *value.JSON[T], transform func(I) (value.JSON[T], error), opts ...ColumnOption[value.JSON[T]]) InputJSON[M, I, T] {
	c := codec.JSON[T]()
	stored, input := bindInput(b.state, ptr, optionCodec(c, opts), transform)
	bindNamedMutator[M, I, value.JSON[T]](stored.core)
	return InputJSON[M, I, T]{stored: &stored, input: input}
}

// NullableInputJSON accepts I for writes and retains value.JSON[T] for stored operations.
// Example: NullableInputJSON[Member, PreferencesInput, Preferences].
type NullableInputJSON[M, I any, T any] = ColumnHandle[M, value.JSON[T], I, value.Nullable[value.JSON[T]], NullableJSONKind, InputRole]

// NullableInputJSONOf binds a distinct input. A nil transform selects the model Mutate method.
// Example: NullableInputJSONOf[Member, PreferencesInput](b, &member.Settings, nil).
func NullableInputJSONOf[M, I any, T any](b Builder[M], ptr *value.Nullable[value.JSON[T]], transform func(I) (value.JSON[T], error), opts ...ColumnOption[value.JSON[T]]) NullableInputJSON[M, I, T] {
	c := codec.JSON[T]()
	stored, input := bindNullableInput(b.state, ptr, optionCodec(c, opts), transform)
	bindNamedMutator[M, I, value.JSON[T]](stored.base.core)
	return NullableInputJSON[M, I, T]{stored: &stored.base, input: input}
}

// InputInterval accepts I for writes and retains temporal.Interval for stored operations.
// Example: InputInterval[Booking, PeriodInput].
type InputInterval[M, I any] = ColumnHandle[M, temporal.Interval, I, temporal.Interval, RequiredIntervalKind, InputRole]

// InputIntervalOf binds a distinct input. A nil transform selects the model Mutate method.
// Example: InputIntervalOf[Booking, PeriodInput](b, &booking.Period, nil).
func InputIntervalOf[M, I any](b Builder[M], ptr *temporal.Interval, transform func(I) (temporal.Interval, error), opts ...ColumnOption[temporal.Interval]) InputInterval[M, I] {
	c := codec.Interval()
	stored, input := bindInput(b.state, ptr, optionCodec(c, opts), transform)
	bindNamedMutator[M, I, temporal.Interval](stored.core)
	return InputInterval[M, I]{stored: &stored, input: input}
}

// NullableInputInterval accepts I for writes and retains temporal.Interval for stored operations.
// Example: NullableInputInterval[Booking, PeriodInput].
type NullableInputInterval[M, I any] = ColumnHandle[M, temporal.Interval, I, value.Nullable[temporal.Interval], NullableIntervalKind, InputRole]

// NullableInputIntervalOf binds a distinct input. A nil transform selects the model Mutate method.
// Example: NullableInputIntervalOf[Booking, PeriodInput](b, &booking.Period, nil).
func NullableInputIntervalOf[M, I any](b Builder[M], ptr *value.Nullable[temporal.Interval], transform func(I) (temporal.Interval, error), opts ...ColumnOption[temporal.Interval]) NullableInputInterval[M, I] {
	c := codec.Interval()
	stored, input := bindNullableInput(b.state, ptr, optionCodec(c, opts), transform)
	bindNamedMutator[M, I, temporal.Interval](stored.base.core)
	return NullableInputInterval[M, I]{stored: &stored.base, input: input}
}

// InputBytesOf binds owned stored bytes and a concrete input.
// Example: InputBytesOf[Document, BodyInput](b, &document.Body, encodeBody).
func InputBytesOf[M, I any, V ~[]byte](b Builder[M], ptr *V, transform func(I) (V, error)) InputColumn[M, I, V] {
	return InputColumnOf(b, ptr, transform, Codec(codec.Bytes[V]()))
}

// NullableInputBytesOf binds owned stored bytes and a concrete input.
// Example: NullableInputBytesOf[Document, BodyInput](b, &document.Body, encodeBody).
func NullableInputBytesOf[M, I any, V ~[]byte](b Builder[M], ptr *value.Nullable[V], transform func(I) (V, error)) NullableInputColumn[M, I, V] {
	return NullableInputColumnOf(b, ptr, transform, Codec(codec.Bytes[V]()))
}
