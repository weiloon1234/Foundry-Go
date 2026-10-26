package orm

import (
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/enum"
	"github.com/weiloon1234/Foundry-Go/internal/ormcore"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

// InputColumn accepts I for writes and retains V for stored operations.
// Example: InputColumn[Member, EmailInput, StoredEmail].
type InputColumn[M, I any, V any] struct {
	_                  [0]*M
	inputColumnStorage columnData[V]
	input              func(I) querycore.AssignmentValue
}

// InputColumnOf binds a distinct input. A nil transform selects the model Mutate method.
// Example: InputColumnOf[Member, EmailInput](b, &member.Email, nil).
func InputColumnOf[M, I any, V any](b Builder[M], ptr *V, transform func(I) (V, error), opts ...ColumnOption[V]) InputColumn[M, I, V] {
	c := selectedCodec(opts)
	stored, input := bindInput(b.state, ptr, c, transform)
	bindNamedMutator[M, I, V](stored.core)
	return InputColumn[M, I, V]{inputColumnStorage: stored, input: input}
}

// Eq retains the stored column operation; Set alone accepts I.
// Example: members.Email.Eq(StoredEmail("ada@example.test")).
func (c InputColumn[M, I, V]) Eq(v V) Predicate[M] {
	return comparison[M](c.inputColumnStorage, querycore.Equal, []V{v})
}

// Ne retains the stored column operation; Set alone accepts I.
// Example: members.Email.Ne(StoredEmail("ada@example.test")).
func (c InputColumn[M, I, V]) Ne(v V) Predicate[M] {
	return comparison[M](c.inputColumnStorage, querycore.NotEqual, []V{v})
}

// In retains the stored column operation; Set alone accepts I.
// Example: members.Email.In(StoredEmail("ada@example.test")).
func (c InputColumn[M, I, V]) In(v ...V) Predicate[M] {
	return comparison[M](c.inputColumnStorage, querycore.In, v)
}

// NotIn retains the stored column operation; Set alone accepts I.
// Example: members.Email.NotIn(StoredEmail("ada@example.test")).
func (c InputColumn[M, I, V]) NotIn(v ...V) Predicate[M] {
	return comparison[M](c.inputColumnStorage, querycore.NotIn, v)
}

// Asc retains the stored column operation; Set alone accepts I.
// Example: members.Email.Asc().
func (c InputColumn[M, I, V]) Asc() Order[M] { return columnOrder[M](c.inputColumnStorage, false) }

// Desc retains the stored column operation; Set alone accepts I.
// Example: members.Email.Desc().
func (c InputColumn[M, I, V]) Desc() Order[M] { return columnOrder[M](c.inputColumnStorage, true) }

// Set captures a typed input without invoking its transform.
// Example: members.Email.Set(EmailInput{Address: "ada@example.test"}).
func (c InputColumn[M, I, V]) Set(v I) Assignment[M] {
	return inputAssignment[M](c.inputColumnStorage.core, c.input, v)
}

func (c InputColumn[M, I, V]) ormColumn(*M) columnData[V] { return c.inputColumnStorage }

func (c InputColumn[M, I, V]) expressionColumn(*M) (ormcore.Column[V], ormcore.Column[V]) {
	return requiredExpression(c.inputColumnStorage)
}

// NullableInputColumn accepts I for writes and retains V for stored operations.
// Example: NullableInputColumn[Member, LabelInput, StoredLabel].
type NullableInputColumn[M, I any, V any] struct {
	_                          [0]*M
	nullableInputColumnStorage nullableColumnData[V]
	input                      func(I) querycore.AssignmentValue
}

// NullableInputColumnOf binds a distinct input. A nil transform selects the model Mutate method.
// Example: NullableInputColumnOf[Member, LabelInput](b, &member.Note, nil).
func NullableInputColumnOf[M, I any, V any](b Builder[M], ptr *value.Nullable[V], transform func(I) (V, error), opts ...ColumnOption[V]) NullableInputColumn[M, I, V] {
	c := selectedCodec(opts)
	stored, input := bindNullableInput(b.state, ptr, c, transform)
	bindNamedMutator[M, I, V](stored.base.core)
	return NullableInputColumn[M, I, V]{nullableInputColumnStorage: stored, input: input}
}

// Eq retains the stored column operation; Set alone accepts I.
// Example: members.Note.Eq(StoredLabel("ada@example.test")).
func (c NullableInputColumn[M, I, V]) Eq(v V) Predicate[M] {
	return comparison[M](c.nullableInputColumnStorage.base, querycore.Equal, []V{v})
}

// Ne retains the stored column operation; Set alone accepts I.
// Example: members.Note.Ne(StoredLabel("ada@example.test")).
func (c NullableInputColumn[M, I, V]) Ne(v V) Predicate[M] {
	return comparison[M](c.nullableInputColumnStorage.base, querycore.NotEqual, []V{v})
}

// In retains the stored column operation; Set alone accepts I.
// Example: members.Note.In(StoredLabel("ada@example.test")).
func (c NullableInputColumn[M, I, V]) In(v ...V) Predicate[M] {
	return comparison[M](c.nullableInputColumnStorage.base, querycore.In, v)
}

// NotIn retains the stored column operation; Set alone accepts I.
// Example: members.Note.NotIn(StoredLabel("ada@example.test")).
func (c NullableInputColumn[M, I, V]) NotIn(v ...V) Predicate[M] {
	return comparison[M](c.nullableInputColumnStorage.base, querycore.NotIn, v)
}

// Asc retains the stored column operation; Set alone accepts I.
// Example: members.Note.Asc().
func (c NullableInputColumn[M, I, V]) Asc() Order[M] {
	return columnOrder[M](c.nullableInputColumnStorage.base, false)
}

// Desc retains the stored column operation; Set alone accepts I.
// Example: members.Note.Desc().
func (c NullableInputColumn[M, I, V]) Desc() Order[M] {
	return columnOrder[M](c.nullableInputColumnStorage.base, true)
}

// Set captures a typed input without invoking its transform.
// Example: members.Note.Set(LabelInput{Text: "ada@example.test"}).
func (c NullableInputColumn[M, I, V]) Set(v I) Assignment[M] {
	return inputAssignment[M](c.nullableInputColumnStorage.base.core, c.input, v)
}

// IsNull retains the stored column operation; Set alone accepts I.
// Example: members.Note.IsNull().
func (c NullableInputColumn[M, I, V]) IsNull() Predicate[M] {
	return comparison[M](c.nullableInputColumnStorage.base, querycore.IsNull, nil)
}

// IsNotNull retains the stored column operation; Set alone accepts I.
// Example: members.Note.IsNotNull().
func (c NullableInputColumn[M, I, V]) IsNotNull() Predicate[M] {
	return comparison[M](c.nullableInputColumnStorage.base, querycore.IsNotNull, nil)
}

// SetNull retains the stored column operation; Set alone accepts I.
// Example: members.Note.SetNull().
func (c NullableInputColumn[M, I, V]) SetNull() Assignment[M] {
	return nullAssignment[M](c.nullableInputColumnStorage.base)
}

func (c NullableInputColumn[M, I, V]) ormColumn(*M) columnData[V] {
	return c.nullableInputColumnStorage.base
}

func (c NullableInputColumn[M, I, V]) expressionColumn(*M) (ormcore.Column[V], ormcore.Column[value.Nullable[V]]) {
	return nullableExpression(c.nullableInputColumnStorage)
}

// InputText accepts I for writes and retains V for stored operations.
// Example: InputText[Member, EmailInput, StoredEmail].
type InputText[M, I any, V ~string] struct {
	_                [0]*M
	inputTextStorage columnData[V]
	input            func(I) querycore.AssignmentValue
}

// InputTextOf binds a distinct input. A nil transform selects the model Mutate method.
// Example: InputTextOf[Member, EmailInput](b, &member.Email, nil).
func InputTextOf[M, I any, V ~string](b Builder[M], ptr *V, transform func(I) (V, error), opts ...ColumnOption[V]) InputText[M, I, V] {
	c := codec.String[V]()
	stored, input := bindInput(b.state, ptr, optionCodec(c, opts), transform)
	bindNamedMutator[M, I, V](stored.core)
	return InputText[M, I, V]{inputTextStorage: stored, input: input}
}

// Eq retains the stored text operation; Set alone accepts I.
// Example: members.Email.Eq(StoredEmail("ada@example.test")).
func (c InputText[M, I, V]) Eq(v V) Predicate[M] {
	return comparison[M](c.inputTextStorage, querycore.Equal, []V{v})
}

// Ne retains the stored text operation; Set alone accepts I.
// Example: members.Email.Ne(StoredEmail("ada@example.test")).
func (c InputText[M, I, V]) Ne(v V) Predicate[M] {
	return comparison[M](c.inputTextStorage, querycore.NotEqual, []V{v})
}

// In retains the stored text operation; Set alone accepts I.
// Example: members.Email.In(StoredEmail("ada@example.test")).
func (c InputText[M, I, V]) In(v ...V) Predicate[M] {
	return comparison[M](c.inputTextStorage, querycore.In, v)
}

// NotIn retains the stored text operation; Set alone accepts I.
// Example: members.Email.NotIn(StoredEmail("ada@example.test")).
func (c InputText[M, I, V]) NotIn(v ...V) Predicate[M] {
	return comparison[M](c.inputTextStorage, querycore.NotIn, v)
}

// Asc retains the stored text operation; Set alone accepts I.
// Example: members.Email.Asc().
func (c InputText[M, I, V]) Asc() Order[M] { return columnOrder[M](c.inputTextStorage, false) }

// Desc retains the stored text operation; Set alone accepts I.
// Example: members.Email.Desc().
func (c InputText[M, I, V]) Desc() Order[M] { return columnOrder[M](c.inputTextStorage, true) }

// Set captures a typed input without invoking its transform.
// Example: members.Email.Set(EmailInput{Address: "ada@example.test"}).
func (c InputText[M, I, V]) Set(v I) Assignment[M] {
	return inputAssignment[M](c.inputTextStorage.core, c.input, v)
}

// Gt retains the stored text operation; Set alone accepts I.
// Example: members.Email.Gt(StoredEmail("ada@example.test")).
func (c InputText[M, I, V]) Gt(v V) Predicate[M] {
	return comparison[M](c.inputTextStorage, querycore.Greater, []V{v})
}

// Gte retains the stored text operation; Set alone accepts I.
// Example: members.Email.Gte(StoredEmail("ada@example.test")).
func (c InputText[M, I, V]) Gte(v V) Predicate[M] {
	return comparison[M](c.inputTextStorage, querycore.GreaterOrEqual, []V{v})
}

// Lt retains the stored text operation; Set alone accepts I.
// Example: members.Email.Lt(StoredEmail("ada@example.test")).
func (c InputText[M, I, V]) Lt(v V) Predicate[M] {
	return comparison[M](c.inputTextStorage, querycore.Less, []V{v})
}

// Lte retains the stored text operation; Set alone accepts I.
// Example: members.Email.Lte(StoredEmail("ada@example.test")).
func (c InputText[M, I, V]) Lte(v V) Predicate[M] {
	return comparison[M](c.inputTextStorage, querycore.LessOrEqual, []V{v})
}

// Between retains the stored text operation; Set alone accepts I.
// Example: members.Email.Between(StoredEmail("a"), StoredEmail("z")).
func (c InputText[M, I, V]) Between(a, b V) Predicate[M] { return between[M](c.inputTextStorage, a, b) }

// Contains retains the stored text operation; Set alone accepts I.
// Example: members.Email.Contains(StoredEmail("ada@example.test")).
func (c InputText[M, I, V]) Contains(v V) Predicate[M] {
	return comparison[M](c.inputTextStorage, querycore.Contains, []V{v}, true)
}

// IContains retains the stored text operation; Set alone accepts I.
// Example: members.Email.IContains(StoredEmail("ada@example.test")).
func (c InputText[M, I, V]) IContains(v V) Predicate[M] {
	return comparison[M](c.inputTextStorage, querycore.InsensitiveContains, []V{v}, true)
}

// StartsWith retains the stored text operation; Set alone accepts I.
// Example: members.Email.StartsWith(StoredEmail("ada@example.test")).
func (c InputText[M, I, V]) StartsWith(v V) Predicate[M] {
	return comparison[M](c.inputTextStorage, querycore.StartsWith, []V{v}, true)
}

// EndsWith retains the stored text operation; Set alone accepts I.
// Example: members.Email.EndsWith(StoredEmail("ada@example.test")).
func (c InputText[M, I, V]) EndsWith(v V) Predicate[M] {
	return comparison[M](c.inputTextStorage, querycore.EndsWith, []V{v}, true)
}

// Like retains the stored text operation; Set alone accepts I.
// Example: members.Email.Like(StoredEmail("ada@example.test")).
func (c InputText[M, I, V]) Like(v V) Predicate[M] {
	return comparison[M](c.inputTextStorage, querycore.Like, []V{v}, true)
}

// ILike retains the stored text operation; Set alone accepts I.
// Example: members.Email.ILike(StoredEmail("ada@example.test")).
func (c InputText[M, I, V]) ILike(v V) Predicate[M] {
	return comparison[M](c.inputTextStorage, querycore.InsensitiveLike, []V{v}, true)
}

// IStartsWith retains the stored text operation; Set alone accepts I.
// Example: members.Email.IStartsWith(StoredEmail("ada@example.test")).
func (c InputText[M, I, V]) IStartsWith(v V) Predicate[M] {
	return comparison[M](c.inputTextStorage, querycore.InsensitiveStartsWith, []V{v}, true)
}

// IEndsWith retains the stored text operation; Set alone accepts I.
// Example: members.Email.IEndsWith(StoredEmail("ada@example.test")).
func (c InputText[M, I, V]) IEndsWith(v V) Predicate[M] {
	return comparison[M](c.inputTextStorage, querycore.InsensitiveEndsWith, []V{v}, true)
}

func (c InputText[M, I, V]) ormColumn(*M) columnData[V] { return c.inputTextStorage }

func (c InputText[M, I, V]) expressionColumn(*M) (ormcore.Column[V], ormcore.Column[V]) {
	comparison, projection := requiredExpression(c.inputTextStorage)
	comparison.Text, projection.Text = true, true
	return comparison, projection
}

// NullableInputText accepts I for writes and retains V for stored operations.
// Example: NullableInputText[Member, LabelInput, StoredLabel].
type NullableInputText[M, I any, V ~string] struct {
	_                        [0]*M
	nullableInputTextStorage nullableColumnData[V]
	input                    func(I) querycore.AssignmentValue
}

// NullableInputTextOf binds a distinct input. A nil transform selects the model Mutate method.
// Example: NullableInputTextOf[Member, LabelInput](b, &member.Note, nil).
func NullableInputTextOf[M, I any, V ~string](b Builder[M], ptr *value.Nullable[V], transform func(I) (V, error), opts ...ColumnOption[V]) NullableInputText[M, I, V] {
	c := codec.String[V]()
	stored, input := bindNullableInput(b.state, ptr, optionCodec(c, opts), transform)
	bindNamedMutator[M, I, V](stored.base.core)
	return NullableInputText[M, I, V]{nullableInputTextStorage: stored, input: input}
}

// Eq retains the stored text operation; Set alone accepts I.
// Example: members.Note.Eq(StoredLabel("ada@example.test")).
func (c NullableInputText[M, I, V]) Eq(v V) Predicate[M] {
	return comparison[M](c.nullableInputTextStorage.base, querycore.Equal, []V{v})
}

// Ne retains the stored text operation; Set alone accepts I.
// Example: members.Note.Ne(StoredLabel("ada@example.test")).
func (c NullableInputText[M, I, V]) Ne(v V) Predicate[M] {
	return comparison[M](c.nullableInputTextStorage.base, querycore.NotEqual, []V{v})
}

// In retains the stored text operation; Set alone accepts I.
// Example: members.Note.In(StoredLabel("ada@example.test")).
func (c NullableInputText[M, I, V]) In(v ...V) Predicate[M] {
	return comparison[M](c.nullableInputTextStorage.base, querycore.In, v)
}

// NotIn retains the stored text operation; Set alone accepts I.
// Example: members.Note.NotIn(StoredLabel("ada@example.test")).
func (c NullableInputText[M, I, V]) NotIn(v ...V) Predicate[M] {
	return comparison[M](c.nullableInputTextStorage.base, querycore.NotIn, v)
}

// Asc retains the stored text operation; Set alone accepts I.
// Example: members.Note.Asc().
func (c NullableInputText[M, I, V]) Asc() Order[M] {
	return columnOrder[M](c.nullableInputTextStorage.base, false)
}

// Desc retains the stored text operation; Set alone accepts I.
// Example: members.Note.Desc().
func (c NullableInputText[M, I, V]) Desc() Order[M] {
	return columnOrder[M](c.nullableInputTextStorage.base, true)
}

// Set captures a typed input without invoking its transform.
// Example: members.Note.Set(LabelInput{Text: "ada@example.test"}).
func (c NullableInputText[M, I, V]) Set(v I) Assignment[M] {
	return inputAssignment[M](c.nullableInputTextStorage.base.core, c.input, v)
}

// Gt retains the stored text operation; Set alone accepts I.
// Example: members.Note.Gt(StoredLabel("ada@example.test")).
func (c NullableInputText[M, I, V]) Gt(v V) Predicate[M] {
	return comparison[M](c.nullableInputTextStorage.base, querycore.Greater, []V{v})
}

// Gte retains the stored text operation; Set alone accepts I.
// Example: members.Note.Gte(StoredLabel("ada@example.test")).
func (c NullableInputText[M, I, V]) Gte(v V) Predicate[M] {
	return comparison[M](c.nullableInputTextStorage.base, querycore.GreaterOrEqual, []V{v})
}

// Lt retains the stored text operation; Set alone accepts I.
// Example: members.Note.Lt(StoredLabel("ada@example.test")).
func (c NullableInputText[M, I, V]) Lt(v V) Predicate[M] {
	return comparison[M](c.nullableInputTextStorage.base, querycore.Less, []V{v})
}

// Lte retains the stored text operation; Set alone accepts I.
// Example: members.Note.Lte(StoredLabel("ada@example.test")).
func (c NullableInputText[M, I, V]) Lte(v V) Predicate[M] {
	return comparison[M](c.nullableInputTextStorage.base, querycore.LessOrEqual, []V{v})
}

// Between retains the stored text operation; Set alone accepts I.
// Example: members.Note.Between(StoredLabel("a"), StoredLabel("z")).
func (c NullableInputText[M, I, V]) Between(a, b V) Predicate[M] {
	return between[M](c.nullableInputTextStorage.base, a, b)
}

// Contains retains the stored text operation; Set alone accepts I.
// Example: members.Note.Contains(StoredLabel("ada@example.test")).
func (c NullableInputText[M, I, V]) Contains(v V) Predicate[M] {
	return comparison[M](c.nullableInputTextStorage.base, querycore.Contains, []V{v}, true)
}

// IContains retains the stored text operation; Set alone accepts I.
// Example: members.Note.IContains(StoredLabel("ada@example.test")).
func (c NullableInputText[M, I, V]) IContains(v V) Predicate[M] {
	return comparison[M](c.nullableInputTextStorage.base, querycore.InsensitiveContains, []V{v}, true)
}

// StartsWith retains the stored text operation; Set alone accepts I.
// Example: members.Note.StartsWith(StoredLabel("ada@example.test")).
func (c NullableInputText[M, I, V]) StartsWith(v V) Predicate[M] {
	return comparison[M](c.nullableInputTextStorage.base, querycore.StartsWith, []V{v}, true)
}

// EndsWith retains the stored text operation; Set alone accepts I.
// Example: members.Note.EndsWith(StoredLabel("ada@example.test")).
func (c NullableInputText[M, I, V]) EndsWith(v V) Predicate[M] {
	return comparison[M](c.nullableInputTextStorage.base, querycore.EndsWith, []V{v}, true)
}

// Like retains the stored text operation; Set alone accepts I.
// Example: members.Note.Like(StoredLabel("ada@example.test")).
func (c NullableInputText[M, I, V]) Like(v V) Predicate[M] {
	return comparison[M](c.nullableInputTextStorage.base, querycore.Like, []V{v}, true)
}

// ILike retains the stored text operation; Set alone accepts I.
// Example: members.Note.ILike(StoredLabel("ada@example.test")).
func (c NullableInputText[M, I, V]) ILike(v V) Predicate[M] {
	return comparison[M](c.nullableInputTextStorage.base, querycore.InsensitiveLike, []V{v}, true)
}

// IStartsWith retains the stored text operation; Set alone accepts I.
// Example: members.Note.IStartsWith(StoredLabel("ada@example.test")).
func (c NullableInputText[M, I, V]) IStartsWith(v V) Predicate[M] {
	return comparison[M](c.nullableInputTextStorage.base, querycore.InsensitiveStartsWith, []V{v}, true)
}

// IEndsWith retains the stored text operation; Set alone accepts I.
// Example: members.Note.IEndsWith(StoredLabel("ada@example.test")).
func (c NullableInputText[M, I, V]) IEndsWith(v V) Predicate[M] {
	return comparison[M](c.nullableInputTextStorage.base, querycore.InsensitiveEndsWith, []V{v}, true)
}

// IsNull retains the stored text operation; Set alone accepts I.
// Example: members.Note.IsNull().
func (c NullableInputText[M, I, V]) IsNull() Predicate[M] {
	return comparison[M](c.nullableInputTextStorage.base, querycore.IsNull, nil)
}

// IsNotNull retains the stored text operation; Set alone accepts I.
// Example: members.Note.IsNotNull().
func (c NullableInputText[M, I, V]) IsNotNull() Predicate[M] {
	return comparison[M](c.nullableInputTextStorage.base, querycore.IsNotNull, nil)
}

// SetNull retains the stored text operation; Set alone accepts I.
// Example: members.Note.SetNull().
func (c NullableInputText[M, I, V]) SetNull() Assignment[M] {
	return nullAssignment[M](c.nullableInputTextStorage.base)
}

func (c NullableInputText[M, I, V]) ormColumn(*M) columnData[V] {
	return c.nullableInputTextStorage.base
}

func (c NullableInputText[M, I, V]) expressionColumn(*M) (ormcore.Column[V], ormcore.Column[value.Nullable[V]]) {
	comparison, projection := nullableExpression(c.nullableInputTextStorage)
	comparison.Text, projection.Text = true, true
	return comparison, projection
}

// InputNumber accepts I for writes and retains V for stored operations.
// Example: InputNumber[Invoice, AmountInput, int64].
type InputNumber[M, I any, V NumberValue] struct {
	_                  [0]*M
	inputNumberStorage columnData[V]
	input              func(I) querycore.AssignmentValue
}

// InputNumberOf binds a distinct input. A nil transform selects the model Mutate method.
// Example: InputNumberOf[Invoice, AmountInput](b, &invoice.Amount, nil).
func InputNumberOf[M, I any, V NumberValue](b Builder[M], ptr *V, transform func(I) (V, error), opts ...ColumnOption[V]) InputNumber[M, I, V] {
	c := ormcore.NumberCodec[V]()
	stored, input := bindInput(b.state, ptr, optionCodec(c, opts), transform)
	bindNamedMutator[M, I, V](stored.core)
	return InputNumber[M, I, V]{inputNumberStorage: stored, input: input}
}

// Eq retains the stored number operation; Set alone accepts I.
// Example: invoices.Amount.Eq(int64(100)).
func (c InputNumber[M, I, V]) Eq(v V) Predicate[M] {
	return comparison[M](c.inputNumberStorage, querycore.Equal, []V{v})
}

// Ne retains the stored number operation; Set alone accepts I.
// Example: invoices.Amount.Ne(int64(100)).
func (c InputNumber[M, I, V]) Ne(v V) Predicate[M] {
	return comparison[M](c.inputNumberStorage, querycore.NotEqual, []V{v})
}

// In retains the stored number operation; Set alone accepts I.
// Example: invoices.Amount.In(int64(100)).
func (c InputNumber[M, I, V]) In(v ...V) Predicate[M] {
	return comparison[M](c.inputNumberStorage, querycore.In, v)
}

// NotIn retains the stored number operation; Set alone accepts I.
// Example: invoices.Amount.NotIn(int64(100)).
func (c InputNumber[M, I, V]) NotIn(v ...V) Predicate[M] {
	return comparison[M](c.inputNumberStorage, querycore.NotIn, v)
}

// Asc retains the stored number operation; Set alone accepts I.
// Example: invoices.Amount.Asc().
func (c InputNumber[M, I, V]) Asc() Order[M] { return columnOrder[M](c.inputNumberStorage, false) }

// Desc retains the stored number operation; Set alone accepts I.
// Example: invoices.Amount.Desc().
func (c InputNumber[M, I, V]) Desc() Order[M] { return columnOrder[M](c.inputNumberStorage, true) }

// Set captures a typed input without invoking its transform.
// Example: invoices.Amount.Set(AmountInput{Cents: 100}).
func (c InputNumber[M, I, V]) Set(v I) Assignment[M] {
	return inputAssignment[M](c.inputNumberStorage.core, c.input, v)
}

// Gt retains the stored number operation; Set alone accepts I.
// Example: invoices.Amount.Gt(int64(100)).
func (c InputNumber[M, I, V]) Gt(v V) Predicate[M] {
	return comparison[M](c.inputNumberStorage, querycore.Greater, []V{v})
}

// Gte retains the stored number operation; Set alone accepts I.
// Example: invoices.Amount.Gte(int64(100)).
func (c InputNumber[M, I, V]) Gte(v V) Predicate[M] {
	return comparison[M](c.inputNumberStorage, querycore.GreaterOrEqual, []V{v})
}

// Lt retains the stored number operation; Set alone accepts I.
// Example: invoices.Amount.Lt(int64(100)).
func (c InputNumber[M, I, V]) Lt(v V) Predicate[M] {
	return comparison[M](c.inputNumberStorage, querycore.Less, []V{v})
}

// Lte retains the stored number operation; Set alone accepts I.
// Example: invoices.Amount.Lte(int64(100)).
func (c InputNumber[M, I, V]) Lte(v V) Predicate[M] {
	return comparison[M](c.inputNumberStorage, querycore.LessOrEqual, []V{v})
}

// Between retains the stored number operation; Set alone accepts I.
// Example: invoices.Amount.Between(int64(0), int64(1000)).
func (c InputNumber[M, I, V]) Between(a, b V) Predicate[M] {
	return between[M](c.inputNumberStorage, a, b)
}

func (c InputNumber[M, I, V]) ormColumn(*M) columnData[V] { return c.inputNumberStorage }

func (c InputNumber[M, I, V]) expressionColumn(*M) (ormcore.Column[V], ormcore.Column[V]) {
	return requiredExpression(c.inputNumberStorage)
}

// NullableInputNumber accepts I for writes and retains V for stored operations.
// Example: NullableInputNumber[Invoice, AmountInput, int64].
type NullableInputNumber[M, I any, V NumberValue] struct {
	_                          [0]*M
	nullableInputNumberStorage nullableColumnData[V]
	input                      func(I) querycore.AssignmentValue
}

// NullableInputNumberOf binds a distinct input. A nil transform selects the model Mutate method.
// Example: NullableInputNumberOf[Invoice, AmountInput](b, &invoice.Amount, nil).
func NullableInputNumberOf[M, I any, V NumberValue](b Builder[M], ptr *value.Nullable[V], transform func(I) (V, error), opts ...ColumnOption[V]) NullableInputNumber[M, I, V] {
	c := ormcore.NumberCodec[V]()
	stored, input := bindNullableInput(b.state, ptr, optionCodec(c, opts), transform)
	bindNamedMutator[M, I, V](stored.base.core)
	return NullableInputNumber[M, I, V]{nullableInputNumberStorage: stored, input: input}
}

// Eq retains the stored number operation; Set alone accepts I.
// Example: invoices.OptionalAmount.Eq(int64(100)).
func (c NullableInputNumber[M, I, V]) Eq(v V) Predicate[M] {
	return comparison[M](c.nullableInputNumberStorage.base, querycore.Equal, []V{v})
}

// Ne retains the stored number operation; Set alone accepts I.
// Example: invoices.OptionalAmount.Ne(int64(100)).
func (c NullableInputNumber[M, I, V]) Ne(v V) Predicate[M] {
	return comparison[M](c.nullableInputNumberStorage.base, querycore.NotEqual, []V{v})
}

// In retains the stored number operation; Set alone accepts I.
// Example: invoices.OptionalAmount.In(int64(100)).
func (c NullableInputNumber[M, I, V]) In(v ...V) Predicate[M] {
	return comparison[M](c.nullableInputNumberStorage.base, querycore.In, v)
}

// NotIn retains the stored number operation; Set alone accepts I.
// Example: invoices.OptionalAmount.NotIn(int64(100)).
func (c NullableInputNumber[M, I, V]) NotIn(v ...V) Predicate[M] {
	return comparison[M](c.nullableInputNumberStorage.base, querycore.NotIn, v)
}

// Asc retains the stored number operation; Set alone accepts I.
// Example: invoices.OptionalAmount.Asc().
func (c NullableInputNumber[M, I, V]) Asc() Order[M] {
	return columnOrder[M](c.nullableInputNumberStorage.base, false)
}

// Desc retains the stored number operation; Set alone accepts I.
// Example: invoices.OptionalAmount.Desc().
func (c NullableInputNumber[M, I, V]) Desc() Order[M] {
	return columnOrder[M](c.nullableInputNumberStorage.base, true)
}

// Set captures a typed input without invoking its transform.
// Example: invoices.OptionalAmount.Set(AmountInput{Cents: 100}).
func (c NullableInputNumber[M, I, V]) Set(v I) Assignment[M] {
	return inputAssignment[M](c.nullableInputNumberStorage.base.core, c.input, v)
}

// Gt retains the stored number operation; Set alone accepts I.
// Example: invoices.OptionalAmount.Gt(int64(100)).
func (c NullableInputNumber[M, I, V]) Gt(v V) Predicate[M] {
	return comparison[M](c.nullableInputNumberStorage.base, querycore.Greater, []V{v})
}

// Gte retains the stored number operation; Set alone accepts I.
// Example: invoices.OptionalAmount.Gte(int64(100)).
func (c NullableInputNumber[M, I, V]) Gte(v V) Predicate[M] {
	return comparison[M](c.nullableInputNumberStorage.base, querycore.GreaterOrEqual, []V{v})
}

// Lt retains the stored number operation; Set alone accepts I.
// Example: invoices.OptionalAmount.Lt(int64(100)).
func (c NullableInputNumber[M, I, V]) Lt(v V) Predicate[M] {
	return comparison[M](c.nullableInputNumberStorage.base, querycore.Less, []V{v})
}

// Lte retains the stored number operation; Set alone accepts I.
// Example: invoices.OptionalAmount.Lte(int64(100)).
func (c NullableInputNumber[M, I, V]) Lte(v V) Predicate[M] {
	return comparison[M](c.nullableInputNumberStorage.base, querycore.LessOrEqual, []V{v})
}

// Between retains the stored number operation; Set alone accepts I.
// Example: invoices.OptionalAmount.Between(int64(0), int64(1000)).
func (c NullableInputNumber[M, I, V]) Between(a, b V) Predicate[M] {
	return between[M](c.nullableInputNumberStorage.base, a, b)
}

// IsNull retains the stored number operation; Set alone accepts I.
// Example: invoices.OptionalAmount.IsNull().
func (c NullableInputNumber[M, I, V]) IsNull() Predicate[M] {
	return comparison[M](c.nullableInputNumberStorage.base, querycore.IsNull, nil)
}

// IsNotNull retains the stored number operation; Set alone accepts I.
// Example: invoices.OptionalAmount.IsNotNull().
func (c NullableInputNumber[M, I, V]) IsNotNull() Predicate[M] {
	return comparison[M](c.nullableInputNumberStorage.base, querycore.IsNotNull, nil)
}

// SetNull retains the stored number operation; Set alone accepts I.
// Example: invoices.OptionalAmount.SetNull().
func (c NullableInputNumber[M, I, V]) SetNull() Assignment[M] {
	return nullAssignment[M](c.nullableInputNumberStorage.base)
}

func (c NullableInputNumber[M, I, V]) ormColumn(*M) columnData[V] {
	return c.nullableInputNumberStorage.base
}

func (c NullableInputNumber[M, I, V]) expressionColumn(*M) (ormcore.Column[V], ormcore.Column[value.Nullable[V]]) {
	return nullableExpression(c.nullableInputNumberStorage)
}

// InputTime accepts I for writes and retains V for stored operations.
// Example: InputTime[Event, TimestampInput, time.Time].
type InputTime[M, I any, V TimeValue] struct {
	_                [0]*M
	inputTimeStorage columnData[V]
	input            func(I) querycore.AssignmentValue
}

// InputTimeOf binds a distinct input. A nil transform selects the model Mutate method.
// Example: InputTimeOf[Event, TimestampInput](b, &event.StartsAt, nil).
func InputTimeOf[M, I any, V TimeValue](b Builder[M], ptr *V, transform func(I) (V, error), opts ...ColumnOption[V]) InputTime[M, I, V] {
	c := ormcore.TimeCodec[V]()
	stored, input := bindInput(b.state, ptr, optionCodec(c, opts), transform)
	bindNamedMutator[M, I, V](stored.core)
	return InputTime[M, I, V]{inputTimeStorage: stored, input: input}
}

// Eq retains the stored time operation; Set alone accepts I.
// Example: events.StartsAt.Eq(time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)).
func (c InputTime[M, I, V]) Eq(v V) Predicate[M] {
	return comparison[M](c.inputTimeStorage, querycore.Equal, []V{v})
}

// Ne retains the stored time operation; Set alone accepts I.
// Example: events.StartsAt.Ne(time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)).
func (c InputTime[M, I, V]) Ne(v V) Predicate[M] {
	return comparison[M](c.inputTimeStorage, querycore.NotEqual, []V{v})
}

// In retains the stored time operation; Set alone accepts I.
// Example: events.StartsAt.In(time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)).
func (c InputTime[M, I, V]) In(v ...V) Predicate[M] {
	return comparison[M](c.inputTimeStorage, querycore.In, v)
}

// NotIn retains the stored time operation; Set alone accepts I.
// Example: events.StartsAt.NotIn(time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)).
func (c InputTime[M, I, V]) NotIn(v ...V) Predicate[M] {
	return comparison[M](c.inputTimeStorage, querycore.NotIn, v)
}

// Asc retains the stored time operation; Set alone accepts I.
// Example: events.StartsAt.Asc().
func (c InputTime[M, I, V]) Asc() Order[M] { return columnOrder[M](c.inputTimeStorage, false) }

// Desc retains the stored time operation; Set alone accepts I.
// Example: events.StartsAt.Desc().
func (c InputTime[M, I, V]) Desc() Order[M] { return columnOrder[M](c.inputTimeStorage, true) }

// Set captures a typed input without invoking its transform.
// Example: events.StartsAt.Set(TimestampInput{Time: startsAt}).
func (c InputTime[M, I, V]) Set(v I) Assignment[M] {
	return inputAssignment[M](c.inputTimeStorage.core, c.input, v)
}

// Gt retains the stored time operation; Set alone accepts I.
// Example: events.StartsAt.Gt(time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)).
func (c InputTime[M, I, V]) Gt(v V) Predicate[M] {
	return comparison[M](c.inputTimeStorage, querycore.Greater, []V{v})
}

// Gte retains the stored time operation; Set alone accepts I.
// Example: events.StartsAt.Gte(time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)).
func (c InputTime[M, I, V]) Gte(v V) Predicate[M] {
	return comparison[M](c.inputTimeStorage, querycore.GreaterOrEqual, []V{v})
}

// Lt retains the stored time operation; Set alone accepts I.
// Example: events.StartsAt.Lt(time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)).
func (c InputTime[M, I, V]) Lt(v V) Predicate[M] {
	return comparison[M](c.inputTimeStorage, querycore.Less, []V{v})
}

// Lte retains the stored time operation; Set alone accepts I.
// Example: events.StartsAt.Lte(time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)).
func (c InputTime[M, I, V]) Lte(v V) Predicate[M] {
	return comparison[M](c.inputTimeStorage, querycore.LessOrEqual, []V{v})
}

// Between retains the stored time operation; Set alone accepts I.
// Example: events.StartsAt.Between(startsAt, endsAt).
func (c InputTime[M, I, V]) Between(a, b V) Predicate[M] { return between[M](c.inputTimeStorage, a, b) }

func (c InputTime[M, I, V]) ormColumn(*M) columnData[V] { return c.inputTimeStorage }

func (c InputTime[M, I, V]) expressionColumn(*M) (ormcore.Column[V], ormcore.Column[V]) {
	return requiredExpression(c.inputTimeStorage)
}

// NullableInputTime accepts I for writes and retains V for stored operations.
// Example: NullableInputTime[Event, TimestampInput, time.Time].
type NullableInputTime[M, I any, V TimeValue] struct {
	_                        [0]*M
	nullableInputTimeStorage nullableColumnData[V]
	input                    func(I) querycore.AssignmentValue
}

// NullableInputTimeOf binds a distinct input. A nil transform selects the model Mutate method.
// Example: NullableInputTimeOf[Event, TimestampInput](b, &event.StartsAt, nil).
func NullableInputTimeOf[M, I any, V TimeValue](b Builder[M], ptr *value.Nullable[V], transform func(I) (V, error), opts ...ColumnOption[V]) NullableInputTime[M, I, V] {
	c := ormcore.TimeCodec[V]()
	stored, input := bindNullableInput(b.state, ptr, optionCodec(c, opts), transform)
	bindNamedMutator[M, I, V](stored.base.core)
	return NullableInputTime[M, I, V]{nullableInputTimeStorage: stored, input: input}
}

// Eq retains the stored time operation; Set alone accepts I.
// Example: events.OptionalStartsAt.Eq(time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)).
func (c NullableInputTime[M, I, V]) Eq(v V) Predicate[M] {
	return comparison[M](c.nullableInputTimeStorage.base, querycore.Equal, []V{v})
}

// Ne retains the stored time operation; Set alone accepts I.
// Example: events.OptionalStartsAt.Ne(time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)).
func (c NullableInputTime[M, I, V]) Ne(v V) Predicate[M] {
	return comparison[M](c.nullableInputTimeStorage.base, querycore.NotEqual, []V{v})
}

// In retains the stored time operation; Set alone accepts I.
// Example: events.OptionalStartsAt.In(time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)).
func (c NullableInputTime[M, I, V]) In(v ...V) Predicate[M] {
	return comparison[M](c.nullableInputTimeStorage.base, querycore.In, v)
}

// NotIn retains the stored time operation; Set alone accepts I.
// Example: events.OptionalStartsAt.NotIn(time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)).
func (c NullableInputTime[M, I, V]) NotIn(v ...V) Predicate[M] {
	return comparison[M](c.nullableInputTimeStorage.base, querycore.NotIn, v)
}

// Asc retains the stored time operation; Set alone accepts I.
// Example: events.OptionalStartsAt.Asc().
func (c NullableInputTime[M, I, V]) Asc() Order[M] {
	return columnOrder[M](c.nullableInputTimeStorage.base, false)
}

// Desc retains the stored time operation; Set alone accepts I.
// Example: events.OptionalStartsAt.Desc().
func (c NullableInputTime[M, I, V]) Desc() Order[M] {
	return columnOrder[M](c.nullableInputTimeStorage.base, true)
}

// Set captures a typed input without invoking its transform.
// Example: events.OptionalStartsAt.Set(TimestampInput{Time: startsAt}).
func (c NullableInputTime[M, I, V]) Set(v I) Assignment[M] {
	return inputAssignment[M](c.nullableInputTimeStorage.base.core, c.input, v)
}

// Gt retains the stored time operation; Set alone accepts I.
// Example: events.OptionalStartsAt.Gt(time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)).
func (c NullableInputTime[M, I, V]) Gt(v V) Predicate[M] {
	return comparison[M](c.nullableInputTimeStorage.base, querycore.Greater, []V{v})
}

// Gte retains the stored time operation; Set alone accepts I.
// Example: events.OptionalStartsAt.Gte(time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)).
func (c NullableInputTime[M, I, V]) Gte(v V) Predicate[M] {
	return comparison[M](c.nullableInputTimeStorage.base, querycore.GreaterOrEqual, []V{v})
}

// Lt retains the stored time operation; Set alone accepts I.
// Example: events.OptionalStartsAt.Lt(time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)).
func (c NullableInputTime[M, I, V]) Lt(v V) Predicate[M] {
	return comparison[M](c.nullableInputTimeStorage.base, querycore.Less, []V{v})
}

// Lte retains the stored time operation; Set alone accepts I.
// Example: events.OptionalStartsAt.Lte(time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)).
func (c NullableInputTime[M, I, V]) Lte(v V) Predicate[M] {
	return comparison[M](c.nullableInputTimeStorage.base, querycore.LessOrEqual, []V{v})
}

// Between retains the stored time operation; Set alone accepts I.
// Example: events.OptionalStartsAt.Between(startsAt, endsAt).
func (c NullableInputTime[M, I, V]) Between(a, b V) Predicate[M] {
	return between[M](c.nullableInputTimeStorage.base, a, b)
}

// IsNull retains the stored time operation; Set alone accepts I.
// Example: events.OptionalStartsAt.IsNull().
func (c NullableInputTime[M, I, V]) IsNull() Predicate[M] {
	return comparison[M](c.nullableInputTimeStorage.base, querycore.IsNull, nil)
}

// IsNotNull retains the stored time operation; Set alone accepts I.
// Example: events.OptionalStartsAt.IsNotNull().
func (c NullableInputTime[M, I, V]) IsNotNull() Predicate[M] {
	return comparison[M](c.nullableInputTimeStorage.base, querycore.IsNotNull, nil)
}

// SetNull retains the stored time operation; Set alone accepts I.
// Example: events.OptionalStartsAt.SetNull().
func (c NullableInputTime[M, I, V]) SetNull() Assignment[M] {
	return nullAssignment[M](c.nullableInputTimeStorage.base)
}

func (c NullableInputTime[M, I, V]) ormColumn(*M) columnData[V] {
	return c.nullableInputTimeStorage.base
}

func (c NullableInputTime[M, I, V]) expressionColumn(*M) (ormcore.Column[V], ormcore.Column[value.Nullable[V]]) {
	return nullableExpression(c.nullableInputTimeStorage)
}

// InputEnum accepts I for writes and retains E for stored operations.
// Example: InputEnum[Member, StatusInput, Status].
type InputEnum[M, I any, E interface {
	enum.Scalar
	EnumDescriptor() enum.Descriptor[E]
}] struct {
	_                [0]*M
	inputEnumStorage columnData[E]
	input            func(I) querycore.AssignmentValue
}

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
	return InputEnum[M, I, E]{inputEnumStorage: stored, input: input}
}

// Eq retains the stored enum operation; Set alone accepts I.
// Example: members.Status.Eq(StatusActive).
func (c InputEnum[M, I, E]) Eq(v E) Predicate[M] {
	return comparison[M](c.inputEnumStorage, querycore.Equal, []E{v})
}

// Ne retains the stored enum operation; Set alone accepts I.
// Example: members.Status.Ne(StatusActive).
func (c InputEnum[M, I, E]) Ne(v E) Predicate[M] {
	return comparison[M](c.inputEnumStorage, querycore.NotEqual, []E{v})
}

// In retains the stored enum operation; Set alone accepts I.
// Example: members.Status.In(StatusActive).
func (c InputEnum[M, I, E]) In(v ...E) Predicate[M] {
	return comparison[M](c.inputEnumStorage, querycore.In, v)
}

// NotIn retains the stored enum operation; Set alone accepts I.
// Example: members.Status.NotIn(StatusActive).
func (c InputEnum[M, I, E]) NotIn(v ...E) Predicate[M] {
	return comparison[M](c.inputEnumStorage, querycore.NotIn, v)
}

// Asc retains the stored enum operation; Set alone accepts I.
// Example: members.Status.Asc().
func (c InputEnum[M, I, E]) Asc() Order[M] { return columnOrder[M](c.inputEnumStorage, false) }

// Desc retains the stored enum operation; Set alone accepts I.
// Example: members.Status.Desc().
func (c InputEnum[M, I, E]) Desc() Order[M] { return columnOrder[M](c.inputEnumStorage, true) }

// Set captures a typed input without invoking its transform.
// Example: members.Status.Set(StatusInput{Value: StatusActive}).
func (c InputEnum[M, I, E]) Set(v I) Assignment[M] {
	return inputAssignment[M](c.inputEnumStorage.core, c.input, v)
}

func (c InputEnum[M, I, E]) ormColumn(*M) columnData[E] { return c.inputEnumStorage }

func (c InputEnum[M, I, E]) expressionColumn(*M) (ormcore.Column[E], ormcore.Column[E]) {
	return requiredExpression(c.inputEnumStorage)
}

// NullableInputEnum accepts I for writes and retains E for stored operations.
// Example: NullableInputEnum[Member, StatusInput, Status].
type NullableInputEnum[M, I any, E interface {
	enum.Scalar
	EnumDescriptor() enum.Descriptor[E]
}] struct {
	_                        [0]*M
	nullableInputEnumStorage nullableColumnData[E]
	input                    func(I) querycore.AssignmentValue
}

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
	return NullableInputEnum[M, I, E]{nullableInputEnumStorage: stored, input: input}
}

// Eq retains the stored enum operation; Set alone accepts I.
// Example: members.OptionalStatus.Eq(StatusActive).
func (c NullableInputEnum[M, I, E]) Eq(v E) Predicate[M] {
	return comparison[M](c.nullableInputEnumStorage.base, querycore.Equal, []E{v})
}

// Ne retains the stored enum operation; Set alone accepts I.
// Example: members.OptionalStatus.Ne(StatusActive).
func (c NullableInputEnum[M, I, E]) Ne(v E) Predicate[M] {
	return comparison[M](c.nullableInputEnumStorage.base, querycore.NotEqual, []E{v})
}

// In retains the stored enum operation; Set alone accepts I.
// Example: members.OptionalStatus.In(StatusActive).
func (c NullableInputEnum[M, I, E]) In(v ...E) Predicate[M] {
	return comparison[M](c.nullableInputEnumStorage.base, querycore.In, v)
}

// NotIn retains the stored enum operation; Set alone accepts I.
// Example: members.OptionalStatus.NotIn(StatusActive).
func (c NullableInputEnum[M, I, E]) NotIn(v ...E) Predicate[M] {
	return comparison[M](c.nullableInputEnumStorage.base, querycore.NotIn, v)
}

// Asc retains the stored enum operation; Set alone accepts I.
// Example: members.OptionalStatus.Asc().
func (c NullableInputEnum[M, I, E]) Asc() Order[M] {
	return columnOrder[M](c.nullableInputEnumStorage.base, false)
}

// Desc retains the stored enum operation; Set alone accepts I.
// Example: members.OptionalStatus.Desc().
func (c NullableInputEnum[M, I, E]) Desc() Order[M] {
	return columnOrder[M](c.nullableInputEnumStorage.base, true)
}

// Set captures a typed input without invoking its transform.
// Example: members.OptionalStatus.Set(StatusInput{Value: StatusActive}).
func (c NullableInputEnum[M, I, E]) Set(v I) Assignment[M] {
	return inputAssignment[M](c.nullableInputEnumStorage.base.core, c.input, v)
}

// IsNull retains the stored enum operation; Set alone accepts I.
// Example: members.OptionalStatus.IsNull().
func (c NullableInputEnum[M, I, E]) IsNull() Predicate[M] {
	return comparison[M](c.nullableInputEnumStorage.base, querycore.IsNull, nil)
}

// IsNotNull retains the stored enum operation; Set alone accepts I.
// Example: members.OptionalStatus.IsNotNull().
func (c NullableInputEnum[M, I, E]) IsNotNull() Predicate[M] {
	return comparison[M](c.nullableInputEnumStorage.base, querycore.IsNotNull, nil)
}

// SetNull retains the stored enum operation; Set alone accepts I.
// Example: members.OptionalStatus.SetNull().
func (c NullableInputEnum[M, I, E]) SetNull() Assignment[M] {
	return nullAssignment[M](c.nullableInputEnumStorage.base)
}

func (c NullableInputEnum[M, I, E]) ormColumn(*M) columnData[E] {
	return c.nullableInputEnumStorage.base
}

func (c NullableInputEnum[M, I, E]) expressionColumn(*M) (ormcore.Column[E], ormcore.Column[value.Nullable[E]]) {
	return nullableExpression(c.nullableInputEnumStorage)
}

// InputJSON accepts I for writes and retains value.JSON[T] for stored operations.
// Example: InputJSON[Member, PreferencesInput, Preferences].
type InputJSON[M, I any, T any] struct {
	_                [0]*M
	inputJSONStorage columnData[value.JSON[T]]
	input            func(I) querycore.AssignmentValue
}

// InputJSONOf binds a distinct input. A nil transform selects the model Mutate method.
// Example: InputJSONOf[Member, PreferencesInput](b, &member.Settings, nil).
func InputJSONOf[M, I any, T any](b Builder[M], ptr *value.JSON[T], transform func(I) (value.JSON[T], error), opts ...ColumnOption[value.JSON[T]]) InputJSON[M, I, T] {
	c := codec.JSON[T]()
	stored, input := bindInput(b.state, ptr, optionCodec(c, opts), transform)
	bindNamedMutator[M, I, value.JSON[T]](stored.core)
	return InputJSON[M, I, T]{inputJSONStorage: stored, input: input}
}

// Eq retains the stored json operation; Set alone accepts I.
// Example: members.Settings.Eq(storedPreferences).
func (c InputJSON[M, I, T]) Eq(v value.JSON[T]) Predicate[M] {
	return comparison[M](c.inputJSONStorage, querycore.Equal, []value.JSON[T]{v})
}

// Ne retains the stored json operation; Set alone accepts I.
// Example: members.Settings.Ne(storedPreferences).
func (c InputJSON[M, I, T]) Ne(v value.JSON[T]) Predicate[M] {
	return comparison[M](c.inputJSONStorage, querycore.NotEqual, []value.JSON[T]{v})
}

// In retains the stored json operation; Set alone accepts I.
// Example: members.Settings.In(storedPreferences).
func (c InputJSON[M, I, T]) In(v ...value.JSON[T]) Predicate[M] {
	return comparison[M](c.inputJSONStorage, querycore.In, v)
}

// NotIn retains the stored json operation; Set alone accepts I.
// Example: members.Settings.NotIn(storedPreferences).
func (c InputJSON[M, I, T]) NotIn(v ...value.JSON[T]) Predicate[M] {
	return comparison[M](c.inputJSONStorage, querycore.NotIn, v)
}

// Asc retains the stored json operation; Set alone accepts I.
// Example: members.Settings.Asc().
func (c InputJSON[M, I, T]) Asc() Order[M] { return columnOrder[M](c.inputJSONStorage, false) }

// Desc retains the stored json operation; Set alone accepts I.
// Example: members.Settings.Desc().
func (c InputJSON[M, I, T]) Desc() Order[M] { return columnOrder[M](c.inputJSONStorage, true) }

// Contains retains the stored json operation; Set alone accepts I.
// Example: members.Settings.Contains(storedPreferences).
func (c InputJSON[M, I, T]) Contains(v value.JSON[T]) Predicate[M] {
	return jsonContainment[M](c.inputJSONStorage, querycore.JsonContainsOperation, v)
}

// ContainedBy retains the stored json operation; Set alone accepts I.
// Example: members.Settings.ContainedBy(storedPreferences).
func (c InputJSON[M, I, T]) ContainedBy(v value.JSON[T]) Predicate[M] {
	return jsonContainment[M](c.inputJSONStorage, querycore.JsonContainedByOperation, v)
}

// IsJSONNull retains the stored json operation; Set alone accepts I.
// Example: members.Settings.IsJSONNull().
func (c InputJSON[M, I, T]) IsJSONNull() Predicate[M] { return jsonNull[M](c.inputJSONStorage) }

// Set captures a typed input without invoking its transform.
// Example: members.Settings.Set(PreferencesInput{Theme: "dark"}).
func (c InputJSON[M, I, T]) Set(v I) Assignment[M] {
	return inputAssignment[M](c.inputJSONStorage.core, c.input, v)
}

func (c InputJSON[M, I, T]) ormColumn(*M) columnData[value.JSON[T]] { return c.inputJSONStorage }

func (c InputJSON[M, I, T]) expressionColumn(*M) (ormcore.Column[value.JSON[T]], ormcore.Column[value.JSON[T]]) {
	return requiredExpression(c.inputJSONStorage)
}

// NullableInputJSON accepts I for writes and retains value.JSON[T] for stored operations.
// Example: NullableInputJSON[Member, PreferencesInput, Preferences].
type NullableInputJSON[M, I any, T any] struct {
	_                        [0]*M
	nullableInputJSONStorage nullableColumnData[value.JSON[T]]
	input                    func(I) querycore.AssignmentValue
}

// NullableInputJSONOf binds a distinct input. A nil transform selects the model Mutate method.
// Example: NullableInputJSONOf[Member, PreferencesInput](b, &member.Settings, nil).
func NullableInputJSONOf[M, I any, T any](b Builder[M], ptr *value.Nullable[value.JSON[T]], transform func(I) (value.JSON[T], error), opts ...ColumnOption[value.JSON[T]]) NullableInputJSON[M, I, T] {
	c := codec.JSON[T]()
	stored, input := bindNullableInput(b.state, ptr, optionCodec(c, opts), transform)
	bindNamedMutator[M, I, value.JSON[T]](stored.base.core)
	return NullableInputJSON[M, I, T]{nullableInputJSONStorage: stored, input: input}
}

// Eq retains the stored json operation; Set alone accepts I.
// Example: members.BackupSettings.Eq(storedPreferences).
func (c NullableInputJSON[M, I, T]) Eq(v value.JSON[T]) Predicate[M] {
	return comparison[M](c.nullableInputJSONStorage.base, querycore.Equal, []value.JSON[T]{v})
}

// Ne retains the stored json operation; Set alone accepts I.
// Example: members.BackupSettings.Ne(storedPreferences).
func (c NullableInputJSON[M, I, T]) Ne(v value.JSON[T]) Predicate[M] {
	return comparison[M](c.nullableInputJSONStorage.base, querycore.NotEqual, []value.JSON[T]{v})
}

// In retains the stored json operation; Set alone accepts I.
// Example: members.BackupSettings.In(storedPreferences).
func (c NullableInputJSON[M, I, T]) In(v ...value.JSON[T]) Predicate[M] {
	return comparison[M](c.nullableInputJSONStorage.base, querycore.In, v)
}

// NotIn retains the stored json operation; Set alone accepts I.
// Example: members.BackupSettings.NotIn(storedPreferences).
func (c NullableInputJSON[M, I, T]) NotIn(v ...value.JSON[T]) Predicate[M] {
	return comparison[M](c.nullableInputJSONStorage.base, querycore.NotIn, v)
}

// Asc retains the stored json operation; Set alone accepts I.
// Example: members.BackupSettings.Asc().
func (c NullableInputJSON[M, I, T]) Asc() Order[M] {
	return columnOrder[M](c.nullableInputJSONStorage.base, false)
}

// Desc retains the stored json operation; Set alone accepts I.
// Example: members.BackupSettings.Desc().
func (c NullableInputJSON[M, I, T]) Desc() Order[M] {
	return columnOrder[M](c.nullableInputJSONStorage.base, true)
}

// Contains retains the stored json operation; Set alone accepts I.
// Example: members.BackupSettings.Contains(storedPreferences).
func (c NullableInputJSON[M, I, T]) Contains(v value.JSON[T]) Predicate[M] {
	return jsonContainment[M](c.nullableInputJSONStorage.base, querycore.JsonContainsOperation, v)
}

// ContainedBy retains the stored json operation; Set alone accepts I.
// Example: members.BackupSettings.ContainedBy(storedPreferences).
func (c NullableInputJSON[M, I, T]) ContainedBy(v value.JSON[T]) Predicate[M] {
	return jsonContainment[M](c.nullableInputJSONStorage.base, querycore.JsonContainedByOperation, v)
}

// IsJSONNull retains the stored json operation; Set alone accepts I.
// Example: members.BackupSettings.IsJSONNull().
func (c NullableInputJSON[M, I, T]) IsJSONNull() Predicate[M] {
	return jsonNull[M](c.nullableInputJSONStorage.base)
}

// IsNull retains the stored json operation; Set alone accepts I.
// Example: members.BackupSettings.IsNull().
func (c NullableInputJSON[M, I, T]) IsNull() Predicate[M] {
	return comparison[M](c.nullableInputJSONStorage.base, querycore.IsNull, nil)
}

// IsNotNull retains the stored json operation; Set alone accepts I.
// Example: members.BackupSettings.IsNotNull().
func (c NullableInputJSON[M, I, T]) IsNotNull() Predicate[M] {
	return comparison[M](c.nullableInputJSONStorage.base, querycore.IsNotNull, nil)
}

// Set captures a typed input without invoking its transform.
// Example: members.BackupSettings.Set(PreferencesInput{Theme: "dark"}).
func (c NullableInputJSON[M, I, T]) Set(v I) Assignment[M] {
	return inputAssignment[M](c.nullableInputJSONStorage.base.core, c.input, v)
}

// SetNull retains the stored json operation; Set alone accepts I.
// Example: members.BackupSettings.SetNull().
func (c NullableInputJSON[M, I, T]) SetNull() Assignment[M] {
	return nullAssignment[M](c.nullableInputJSONStorage.base)
}

func (c NullableInputJSON[M, I, T]) ormColumn(*M) columnData[value.JSON[T]] {
	return c.nullableInputJSONStorage.base
}

func (c NullableInputJSON[M, I, T]) expressionColumn(*M) (ormcore.Column[value.JSON[T]], ormcore.Column[value.Nullable[value.JSON[T]]]) {
	return nullableExpression(c.nullableInputJSONStorage)
}

// InputInterval accepts I for writes and retains temporal.Interval for stored operations.
// Example: InputInterval[Booking, PeriodInput].
type InputInterval[M, I any] struct {
	_                    [0]*M
	inputIntervalStorage columnData[temporal.Interval]
	input                func(I) querycore.AssignmentValue
}

// InputIntervalOf binds a distinct input. A nil transform selects the model Mutate method.
// Example: InputIntervalOf[Booking, PeriodInput](b, &booking.Period, nil).
func InputIntervalOf[M, I any](b Builder[M], ptr *temporal.Interval, transform func(I) (temporal.Interval, error), opts ...ColumnOption[temporal.Interval]) InputInterval[M, I] {
	c := codec.Interval()
	stored, input := bindInput(b.state, ptr, optionCodec(c, opts), transform)
	bindNamedMutator[M, I, temporal.Interval](stored.core)
	return InputInterval[M, I]{inputIntervalStorage: stored, input: input}
}

// Eq retains the stored interval operation; Set alone accepts I.
// Example: bookings.Period.Eq(temporal.Days(1)).
func (c InputInterval[M, I]) Eq(v temporal.Interval) Predicate[M] {
	return comparison[M](c.inputIntervalStorage, querycore.Equal, []temporal.Interval{v})
}

// Ne retains the stored interval operation; Set alone accepts I.
// Example: bookings.Period.Ne(temporal.Days(1)).
func (c InputInterval[M, I]) Ne(v temporal.Interval) Predicate[M] {
	return comparison[M](c.inputIntervalStorage, querycore.NotEqual, []temporal.Interval{v})
}

// In retains the stored interval operation; Set alone accepts I.
// Example: bookings.Period.In(temporal.Days(1)).
func (c InputInterval[M, I]) In(v ...temporal.Interval) Predicate[M] {
	return comparison[M](c.inputIntervalStorage, querycore.In, v)
}

// NotIn retains the stored interval operation; Set alone accepts I.
// Example: bookings.Period.NotIn(temporal.Days(1)).
func (c InputInterval[M, I]) NotIn(v ...temporal.Interval) Predicate[M] {
	return comparison[M](c.inputIntervalStorage, querycore.NotIn, v)
}

// Asc retains the stored interval operation; Set alone accepts I.
// Example: bookings.Period.Asc().
func (c InputInterval[M, I]) Asc() Order[M] { return columnOrder[M](c.inputIntervalStorage, false) }

// Desc retains the stored interval operation; Set alone accepts I.
// Example: bookings.Period.Desc().
func (c InputInterval[M, I]) Desc() Order[M] { return columnOrder[M](c.inputIntervalStorage, true) }

// Set captures a typed input without invoking its transform.
// Example: bookings.Period.Set(PeriodInput{Days: 1}).
func (c InputInterval[M, I]) Set(v I) Assignment[M] {
	return inputAssignment[M](c.inputIntervalStorage.core, c.input, v)
}

// Gt retains the stored interval operation; Set alone accepts I.
// Example: bookings.Period.Gt(temporal.Days(1)).
func (c InputInterval[M, I]) Gt(v temporal.Interval) Predicate[M] {
	return comparison[M](c.inputIntervalStorage, querycore.Greater, []temporal.Interval{v})
}

// Gte retains the stored interval operation; Set alone accepts I.
// Example: bookings.Period.Gte(temporal.Days(1)).
func (c InputInterval[M, I]) Gte(v temporal.Interval) Predicate[M] {
	return comparison[M](c.inputIntervalStorage, querycore.GreaterOrEqual, []temporal.Interval{v})
}

// Lt retains the stored interval operation; Set alone accepts I.
// Example: bookings.Period.Lt(temporal.Days(1)).
func (c InputInterval[M, I]) Lt(v temporal.Interval) Predicate[M] {
	return comparison[M](c.inputIntervalStorage, querycore.Less, []temporal.Interval{v})
}

// Lte retains the stored interval operation; Set alone accepts I.
// Example: bookings.Period.Lte(temporal.Days(1)).
func (c InputInterval[M, I]) Lte(v temporal.Interval) Predicate[M] {
	return comparison[M](c.inputIntervalStorage, querycore.LessOrEqual, []temporal.Interval{v})
}

// Between retains the stored interval operation; Set alone accepts I.
// Example: bookings.Period.Between(temporal.Days(1), temporal.Days(7)).
func (c InputInterval[M, I]) Between(a, b temporal.Interval) Predicate[M] {
	return between[M](c.inputIntervalStorage, a, b)
}

func (c InputInterval[M, I]) ormColumn(*M) columnData[temporal.Interval] {
	return c.inputIntervalStorage
}

func (c InputInterval[M, I]) expressionColumn(*M) (ormcore.Column[temporal.Interval], ormcore.Column[temporal.Interval]) {
	return requiredExpression(c.inputIntervalStorage)
}

// NullableInputInterval accepts I for writes and retains temporal.Interval for stored operations.
// Example: NullableInputInterval[Booking, PeriodInput].
type NullableInputInterval[M, I any] struct {
	_                            [0]*M
	nullableInputIntervalStorage nullableColumnData[temporal.Interval]
	input                        func(I) querycore.AssignmentValue
}

// NullableInputIntervalOf binds a distinct input. A nil transform selects the model Mutate method.
// Example: NullableInputIntervalOf[Booking, PeriodInput](b, &booking.Period, nil).
func NullableInputIntervalOf[M, I any](b Builder[M], ptr *value.Nullable[temporal.Interval], transform func(I) (temporal.Interval, error), opts ...ColumnOption[temporal.Interval]) NullableInputInterval[M, I] {
	c := codec.Interval()
	stored, input := bindNullableInput(b.state, ptr, optionCodec(c, opts), transform)
	bindNamedMutator[M, I, temporal.Interval](stored.base.core)
	return NullableInputInterval[M, I]{nullableInputIntervalStorage: stored, input: input}
}

// Eq retains the stored interval operation; Set alone accepts I.
// Example: bookings.OptionalPeriod.Eq(temporal.Days(1)).
func (c NullableInputInterval[M, I]) Eq(v temporal.Interval) Predicate[M] {
	return comparison[M](c.nullableInputIntervalStorage.base, querycore.Equal, []temporal.Interval{v})
}

// Ne retains the stored interval operation; Set alone accepts I.
// Example: bookings.OptionalPeriod.Ne(temporal.Days(1)).
func (c NullableInputInterval[M, I]) Ne(v temporal.Interval) Predicate[M] {
	return comparison[M](c.nullableInputIntervalStorage.base, querycore.NotEqual, []temporal.Interval{v})
}

// In retains the stored interval operation; Set alone accepts I.
// Example: bookings.OptionalPeriod.In(temporal.Days(1)).
func (c NullableInputInterval[M, I]) In(v ...temporal.Interval) Predicate[M] {
	return comparison[M](c.nullableInputIntervalStorage.base, querycore.In, v)
}

// NotIn retains the stored interval operation; Set alone accepts I.
// Example: bookings.OptionalPeriod.NotIn(temporal.Days(1)).
func (c NullableInputInterval[M, I]) NotIn(v ...temporal.Interval) Predicate[M] {
	return comparison[M](c.nullableInputIntervalStorage.base, querycore.NotIn, v)
}

// Asc retains the stored interval operation; Set alone accepts I.
// Example: bookings.OptionalPeriod.Asc().
func (c NullableInputInterval[M, I]) Asc() Order[M] {
	return columnOrder[M](c.nullableInputIntervalStorage.base, false)
}

// Desc retains the stored interval operation; Set alone accepts I.
// Example: bookings.OptionalPeriod.Desc().
func (c NullableInputInterval[M, I]) Desc() Order[M] {
	return columnOrder[M](c.nullableInputIntervalStorage.base, true)
}

// Set captures a typed input without invoking its transform.
// Example: bookings.OptionalPeriod.Set(PeriodInput{Days: 1}).
func (c NullableInputInterval[M, I]) Set(v I) Assignment[M] {
	return inputAssignment[M](c.nullableInputIntervalStorage.base.core, c.input, v)
}

// Gt retains the stored interval operation; Set alone accepts I.
// Example: bookings.OptionalPeriod.Gt(temporal.Days(1)).
func (c NullableInputInterval[M, I]) Gt(v temporal.Interval) Predicate[M] {
	return comparison[M](c.nullableInputIntervalStorage.base, querycore.Greater, []temporal.Interval{v})
}

// Gte retains the stored interval operation; Set alone accepts I.
// Example: bookings.OptionalPeriod.Gte(temporal.Days(1)).
func (c NullableInputInterval[M, I]) Gte(v temporal.Interval) Predicate[M] {
	return comparison[M](c.nullableInputIntervalStorage.base, querycore.GreaterOrEqual, []temporal.Interval{v})
}

// Lt retains the stored interval operation; Set alone accepts I.
// Example: bookings.OptionalPeriod.Lt(temporal.Days(1)).
func (c NullableInputInterval[M, I]) Lt(v temporal.Interval) Predicate[M] {
	return comparison[M](c.nullableInputIntervalStorage.base, querycore.Less, []temporal.Interval{v})
}

// Lte retains the stored interval operation; Set alone accepts I.
// Example: bookings.OptionalPeriod.Lte(temporal.Days(1)).
func (c NullableInputInterval[M, I]) Lte(v temporal.Interval) Predicate[M] {
	return comparison[M](c.nullableInputIntervalStorage.base, querycore.LessOrEqual, []temporal.Interval{v})
}

// Between retains the stored interval operation; Set alone accepts I.
// Example: bookings.OptionalPeriod.Between(temporal.Days(1), temporal.Days(7)).
func (c NullableInputInterval[M, I]) Between(a, b temporal.Interval) Predicate[M] {
	return between[M](c.nullableInputIntervalStorage.base, a, b)
}

// IsNull retains the stored interval operation; Set alone accepts I.
// Example: bookings.OptionalPeriod.IsNull().
func (c NullableInputInterval[M, I]) IsNull() Predicate[M] {
	return comparison[M](c.nullableInputIntervalStorage.base, querycore.IsNull, nil)
}

// IsNotNull retains the stored interval operation; Set alone accepts I.
// Example: bookings.OptionalPeriod.IsNotNull().
func (c NullableInputInterval[M, I]) IsNotNull() Predicate[M] {
	return comparison[M](c.nullableInputIntervalStorage.base, querycore.IsNotNull, nil)
}

// SetNull retains the stored interval operation; Set alone accepts I.
// Example: bookings.OptionalPeriod.SetNull().
func (c NullableInputInterval[M, I]) SetNull() Assignment[M] {
	return nullAssignment[M](c.nullableInputIntervalStorage.base)
}

func (c NullableInputInterval[M, I]) ormColumn(*M) columnData[temporal.Interval] {
	return c.nullableInputIntervalStorage.base
}

func (c NullableInputInterval[M, I]) expressionColumn(*M) (ormcore.Column[temporal.Interval], ormcore.Column[value.Nullable[temporal.Interval]]) {
	return nullableExpression(c.nullableInputIntervalStorage)
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
