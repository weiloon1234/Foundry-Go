package orm

import "github.com/weiloon1234/Foundry-Go/internal/ormcore"
import "github.com/weiloon1234/Foundry-Go/database/encrypted"
import "github.com/weiloon1234/Foundry-Go/value"
import "github.com/weiloon1234/Foundry-Go/temporal"
import "github.com/weiloon1234/Foundry-Go/enum"

// ColumnAdapter retains the column's typed, sealed expression/source capabilities.
// Example: ColumnExpr(column).
type ColumnAdapter[M, V any] Column[M, V]

// ColumnExpr opts into the column's sealed expression/source capabilities.
// Example: ColumnExpr(column).
func ColumnExpr[M, V any](c Column[M, V]) ColumnAdapter[M, V] { return ColumnAdapter[M, V](c) }

func (c ColumnAdapter[M, V]) ormSet(v V) Assignment[M] { return ColumnSet(Column[M, V](c), v) }

func (c ColumnAdapter[M, V]) ormColumn(*M) columnData[V] { return c.column }

func (c ColumnAdapter[M, V]) expressionColumn(*M) (ormcore.Column[V], ormcore.Column[V]) {
	return requiredExpression(c.column)
}

type keyColumnAdapter[M any, K comparable, R columnRole] keyColumn[M, K, R]

// KeyExpr opts into the column's sealed expression/source capabilities.
// Example: KeyExpr(column).
func KeyExpr[M any, K comparable, R columnRole](c keyColumn[M, K, R]) keyColumnAdapter[M, K, R] {
	return keyColumnAdapter[M, K, R](c)
}

func (c keyColumnAdapter[M, K, R]) ormSet(v K) Assignment[M] { return KeySet(keyColumn[M, K, R](c), v) }

func (c keyColumnAdapter[M, K, R]) ormColumn(*M) columnData[K] { return c.key }

func (c keyColumnAdapter[M, K, R]) expressionColumn(*M) (ormcore.Column[K], ormcore.Column[K]) {
	return requiredExpression(c.key)
}

type numberColumnAdapter[M any, V NumberValue, R columnRole] numberColumn[M, V, R]

// NumberExpr opts into the column's sealed expression/source capabilities.
// Example: NumberExpr(column).
func NumberExpr[M any, V NumberValue, R columnRole](c numberColumn[M, V, R]) numberColumnAdapter[M, V, R] {
	return numberColumnAdapter[M, V, R](c)
}

func (c numberColumnAdapter[M, V, R]) ormSet(v V) Assignment[M] {
	return NumberSet(numberColumn[M, V, R](c), v)
}

func (c numberColumnAdapter[M, V, R]) ormColumn(*M) columnData[V] { return c.number }

func (c numberColumnAdapter[M, V, R]) expressionColumn(*M) (ormcore.Column[V], ormcore.Column[V]) {
	return requiredExpression(c.number)
}

func (c numberColumnAdapter[M, V, R]) orderedColumn() {}

type textColumnAdapter[M any, V ~string, R columnRole] textColumn[M, V, R]

// TextExpr opts into the column's sealed expression/source capabilities.
// Example: TextExpr(column).
func TextExpr[M any, V ~string, R columnRole](c textColumn[M, V, R]) textColumnAdapter[M, V, R] {
	return textColumnAdapter[M, V, R](c)
}

func (c textColumnAdapter[M, V, R]) ormSet(v V) Assignment[M] {
	return TextSet(textColumn[M, V, R](c), v)
}

func (c textColumnAdapter[M, V, R]) ormContains(v V) Predicate[M] {
	return TextContains(textColumn[M, V, R](c), v)
}

func (c textColumnAdapter[M, V, R]) ormColumn(*M) columnData[V] { return c.text }

func (c textColumnAdapter[M, V, R]) expressionColumn(*M) (ormcore.Column[V], ormcore.Column[V]) {
	comparison, projection := requiredExpression(c.text)
	comparison.Text, projection.Text = true, true
	return comparison, projection
}

func (c textColumnAdapter[M, V, R]) orderedColumn() {}

type timeColumnAdapter[M any, V TimeValue, R columnRole] timeColumn[M, V, R]

// TimeExpr opts into the column's sealed expression/source capabilities.
// Example: TimeExpr(column).
func TimeExpr[M any, V TimeValue, R columnRole](c timeColumn[M, V, R]) timeColumnAdapter[M, V, R] {
	return timeColumnAdapter[M, V, R](c)
}

func (c timeColumnAdapter[M, V, R]) ormSet(v V) Assignment[M] {
	return TimeSet(timeColumn[M, V, R](c), v)
}

func (c timeColumnAdapter[M, V, R]) ormColumn(*M) columnData[V] { return c.time }

func (c timeColumnAdapter[M, V, R]) expressionColumn(*M) (ormcore.Column[V], ormcore.Column[V]) {
	return requiredExpression(c.time)
}

func (c timeColumnAdapter[M, V, R]) orderedColumn() {}

// EnumAdapter retains the column's typed, sealed expression/source capabilities.
// Example: EnumExpr(column).
type EnumAdapter[M any, E enum.Scalar] Enum[M, E]

// EnumExpr opts into the column's sealed expression/source capabilities.
// Example: EnumExpr(column).
func EnumExpr[M any, E enum.Scalar](c Enum[M, E]) EnumAdapter[M, E] { return EnumAdapter[M, E](c) }

func (c EnumAdapter[M, E]) ormSet(v E) Assignment[M] { return EnumSet(Enum[M, E](c), v) }

func (c EnumAdapter[M, E]) ormColumn(*M) columnData[E] { return c.enum }

func (c EnumAdapter[M, E]) expressionColumn(*M) (ormcore.Column[E], ormcore.Column[E]) {
	return requiredExpression(c.enum)
}

// NullableColumnAdapter retains the column's typed, sealed expression/source capabilities.
// Example: NullableColumnExpr(column).
type NullableColumnAdapter[M, V any] NullableColumn[M, V]

// NullableColumnExpr opts into the column's sealed expression/source capabilities.
// Example: NullableColumnExpr(column).
func NullableColumnExpr[M, V any](c NullableColumn[M, V]) NullableColumnAdapter[M, V] {
	return NullableColumnAdapter[M, V](c)
}

func (c NullableColumnAdapter[M, V]) ormSet(v V) Assignment[M] {
	return NullableColumnSet(NullableColumn[M, V](c), v)
}

func (c NullableColumnAdapter[M, V]) ormNullColumn(*M) columnData[V] { return c.nullableColumn.base }

func (c NullableColumnAdapter[M, V]) ormSetNull() Assignment[M] {
	return NullableColumnSetNull(NullableColumn[M, V](c))
}

func (c NullableColumnAdapter[M, V]) ormColumn(*M) columnData[V] { return c.nullableColumn.base }

func (c NullableColumnAdapter[M, V]) expressionColumn(*M) (ormcore.Column[V], ormcore.Column[value.Nullable[V]]) {
	return nullableExpression(c.nullableColumn)
}

// NullableNumberAdapter retains the column's typed, sealed expression/source capabilities.
// Example: NullableNumberExpr(column).
type NullableNumberAdapter[M any, V NumberValue] NullableNumber[M, V]

// NullableNumberExpr opts into the column's sealed expression/source capabilities.
// Example: NullableNumberExpr(column).
func NullableNumberExpr[M any, V NumberValue](c NullableNumber[M, V]) NullableNumberAdapter[M, V] {
	return NullableNumberAdapter[M, V](c)
}

func (c NullableNumberAdapter[M, V]) ormSet(v V) Assignment[M] {
	return NullableNumberSet(NullableNumber[M, V](c), v)
}

func (c NullableNumberAdapter[M, V]) ormNullColumn(*M) columnData[V] { return c.nullableNumber.base }

func (c NullableNumberAdapter[M, V]) ormSetNull() Assignment[M] {
	return NullableNumberSetNull(NullableNumber[M, V](c))
}

func (c NullableNumberAdapter[M, V]) ormColumn(*M) columnData[V] { return c.nullableNumber.base }

func (c NullableNumberAdapter[M, V]) expressionColumn(*M) (ormcore.Column[V], ormcore.Column[value.Nullable[V]]) {
	return nullableExpression(c.nullableNumber)
}

func (c NullableNumberAdapter[M, V]) orderedColumn() {}

// NullableTextAdapter retains the column's typed, sealed expression/source capabilities.
// Example: NullableTextExpr(column).
type NullableTextAdapter[M any, V ~string] NullableText[M, V]

// NullableTextExpr opts into the column's sealed expression/source capabilities.
// Example: NullableTextExpr(column).
func NullableTextExpr[M any, V ~string](c NullableText[M, V]) NullableTextAdapter[M, V] {
	return NullableTextAdapter[M, V](c)
}

func (c NullableTextAdapter[M, V]) ormSet(v V) Assignment[M] {
	return NullableTextSet(NullableText[M, V](c), v)
}

func (c NullableTextAdapter[M, V]) ormContains(v V) Predicate[M] {
	return NullableTextContains(NullableText[M, V](c), v)
}

func (c NullableTextAdapter[M, V]) ormNullColumn(*M) columnData[V] { return c.nullableText.base }

func (c NullableTextAdapter[M, V]) ormSetNull() Assignment[M] {
	return NullableTextSetNull(NullableText[M, V](c))
}

func (c NullableTextAdapter[M, V]) ormColumn(*M) columnData[V] { return c.nullableText.base }

func (c NullableTextAdapter[M, V]) expressionColumn(*M) (ormcore.Column[V], ormcore.Column[value.Nullable[V]]) {
	comparison, projection := nullableExpression(c.nullableText)
	comparison.Text, projection.Text = true, true
	return comparison, projection
}

func (c NullableTextAdapter[M, V]) orderedColumn() {}

type nullableTimeColumnAdapter[M any, V TimeValue, R columnRole] nullableTimeColumn[M, V, R]

// NullableTimeExpr opts into the column's sealed expression/source capabilities.
// Example: NullableTimeExpr(column).
func NullableTimeExpr[M any, V TimeValue, R columnRole](c nullableTimeColumn[M, V, R]) nullableTimeColumnAdapter[M, V, R] {
	return nullableTimeColumnAdapter[M, V, R](c)
}

func (c nullableTimeColumnAdapter[M, V, R]) ormSet(v V) Assignment[M] {
	return NullableTimeSet(nullableTimeColumn[M, V, R](c), v)
}

func (c nullableTimeColumnAdapter[M, V, R]) ormNullColumn(*M) columnData[V] {
	return c.nullableTime.base
}

func (c nullableTimeColumnAdapter[M, V, R]) ormSetNull() Assignment[M] {
	return NullableTimeSetNull(nullableTimeColumn[M, V, R](c))
}

func (c nullableTimeColumnAdapter[M, V, R]) ormColumn(*M) columnData[V] { return c.nullableTime.base }

func (c nullableTimeColumnAdapter[M, V, R]) expressionColumn(*M) (ormcore.Column[V], ormcore.Column[value.Nullable[V]]) {
	return nullableExpression(c.nullableTime)
}

func (c nullableTimeColumnAdapter[M, V, R]) orderedColumn() {}

// NullableEnumAdapter retains the column's typed, sealed expression/source capabilities.
// Example: NullableEnumExpr(column).
type NullableEnumAdapter[M any, E enum.Scalar] NullableEnum[M, E]

// NullableEnumExpr opts into the column's sealed expression/source capabilities.
// Example: NullableEnumExpr(column).
func NullableEnumExpr[M any, E enum.Scalar](c NullableEnum[M, E]) NullableEnumAdapter[M, E] {
	return NullableEnumAdapter[M, E](c)
}

func (c NullableEnumAdapter[M, E]) ormSet(v E) Assignment[M] {
	return NullableEnumSet(NullableEnum[M, E](c), v)
}

func (c NullableEnumAdapter[M, E]) ormNullColumn(*M) columnData[E] { return c.nullableEnum.base }

func (c NullableEnumAdapter[M, E]) ormSetNull() Assignment[M] {
	return NullableEnumSetNull(NullableEnum[M, E](c))
}

func (c NullableEnumAdapter[M, E]) ormColumn(*M) columnData[E] { return c.nullableEnum.base }

func (c NullableEnumAdapter[M, E]) expressionColumn(*M) (ormcore.Column[E], ormcore.Column[value.Nullable[E]]) {
	return nullableExpression(c.nullableEnum)
}

// InputColumnAdapter retains the column's typed, sealed expression/source capabilities.
// Example: InputColumnExpr(column).
type InputColumnAdapter[M, I any, V any] InputColumn[M, I, V]

// InputColumnExpr opts into the column's sealed expression/source capabilities.
// Example: InputColumnExpr(column).
func InputColumnExpr[M, I any, V any](c InputColumn[M, I, V]) InputColumnAdapter[M, I, V] {
	return InputColumnAdapter[M, I, V](c)
}

func (c InputColumnAdapter[M, I, V]) ormSet(v I) Assignment[M] {
	return InputColumnSet(InputColumn[M, I, V](c), v)
}

func (c InputColumnAdapter[M, I, V]) ormColumn(*M) columnData[V] { return c.inputColumnStorage }

func (c InputColumnAdapter[M, I, V]) expressionColumn(*M) (ormcore.Column[V], ormcore.Column[V]) {
	return requiredExpression(c.inputColumnStorage)
}

// NullableInputColumnAdapter retains the column's typed, sealed expression/source capabilities.
// Example: NullableInputColumnExpr(column).
type NullableInputColumnAdapter[M, I any, V any] NullableInputColumn[M, I, V]

// NullableInputColumnExpr opts into the column's sealed expression/source capabilities.
// Example: NullableInputColumnExpr(column).
func NullableInputColumnExpr[M, I any, V any](c NullableInputColumn[M, I, V]) NullableInputColumnAdapter[M, I, V] {
	return NullableInputColumnAdapter[M, I, V](c)
}

func (c NullableInputColumnAdapter[M, I, V]) ormSet(v I) Assignment[M] {
	return NullableInputColumnSet(NullableInputColumn[M, I, V](c), v)
}

func (c NullableInputColumnAdapter[M, I, V]) ormNullColumn(*M) columnData[V] {
	return c.nullableInputColumnStorage.base
}

func (c NullableInputColumnAdapter[M, I, V]) ormSetNull() Assignment[M] {
	return NullableInputColumnSetNull(NullableInputColumn[M, I, V](c))
}

func (c NullableInputColumnAdapter[M, I, V]) ormColumn(*M) columnData[V] {
	return c.nullableInputColumnStorage.base
}

func (c NullableInputColumnAdapter[M, I, V]) expressionColumn(*M) (ormcore.Column[V], ormcore.Column[value.Nullable[V]]) {
	return nullableExpression(c.nullableInputColumnStorage)
}

// InputTextAdapter retains the column's typed, sealed expression/source capabilities.
// Example: InputTextExpr(column).
type InputTextAdapter[M, I any, V ~string] InputText[M, I, V]

// InputTextExpr opts into the column's sealed expression/source capabilities.
// Example: InputTextExpr(column).
func InputTextExpr[M, I any, V ~string](c InputText[M, I, V]) InputTextAdapter[M, I, V] {
	return InputTextAdapter[M, I, V](c)
}

func (c InputTextAdapter[M, I, V]) ormSet(v I) Assignment[M] {
	return InputTextSet(InputText[M, I, V](c), v)
}

func (c InputTextAdapter[M, I, V]) ormContains(v V) Predicate[M] {
	return InputTextContains(InputText[M, I, V](c), v)
}

func (c InputTextAdapter[M, I, V]) ormColumn(*M) columnData[V] { return c.inputTextStorage }

func (c InputTextAdapter[M, I, V]) expressionColumn(*M) (ormcore.Column[V], ormcore.Column[V]) {
	comparison, projection := requiredExpression(c.inputTextStorage)
	comparison.Text, projection.Text = true, true
	return comparison, projection
}

func (c InputTextAdapter[M, I, V]) orderedColumn() {}

// NullableInputTextAdapter retains the column's typed, sealed expression/source capabilities.
// Example: NullableInputTextExpr(column).
type NullableInputTextAdapter[M, I any, V ~string] NullableInputText[M, I, V]

// NullableInputTextExpr opts into the column's sealed expression/source capabilities.
// Example: NullableInputTextExpr(column).
func NullableInputTextExpr[M, I any, V ~string](c NullableInputText[M, I, V]) NullableInputTextAdapter[M, I, V] {
	return NullableInputTextAdapter[M, I, V](c)
}

func (c NullableInputTextAdapter[M, I, V]) ormSet(v I) Assignment[M] {
	return NullableInputTextSet(NullableInputText[M, I, V](c), v)
}

func (c NullableInputTextAdapter[M, I, V]) ormContains(v V) Predicate[M] {
	return NullableInputTextContains(NullableInputText[M, I, V](c), v)
}

func (c NullableInputTextAdapter[M, I, V]) ormNullColumn(*M) columnData[V] {
	return c.nullableInputTextStorage.base
}

func (c NullableInputTextAdapter[M, I, V]) ormSetNull() Assignment[M] {
	return NullableInputTextSetNull(NullableInputText[M, I, V](c))
}

func (c NullableInputTextAdapter[M, I, V]) ormColumn(*M) columnData[V] {
	return c.nullableInputTextStorage.base
}

func (c NullableInputTextAdapter[M, I, V]) expressionColumn(*M) (ormcore.Column[V], ormcore.Column[value.Nullable[V]]) {
	comparison, projection := nullableExpression(c.nullableInputTextStorage)
	comparison.Text, projection.Text = true, true
	return comparison, projection
}

func (c NullableInputTextAdapter[M, I, V]) orderedColumn() {}

// InputNumberAdapter retains the column's typed, sealed expression/source capabilities.
// Example: InputNumberExpr(column).
type InputNumberAdapter[M, I any, V NumberValue] InputNumber[M, I, V]

// InputNumberExpr opts into the column's sealed expression/source capabilities.
// Example: InputNumberExpr(column).
func InputNumberExpr[M, I any, V NumberValue](c InputNumber[M, I, V]) InputNumberAdapter[M, I, V] {
	return InputNumberAdapter[M, I, V](c)
}

func (c InputNumberAdapter[M, I, V]) ormSet(v I) Assignment[M] {
	return InputNumberSet(InputNumber[M, I, V](c), v)
}

func (c InputNumberAdapter[M, I, V]) ormColumn(*M) columnData[V] { return c.inputNumberStorage }

func (c InputNumberAdapter[M, I, V]) expressionColumn(*M) (ormcore.Column[V], ormcore.Column[V]) {
	return requiredExpression(c.inputNumberStorage)
}

func (c InputNumberAdapter[M, I, V]) orderedColumn() {}

// NullableInputNumberAdapter retains the column's typed, sealed expression/source capabilities.
// Example: NullableInputNumberExpr(column).
type NullableInputNumberAdapter[M, I any, V NumberValue] NullableInputNumber[M, I, V]

// NullableInputNumberExpr opts into the column's sealed expression/source capabilities.
// Example: NullableInputNumberExpr(column).
func NullableInputNumberExpr[M, I any, V NumberValue](c NullableInputNumber[M, I, V]) NullableInputNumberAdapter[M, I, V] {
	return NullableInputNumberAdapter[M, I, V](c)
}

func (c NullableInputNumberAdapter[M, I, V]) ormSet(v I) Assignment[M] {
	return NullableInputNumberSet(NullableInputNumber[M, I, V](c), v)
}

func (c NullableInputNumberAdapter[M, I, V]) ormNullColumn(*M) columnData[V] {
	return c.nullableInputNumberStorage.base
}

func (c NullableInputNumberAdapter[M, I, V]) ormSetNull() Assignment[M] {
	return NullableInputNumberSetNull(NullableInputNumber[M, I, V](c))
}

func (c NullableInputNumberAdapter[M, I, V]) ormColumn(*M) columnData[V] {
	return c.nullableInputNumberStorage.base
}

func (c NullableInputNumberAdapter[M, I, V]) expressionColumn(*M) (ormcore.Column[V], ormcore.Column[value.Nullable[V]]) {
	return nullableExpression(c.nullableInputNumberStorage)
}

func (c NullableInputNumberAdapter[M, I, V]) orderedColumn() {}

// InputTimeAdapter retains the column's typed, sealed expression/source capabilities.
// Example: InputTimeExpr(column).
type InputTimeAdapter[M, I any, V TimeValue] InputTime[M, I, V]

// InputTimeExpr opts into the column's sealed expression/source capabilities.
// Example: InputTimeExpr(column).
func InputTimeExpr[M, I any, V TimeValue](c InputTime[M, I, V]) InputTimeAdapter[M, I, V] {
	return InputTimeAdapter[M, I, V](c)
}

func (c InputTimeAdapter[M, I, V]) ormSet(v I) Assignment[M] {
	return InputTimeSet(InputTime[M, I, V](c), v)
}

func (c InputTimeAdapter[M, I, V]) ormColumn(*M) columnData[V] { return c.inputTimeStorage }

func (c InputTimeAdapter[M, I, V]) expressionColumn(*M) (ormcore.Column[V], ormcore.Column[V]) {
	return requiredExpression(c.inputTimeStorage)
}

func (c InputTimeAdapter[M, I, V]) orderedColumn() {}

// NullableInputTimeAdapter retains the column's typed, sealed expression/source capabilities.
// Example: NullableInputTimeExpr(column).
type NullableInputTimeAdapter[M, I any, V TimeValue] NullableInputTime[M, I, V]

// NullableInputTimeExpr opts into the column's sealed expression/source capabilities.
// Example: NullableInputTimeExpr(column).
func NullableInputTimeExpr[M, I any, V TimeValue](c NullableInputTime[M, I, V]) NullableInputTimeAdapter[M, I, V] {
	return NullableInputTimeAdapter[M, I, V](c)
}

func (c NullableInputTimeAdapter[M, I, V]) ormSet(v I) Assignment[M] {
	return NullableInputTimeSet(NullableInputTime[M, I, V](c), v)
}

func (c NullableInputTimeAdapter[M, I, V]) ormNullColumn(*M) columnData[V] {
	return c.nullableInputTimeStorage.base
}

func (c NullableInputTimeAdapter[M, I, V]) ormSetNull() Assignment[M] {
	return NullableInputTimeSetNull(NullableInputTime[M, I, V](c))
}

func (c NullableInputTimeAdapter[M, I, V]) ormColumn(*M) columnData[V] {
	return c.nullableInputTimeStorage.base
}

func (c NullableInputTimeAdapter[M, I, V]) expressionColumn(*M) (ormcore.Column[V], ormcore.Column[value.Nullable[V]]) {
	return nullableExpression(c.nullableInputTimeStorage)
}

func (c NullableInputTimeAdapter[M, I, V]) orderedColumn() {}

// InputEnumAdapter retains the column's typed, sealed expression/source capabilities.
// Example: InputEnumExpr(column).
type InputEnumAdapter[M, I any, E interface {
	enum.Scalar
	EnumDescriptor() enum.Descriptor[E]
}] InputEnum[M, I, E]

// InputEnumExpr opts into the column's sealed expression/source capabilities.
// Example: InputEnumExpr(column).
func InputEnumExpr[M, I any, E interface {
	enum.Scalar
	EnumDescriptor() enum.Descriptor[E]
}](c InputEnum[M, I, E]) InputEnumAdapter[M, I, E] {
	return InputEnumAdapter[M, I, E](c)
}

func (c InputEnumAdapter[M, I, E]) ormSet(v I) Assignment[M] {
	return InputEnumSet(InputEnum[M, I, E](c), v)
}

func (c InputEnumAdapter[M, I, E]) ormColumn(*M) columnData[E] { return c.inputEnumStorage }

func (c InputEnumAdapter[M, I, E]) expressionColumn(*M) (ormcore.Column[E], ormcore.Column[E]) {
	return requiredExpression(c.inputEnumStorage)
}

// NullableInputEnumAdapter retains the column's typed, sealed expression/source capabilities.
// Example: NullableInputEnumExpr(column).
type NullableInputEnumAdapter[M, I any, E interface {
	enum.Scalar
	EnumDescriptor() enum.Descriptor[E]
}] NullableInputEnum[M, I, E]

// NullableInputEnumExpr opts into the column's sealed expression/source capabilities.
// Example: NullableInputEnumExpr(column).
func NullableInputEnumExpr[M, I any, E interface {
	enum.Scalar
	EnumDescriptor() enum.Descriptor[E]
}](c NullableInputEnum[M, I, E]) NullableInputEnumAdapter[M, I, E] {
	return NullableInputEnumAdapter[M, I, E](c)
}

func (c NullableInputEnumAdapter[M, I, E]) ormSet(v I) Assignment[M] {
	return NullableInputEnumSet(NullableInputEnum[M, I, E](c), v)
}

func (c NullableInputEnumAdapter[M, I, E]) ormNullColumn(*M) columnData[E] {
	return c.nullableInputEnumStorage.base
}

func (c NullableInputEnumAdapter[M, I, E]) ormSetNull() Assignment[M] {
	return NullableInputEnumSetNull(NullableInputEnum[M, I, E](c))
}

func (c NullableInputEnumAdapter[M, I, E]) ormColumn(*M) columnData[E] {
	return c.nullableInputEnumStorage.base
}

func (c NullableInputEnumAdapter[M, I, E]) expressionColumn(*M) (ormcore.Column[E], ormcore.Column[value.Nullable[E]]) {
	return nullableExpression(c.nullableInputEnumStorage)
}

// InputJSONAdapter retains the column's typed, sealed expression/source capabilities.
// Example: InputJSONExpr(column).
type InputJSONAdapter[M, I any, T any] InputJSON[M, I, T]

// InputJSONExpr opts into the column's sealed expression/source capabilities.
// Example: InputJSONExpr(column).
func InputJSONExpr[M, I any, T any](c InputJSON[M, I, T]) InputJSONAdapter[M, I, T] {
	return InputJSONAdapter[M, I, T](c)
}

func (c InputJSONAdapter[M, I, T]) ormContains(v value.JSON[T]) Predicate[M] {
	return InputJSONContains(InputJSON[M, I, T](c), v)
}

func (c InputJSONAdapter[M, I, T]) ormContainedBy(v value.JSON[T]) Predicate[M] {
	return InputJSONContainedBy(InputJSON[M, I, T](c), v)
}

func (c InputJSONAdapter[M, I, T]) ormIsJSONNull() Predicate[M] {
	return InputJSONIsJSONNull(InputJSON[M, I, T](c))
}

func (c InputJSONAdapter[M, I, T]) ormSet(v I) Assignment[M] {
	return InputJSONSet(InputJSON[M, I, T](c), v)
}

func (c InputJSONAdapter[M, I, T]) ormColumn(*M) columnData[value.JSON[T]] { return c.inputJSONStorage }

func (c InputJSONAdapter[M, I, T]) expressionColumn(*M) (ormcore.Column[value.JSON[T]], ormcore.Column[value.JSON[T]]) {
	return requiredExpression(c.inputJSONStorage)
}

// NullableInputJSONAdapter retains the column's typed, sealed expression/source capabilities.
// Example: NullableInputJSONExpr(column).
type NullableInputJSONAdapter[M, I any, T any] NullableInputJSON[M, I, T]

// NullableInputJSONExpr opts into the column's sealed expression/source capabilities.
// Example: NullableInputJSONExpr(column).
func NullableInputJSONExpr[M, I any, T any](c NullableInputJSON[M, I, T]) NullableInputJSONAdapter[M, I, T] {
	return NullableInputJSONAdapter[M, I, T](c)
}

func (c NullableInputJSONAdapter[M, I, T]) ormContains(v value.JSON[T]) Predicate[M] {
	return NullableInputJSONContains(NullableInputJSON[M, I, T](c), v)
}

func (c NullableInputJSONAdapter[M, I, T]) ormContainedBy(v value.JSON[T]) Predicate[M] {
	return NullableInputJSONContainedBy(NullableInputJSON[M, I, T](c), v)
}

func (c NullableInputJSONAdapter[M, I, T]) ormIsJSONNull() Predicate[M] {
	return NullableInputJSONIsJSONNull(NullableInputJSON[M, I, T](c))
}

func (c NullableInputJSONAdapter[M, I, T]) ormNullColumn(*M) columnData[value.JSON[T]] {
	return c.nullableInputJSONStorage.base
}

func (c NullableInputJSONAdapter[M, I, T]) ormSet(v I) Assignment[M] {
	return NullableInputJSONSet(NullableInputJSON[M, I, T](c), v)
}

func (c NullableInputJSONAdapter[M, I, T]) ormSetNull() Assignment[M] {
	return NullableInputJSONSetNull(NullableInputJSON[M, I, T](c))
}

func (c NullableInputJSONAdapter[M, I, T]) ormColumn(*M) columnData[value.JSON[T]] {
	return c.nullableInputJSONStorage.base
}

func (c NullableInputJSONAdapter[M, I, T]) expressionColumn(*M) (ormcore.Column[value.JSON[T]], ormcore.Column[value.Nullable[value.JSON[T]]]) {
	return nullableExpression(c.nullableInputJSONStorage)
}

// InputIntervalAdapter retains the column's typed, sealed expression/source capabilities.
// Example: InputIntervalExpr(column).
type InputIntervalAdapter[M, I any] InputInterval[M, I]

// InputIntervalExpr opts into the column's sealed expression/source capabilities.
// Example: InputIntervalExpr(column).
func InputIntervalExpr[M, I any](c InputInterval[M, I]) InputIntervalAdapter[M, I] {
	return InputIntervalAdapter[M, I](c)
}

func (c InputIntervalAdapter[M, I]) ormSet(v I) Assignment[M] {
	return InputIntervalSet(InputInterval[M, I](c), v)
}

func (c InputIntervalAdapter[M, I]) ormColumn(*M) columnData[temporal.Interval] {
	return c.inputIntervalStorage
}

func (c InputIntervalAdapter[M, I]) expressionColumn(*M) (ormcore.Column[temporal.Interval], ormcore.Column[temporal.Interval]) {
	return requiredExpression(c.inputIntervalStorage)
}

func (c InputIntervalAdapter[M, I]) orderedColumn() {}

// NullableInputIntervalAdapter retains the column's typed, sealed expression/source capabilities.
// Example: NullableInputIntervalExpr(column).
type NullableInputIntervalAdapter[M, I any] NullableInputInterval[M, I]

// NullableInputIntervalExpr opts into the column's sealed expression/source capabilities.
// Example: NullableInputIntervalExpr(column).
func NullableInputIntervalExpr[M, I any](c NullableInputInterval[M, I]) NullableInputIntervalAdapter[M, I] {
	return NullableInputIntervalAdapter[M, I](c)
}

func (c NullableInputIntervalAdapter[M, I]) ormSet(v I) Assignment[M] {
	return NullableInputIntervalSet(NullableInputInterval[M, I](c), v)
}

func (c NullableInputIntervalAdapter[M, I]) ormNullColumn(*M) columnData[temporal.Interval] {
	return c.nullableInputIntervalStorage.base
}

func (c NullableInputIntervalAdapter[M, I]) ormSetNull() Assignment[M] {
	return NullableInputIntervalSetNull(NullableInputInterval[M, I](c))
}

func (c NullableInputIntervalAdapter[M, I]) ormColumn(*M) columnData[temporal.Interval] {
	return c.nullableInputIntervalStorage.base
}

func (c NullableInputIntervalAdapter[M, I]) expressionColumn(*M) (ormcore.Column[temporal.Interval], ormcore.Column[value.Nullable[temporal.Interval]]) {
	return nullableExpression(c.nullableInputIntervalStorage)
}

func (c NullableInputIntervalAdapter[M, I]) orderedColumn() {}

// JSONAdapter retains the column's typed, sealed expression/source capabilities.
// Example: JSONExpr(column).
type JSONAdapter[M, T any] JSON[M, T]

// JSONExpr opts into the column's sealed expression/source capabilities.
// Example: JSONExpr(column).
func JSONExpr[M, T any](c JSON[M, T]) JSONAdapter[M, T] { return JSONAdapter[M, T](c) }

func (c JSONAdapter[M, T]) ormContains(v value.JSON[T]) Predicate[M] {
	return JSONContains(JSON[M, T](c), v)
}

func (c JSONAdapter[M, T]) ormContainedBy(v value.JSON[T]) Predicate[M] {
	return JSONContainedBy(JSON[M, T](c), v)
}

func (c JSONAdapter[M, T]) ormIsJSONNull() Predicate[M] { return JSONIsJSONNull(JSON[M, T](c)) }

func (c JSONAdapter[M, T]) ormSet(v value.JSON[T]) Assignment[M] { return JSONSet(JSON[M, T](c), v) }

func (c JSONAdapter[M, T]) ormColumn(*M) columnData[value.JSON[T]] { return c.json }

func (c JSONAdapter[M, T]) expressionColumn(*M) (ormcore.Column[value.JSON[T]], ormcore.Column[value.JSON[T]]) {
	return requiredExpression(c.json)
}

// NullableJSONAdapter retains the column's typed, sealed expression/source capabilities.
// Example: NullableJSONExpr(column).
type NullableJSONAdapter[M, T any] NullableJSON[M, T]

// NullableJSONExpr opts into the column's sealed expression/source capabilities.
// Example: NullableJSONExpr(column).
func NullableJSONExpr[M, T any](c NullableJSON[M, T]) NullableJSONAdapter[M, T] {
	return NullableJSONAdapter[M, T](c)
}

func (c NullableJSONAdapter[M, T]) ormContains(v value.JSON[T]) Predicate[M] {
	return NullableJSONContains(NullableJSON[M, T](c), v)
}

func (c NullableJSONAdapter[M, T]) ormContainedBy(v value.JSON[T]) Predicate[M] {
	return NullableJSONContainedBy(NullableJSON[M, T](c), v)
}

func (c NullableJSONAdapter[M, T]) ormIsJSONNull() Predicate[M] {
	return NullableJSONIsJSONNull(NullableJSON[M, T](c))
}

func (c NullableJSONAdapter[M, T]) ormNullColumn(*M) columnData[value.JSON[T]] {
	return c.nullableJSON.base
}

func (c NullableJSONAdapter[M, T]) ormSet(v value.JSON[T]) Assignment[M] {
	return NullableJSONSet(NullableJSON[M, T](c), v)
}

func (c NullableJSONAdapter[M, T]) ormSetNull() Assignment[M] {
	return NullableJSONSetNull(NullableJSON[M, T](c))
}

func (c NullableJSONAdapter[M, T]) ormColumn(*M) columnData[value.JSON[T]] {
	return c.nullableJSON.base
}

func (c NullableJSONAdapter[M, T]) expressionColumn(*M) (ormcore.Column[value.JSON[T]], ormcore.Column[value.Nullable[value.JSON[T]]]) {
	return nullableExpression(c.nullableJSON)
}

// IntervalAdapter retains the column's typed, sealed expression/source capabilities.
// Example: IntervalExpr(column).
type IntervalAdapter[M any] Interval[M]

// IntervalExpr opts into the column's sealed expression/source capabilities.
// Example: IntervalExpr(column).
func IntervalExpr[M any](c Interval[M]) IntervalAdapter[M] { return IntervalAdapter[M](c) }

func (c IntervalAdapter[M]) ormSet(v temporal.Interval) Assignment[M] {
	return IntervalSet(Interval[M](c), v)
}

func (c IntervalAdapter[M]) ormColumn(*M) columnData[temporal.Interval] { return c.interval }

func (c IntervalAdapter[M]) expressionColumn(*M) (ormcore.Column[temporal.Interval], ormcore.Column[temporal.Interval]) {
	return requiredExpression(c.interval)
}

func (c IntervalAdapter[M]) orderedColumn() {}

// NullableIntervalAdapter retains the column's typed, sealed expression/source capabilities.
// Example: NullableIntervalExpr(column).
type NullableIntervalAdapter[M any] NullableInterval[M]

// NullableIntervalExpr opts into the column's sealed expression/source capabilities.
// Example: NullableIntervalExpr(column).
func NullableIntervalExpr[M any](c NullableInterval[M]) NullableIntervalAdapter[M] {
	return NullableIntervalAdapter[M](c)
}

func (c NullableIntervalAdapter[M]) ormSet(v temporal.Interval) Assignment[M] {
	return NullableIntervalSet(NullableInterval[M](c), v)
}

func (c NullableIntervalAdapter[M]) ormNullColumn(*M) columnData[temporal.Interval] {
	return c.nullableInterval.base
}

func (c NullableIntervalAdapter[M]) ormSetNull() Assignment[M] {
	return NullableIntervalSetNull(NullableInterval[M](c))
}

func (c NullableIntervalAdapter[M]) ormColumn(*M) columnData[temporal.Interval] {
	return c.nullableInterval.base
}

func (c NullableIntervalAdapter[M]) expressionColumn(*M) (ormcore.Column[temporal.Interval], ormcore.Column[value.Nullable[temporal.Interval]]) {
	return nullableExpression(c.nullableInterval)
}

func (c NullableIntervalAdapter[M]) orderedColumn() {}

// EncryptedAdapter retains the column's typed, sealed expression/source capabilities.
// Example: EncryptedExpr(column).
type EncryptedAdapter[M any, V encrypted.Value[V]] Encrypted[M, V]

// EncryptedExpr opts into the column's sealed expression/source capabilities.
// Example: EncryptedExpr(column).
func EncryptedExpr[M any, V encrypted.Value[V]](c Encrypted[M, V]) EncryptedAdapter[M, V] {
	return EncryptedAdapter[M, V](c)
}

func (c EncryptedAdapter[M, V]) ormSet(v V) Assignment[M] { return EncryptedSet(Encrypted[M, V](c), v) }

// NullableEncryptedAdapter retains the column's typed, sealed expression/source capabilities.
// Example: NullableEncryptedExpr(column).
type NullableEncryptedAdapter[M any, V encrypted.Value[V]] NullableEncrypted[M, V]

// NullableEncryptedExpr opts into the column's sealed expression/source capabilities.
// Example: NullableEncryptedExpr(column).
func NullableEncryptedExpr[M any, V encrypted.Value[V]](c NullableEncrypted[M, V]) NullableEncryptedAdapter[M, V] {
	return NullableEncryptedAdapter[M, V](c)
}

func (c NullableEncryptedAdapter[M, V]) ormSet(v V) Assignment[M] {
	return NullableEncryptedSet(NullableEncrypted[M, V](c), v)
}

func (c NullableEncryptedAdapter[M, V]) ormSetNull() Assignment[M] {
	return NullableEncryptedSetNull(NullableEncrypted[M, V](c))
}

func (c NullableEncryptedAdapter[M, V]) ormNullColumn(*M) columnData[V] { return c.encrypted.base }

type numberKeyColumnAdapter[M any, V NumberValue, R keyRole] numberKeyColumn[M, V, R]

// NumberKeyExpr opts into the column's sealed expression/source capabilities.
// Example: NumberKeyExpr(column).
func NumberKeyExpr[M any, V NumberValue, R keyRole](c numberKeyColumn[M, V, R]) numberKeyColumnAdapter[M, V, R] {
	return numberKeyColumnAdapter[M, V, R](c)
}

func (c numberKeyColumnAdapter[M, V, R]) ormSet(v V) Assignment[M] {
	return NumberKeySet(numberKeyColumn[M, V, R](c), v)
}

func (c numberKeyColumnAdapter[M, V, R]) ormColumn(*M) columnData[V] { return c.number }

func (c numberKeyColumnAdapter[M, V, R]) expressionColumn(*M) (ormcore.Column[V], ormcore.Column[V]) {
	return requiredExpression(c.number)
}

func (c numberKeyColumnAdapter[M, V, R]) orderedColumn() {}

// InputKeyAdapter retains the column's typed, sealed expression/source capabilities.
// Example: InputKeyExpr(column).
type InputKeyAdapter[M, I any, K comparable] InputKey[M, I, K]

// InputKeyExpr opts into the column's sealed expression/source capabilities.
// Example: InputKeyExpr(column).
func InputKeyExpr[M, I any, K comparable](c InputKey[M, I, K]) InputKeyAdapter[M, I, K] {
	return InputKeyAdapter[M, I, K](c)
}

func (c InputKeyAdapter[M, I, K]) ormSet(v I) Assignment[M] {
	return InputKeySet(InputKey[M, I, K](c), v)
}

func (c InputKeyAdapter[M, I, K]) ormColumn(*M) columnData[K] { return c.key }

func (c InputKeyAdapter[M, I, K]) expressionColumn(*M) (ormcore.Column[K], ormcore.Column[K]) {
	return requiredExpression(c.key)
}
