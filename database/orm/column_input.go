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

// InputColumnEq retains the stored column operation; Set alone accepts I.
// Example: InputColumnEq(members.Email, StoredEmail("ada@example.test")).
func InputColumnEq[M, I any, V any](c InputColumn[M, I, V], v V) Predicate[M] {
	return comparison[M](c.inputColumnStorage, querycore.Equal, []V{v})
}

// InputColumnNe retains the stored column operation; Set alone accepts I.
// Example: InputColumnNe(members.Email, StoredEmail("ada@example.test")).
func InputColumnNe[M, I any, V any](c InputColumn[M, I, V], v V) Predicate[M] {
	return comparison[M](c.inputColumnStorage, querycore.NotEqual, []V{v})
}

// InputColumnIn retains the stored column operation; Set alone accepts I.
// Example: InputColumnIn(members.Email, StoredEmail("ada@example.test")).
func InputColumnIn[M, I any, V any](c InputColumn[M, I, V], v ...V) Predicate[M] {
	return comparison[M](c.inputColumnStorage, querycore.In, v)
}

// InputColumnNotIn retains the stored column operation; Set alone accepts I.
// Example: InputColumnNotIn(members.Email, StoredEmail("ada@example.test")).
func InputColumnNotIn[M, I any, V any](c InputColumn[M, I, V], v ...V) Predicate[M] {
	return comparison[M](c.inputColumnStorage, querycore.NotIn, v)
}

// InputColumnAsc retains the stored column operation; Set alone accepts I.
// Example: InputColumnAsc(members.Email).
func InputColumnAsc[M, I any, V any](c InputColumn[M, I, V]) Order[M] {
	return columnOrder[M](c.inputColumnStorage, false)
}

// InputColumnDesc retains the stored column operation; Set alone accepts I.
// Example: InputColumnDesc(members.Email).
func InputColumnDesc[M, I any, V any](c InputColumn[M, I, V]) Order[M] {
	return columnOrder[M](c.inputColumnStorage, true)
}

// InputColumnSet captures a typed input without invoking its transform.
// Example: InputColumnSet(members.Email, EmailInput{Address: "ada@example.test"}).
func InputColumnSet[M, I any, V any](c InputColumn[M, I, V], v I) Assignment[M] {
	return inputAssignment[M](c.inputColumnStorage.core, c.input, v)
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

// NullableInputColumnEq retains the stored column operation; Set alone accepts I.
// Example: NullableInputColumnEq(members.Note, StoredLabel("ada@example.test")).
func NullableInputColumnEq[M, I any, V any](c NullableInputColumn[M, I, V], v V) Predicate[M] {
	return comparison[M](c.nullableInputColumnStorage.base, querycore.Equal, []V{v})
}

// NullableInputColumnNe retains the stored column operation; Set alone accepts I.
// Example: NullableInputColumnNe(members.Note, StoredLabel("ada@example.test")).
func NullableInputColumnNe[M, I any, V any](c NullableInputColumn[M, I, V], v V) Predicate[M] {
	return comparison[M](c.nullableInputColumnStorage.base, querycore.NotEqual, []V{v})
}

// NullableInputColumnIn retains the stored column operation; Set alone accepts I.
// Example: NullableInputColumnIn(members.Note, StoredLabel("ada@example.test")).
func NullableInputColumnIn[M, I any, V any](c NullableInputColumn[M, I, V], v ...V) Predicate[M] {
	return comparison[M](c.nullableInputColumnStorage.base, querycore.In, v)
}

// NullableInputColumnNotIn retains the stored column operation; Set alone accepts I.
// Example: NullableInputColumnNotIn(members.Note, StoredLabel("ada@example.test")).
func NullableInputColumnNotIn[M, I any, V any](c NullableInputColumn[M, I, V], v ...V) Predicate[M] {
	return comparison[M](c.nullableInputColumnStorage.base, querycore.NotIn, v)
}

// NullableInputColumnAsc retains the stored column operation; Set alone accepts I.
// Example: NullableInputColumnAsc(members.Note).
func NullableInputColumnAsc[M, I any, V any](c NullableInputColumn[M, I, V]) Order[M] {
	return columnOrder[M](c.nullableInputColumnStorage.base, false)
}

// NullableInputColumnDesc retains the stored column operation; Set alone accepts I.
// Example: NullableInputColumnDesc(members.Note).
func NullableInputColumnDesc[M, I any, V any](c NullableInputColumn[M, I, V]) Order[M] {
	return columnOrder[M](c.nullableInputColumnStorage.base, true)
}

// NullableInputColumnSet captures a typed input without invoking its transform.
// Example: NullableInputColumnSet(members.Note, LabelInput{Text: "ada@example.test"}).
func NullableInputColumnSet[M, I any, V any](c NullableInputColumn[M, I, V], v I) Assignment[M] {
	return inputAssignment[M](c.nullableInputColumnStorage.base.core, c.input, v)
}

// NullableInputColumnIsNull retains the stored column operation; Set alone accepts I.
// Example: NullableInputColumnIsNull(members.Note).
func NullableInputColumnIsNull[M, I any, V any](c NullableInputColumn[M, I, V]) Predicate[M] {
	return comparison[M](c.nullableInputColumnStorage.base, querycore.IsNull, nil)
}

// NullableInputColumnIsNotNull retains the stored column operation; Set alone accepts I.
// Example: NullableInputColumnIsNotNull(members.Note).
func NullableInputColumnIsNotNull[M, I any, V any](c NullableInputColumn[M, I, V]) Predicate[M] {
	return comparison[M](c.nullableInputColumnStorage.base, querycore.IsNotNull, nil)
}

// NullableInputColumnSetNull retains the stored column operation; Set alone accepts I.
// Example: NullableInputColumnSetNull(members.Note).
func NullableInputColumnSetNull[M, I any, V any](c NullableInputColumn[M, I, V]) Assignment[M] {
	return nullAssignment[M](c.nullableInputColumnStorage.base)
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

// InputTextEq retains the stored text operation; Set alone accepts I.
// Example: InputTextEq(members.Email, StoredEmail("ada@example.test")).
func InputTextEq[M, I any, V ~string](c InputText[M, I, V], v V) Predicate[M] {
	return comparison[M](c.inputTextStorage, querycore.Equal, []V{v})
}

// InputTextNe retains the stored text operation; Set alone accepts I.
// Example: InputTextNe(members.Email, StoredEmail("ada@example.test")).
func InputTextNe[M, I any, V ~string](c InputText[M, I, V], v V) Predicate[M] {
	return comparison[M](c.inputTextStorage, querycore.NotEqual, []V{v})
}

// InputTextIn retains the stored text operation; Set alone accepts I.
// Example: InputTextIn(members.Email, StoredEmail("ada@example.test")).
func InputTextIn[M, I any, V ~string](c InputText[M, I, V], v ...V) Predicate[M] {
	return comparison[M](c.inputTextStorage, querycore.In, v)
}

// InputTextNotIn retains the stored text operation; Set alone accepts I.
// Example: InputTextNotIn(members.Email, StoredEmail("ada@example.test")).
func InputTextNotIn[M, I any, V ~string](c InputText[M, I, V], v ...V) Predicate[M] {
	return comparison[M](c.inputTextStorage, querycore.NotIn, v)
}

// InputTextAsc retains the stored text operation; Set alone accepts I.
// Example: InputTextAsc(members.Email).
func InputTextAsc[M, I any, V ~string](c InputText[M, I, V]) Order[M] {
	return columnOrder[M](c.inputTextStorage, false)
}

// InputTextDesc retains the stored text operation; Set alone accepts I.
// Example: InputTextDesc(members.Email).
func InputTextDesc[M, I any, V ~string](c InputText[M, I, V]) Order[M] {
	return columnOrder[M](c.inputTextStorage, true)
}

// InputTextSet captures a typed input without invoking its transform.
// Example: InputTextSet(members.Email, EmailInput{Address: "ada@example.test"}).
func InputTextSet[M, I any, V ~string](c InputText[M, I, V], v I) Assignment[M] {
	return inputAssignment[M](c.inputTextStorage.core, c.input, v)
}

// InputTextGt retains the stored text operation; Set alone accepts I.
// Example: InputTextGt(members.Email, StoredEmail("ada@example.test")).
func InputTextGt[M, I any, V ~string](c InputText[M, I, V], v V) Predicate[M] {
	return comparison[M](c.inputTextStorage, querycore.Greater, []V{v})
}

// InputTextGte retains the stored text operation; Set alone accepts I.
// Example: InputTextGte(members.Email, StoredEmail("ada@example.test")).
func InputTextGte[M, I any, V ~string](c InputText[M, I, V], v V) Predicate[M] {
	return comparison[M](c.inputTextStorage, querycore.GreaterOrEqual, []V{v})
}

// InputTextLt retains the stored text operation; Set alone accepts I.
// Example: InputTextLt(members.Email, StoredEmail("ada@example.test")).
func InputTextLt[M, I any, V ~string](c InputText[M, I, V], v V) Predicate[M] {
	return comparison[M](c.inputTextStorage, querycore.Less, []V{v})
}

// InputTextLte retains the stored text operation; Set alone accepts I.
// Example: InputTextLte(members.Email, StoredEmail("ada@example.test")).
func InputTextLte[M, I any, V ~string](c InputText[M, I, V], v V) Predicate[M] {
	return comparison[M](c.inputTextStorage, querycore.LessOrEqual, []V{v})
}

// InputTextBetween retains the stored text operation; Set alone accepts I.
// Example: InputTextBetween(members.Email, StoredEmail("a"), StoredEmail("z")).
func InputTextBetween[M, I any, V ~string](c InputText[M, I, V], a, b V) Predicate[M] {
	return between[M](c.inputTextStorage, a, b)
}

// InputTextContains retains the stored text operation; Set alone accepts I.
// Example: InputTextContains(members.Email, StoredEmail("ada@example.test")).
func InputTextContains[M, I any, V ~string](c InputText[M, I, V], v V) Predicate[M] {
	return comparison[M](c.inputTextStorage, querycore.Contains, []V{v}, true)
}

// InputTextIContains retains the stored text operation; Set alone accepts I.
// Example: InputTextIContains(members.Email, StoredEmail("ada@example.test")).
func InputTextIContains[M, I any, V ~string](c InputText[M, I, V], v V) Predicate[M] {
	return comparison[M](c.inputTextStorage, querycore.InsensitiveContains, []V{v}, true)
}

// InputTextStartsWith retains the stored text operation; Set alone accepts I.
// Example: InputTextStartsWith(members.Email, StoredEmail("ada@example.test")).
func InputTextStartsWith[M, I any, V ~string](c InputText[M, I, V], v V) Predicate[M] {
	return comparison[M](c.inputTextStorage, querycore.StartsWith, []V{v}, true)
}

// InputTextEndsWith retains the stored text operation; Set alone accepts I.
// Example: InputTextEndsWith(members.Email, StoredEmail("ada@example.test")).
func InputTextEndsWith[M, I any, V ~string](c InputText[M, I, V], v V) Predicate[M] {
	return comparison[M](c.inputTextStorage, querycore.EndsWith, []V{v}, true)
}

// InputTextLike retains the stored text operation; Set alone accepts I.
// Example: InputTextLike(members.Email, StoredEmail("ada@example.test")).
func InputTextLike[M, I any, V ~string](c InputText[M, I, V], v V) Predicate[M] {
	return comparison[M](c.inputTextStorage, querycore.Like, []V{v}, true)
}

// InputTextILike retains the stored text operation; Set alone accepts I.
// Example: InputTextILike(members.Email, StoredEmail("ada@example.test")).
func InputTextILike[M, I any, V ~string](c InputText[M, I, V], v V) Predicate[M] {
	return comparison[M](c.inputTextStorage, querycore.InsensitiveLike, []V{v}, true)
}

// InputTextIStartsWith retains the stored text operation; Set alone accepts I.
// Example: InputTextIStartsWith(members.Email, StoredEmail("ada@example.test")).
func InputTextIStartsWith[M, I any, V ~string](c InputText[M, I, V], v V) Predicate[M] {
	return comparison[M](c.inputTextStorage, querycore.InsensitiveStartsWith, []V{v}, true)
}

// InputTextIEndsWith retains the stored text operation; Set alone accepts I.
// Example: InputTextIEndsWith(members.Email, StoredEmail("ada@example.test")).
func InputTextIEndsWith[M, I any, V ~string](c InputText[M, I, V], v V) Predicate[M] {
	return comparison[M](c.inputTextStorage, querycore.InsensitiveEndsWith, []V{v}, true)
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

// NullableInputTextEq retains the stored text operation; Set alone accepts I.
// Example: NullableInputTextEq(members.Note, StoredLabel("ada@example.test")).
func NullableInputTextEq[M, I any, V ~string](c NullableInputText[M, I, V], v V) Predicate[M] {
	return comparison[M](c.nullableInputTextStorage.base, querycore.Equal, []V{v})
}

// NullableInputTextNe retains the stored text operation; Set alone accepts I.
// Example: NullableInputTextNe(members.Note, StoredLabel("ada@example.test")).
func NullableInputTextNe[M, I any, V ~string](c NullableInputText[M, I, V], v V) Predicate[M] {
	return comparison[M](c.nullableInputTextStorage.base, querycore.NotEqual, []V{v})
}

// NullableInputTextIn retains the stored text operation; Set alone accepts I.
// Example: NullableInputTextIn(members.Note, StoredLabel("ada@example.test")).
func NullableInputTextIn[M, I any, V ~string](c NullableInputText[M, I, V], v ...V) Predicate[M] {
	return comparison[M](c.nullableInputTextStorage.base, querycore.In, v)
}

// NullableInputTextNotIn retains the stored text operation; Set alone accepts I.
// Example: NullableInputTextNotIn(members.Note, StoredLabel("ada@example.test")).
func NullableInputTextNotIn[M, I any, V ~string](c NullableInputText[M, I, V], v ...V) Predicate[M] {
	return comparison[M](c.nullableInputTextStorage.base, querycore.NotIn, v)
}

// NullableInputTextAsc retains the stored text operation; Set alone accepts I.
// Example: NullableInputTextAsc(members.Note).
func NullableInputTextAsc[M, I any, V ~string](c NullableInputText[M, I, V]) Order[M] {
	return columnOrder[M](c.nullableInputTextStorage.base, false)
}

// NullableInputTextDesc retains the stored text operation; Set alone accepts I.
// Example: NullableInputTextDesc(members.Note).
func NullableInputTextDesc[M, I any, V ~string](c NullableInputText[M, I, V]) Order[M] {
	return columnOrder[M](c.nullableInputTextStorage.base, true)
}

// NullableInputTextSet captures a typed input without invoking its transform.
// Example: NullableInputTextSet(members.Note, LabelInput{Text: "ada@example.test"}).
func NullableInputTextSet[M, I any, V ~string](c NullableInputText[M, I, V], v I) Assignment[M] {
	return inputAssignment[M](c.nullableInputTextStorage.base.core, c.input, v)
}

// NullableInputTextGt retains the stored text operation; Set alone accepts I.
// Example: NullableInputTextGt(members.Note, StoredLabel("ada@example.test")).
func NullableInputTextGt[M, I any, V ~string](c NullableInputText[M, I, V], v V) Predicate[M] {
	return comparison[M](c.nullableInputTextStorage.base, querycore.Greater, []V{v})
}

// NullableInputTextGte retains the stored text operation; Set alone accepts I.
// Example: NullableInputTextGte(members.Note, StoredLabel("ada@example.test")).
func NullableInputTextGte[M, I any, V ~string](c NullableInputText[M, I, V], v V) Predicate[M] {
	return comparison[M](c.nullableInputTextStorage.base, querycore.GreaterOrEqual, []V{v})
}

// NullableInputTextLt retains the stored text operation; Set alone accepts I.
// Example: NullableInputTextLt(members.Note, StoredLabel("ada@example.test")).
func NullableInputTextLt[M, I any, V ~string](c NullableInputText[M, I, V], v V) Predicate[M] {
	return comparison[M](c.nullableInputTextStorage.base, querycore.Less, []V{v})
}

// NullableInputTextLte retains the stored text operation; Set alone accepts I.
// Example: NullableInputTextLte(members.Note, StoredLabel("ada@example.test")).
func NullableInputTextLte[M, I any, V ~string](c NullableInputText[M, I, V], v V) Predicate[M] {
	return comparison[M](c.nullableInputTextStorage.base, querycore.LessOrEqual, []V{v})
}

// NullableInputTextBetween retains the stored text operation; Set alone accepts I.
// Example: NullableInputTextBetween(members.Note, StoredLabel("a"), StoredLabel("z")).
func NullableInputTextBetween[M, I any, V ~string](c NullableInputText[M, I, V], a, b V) Predicate[M] {
	return between[M](c.nullableInputTextStorage.base, a, b)
}

// NullableInputTextContains retains the stored text operation; Set alone accepts I.
// Example: NullableInputTextContains(members.Note, StoredLabel("ada@example.test")).
func NullableInputTextContains[M, I any, V ~string](c NullableInputText[M, I, V], v V) Predicate[M] {
	return comparison[M](c.nullableInputTextStorage.base, querycore.Contains, []V{v}, true)
}

// NullableInputTextIContains retains the stored text operation; Set alone accepts I.
// Example: NullableInputTextIContains(members.Note, StoredLabel("ada@example.test")).
func NullableInputTextIContains[M, I any, V ~string](c NullableInputText[M, I, V], v V) Predicate[M] {
	return comparison[M](c.nullableInputTextStorage.base, querycore.InsensitiveContains, []V{v}, true)
}

// NullableInputTextStartsWith retains the stored text operation; Set alone accepts I.
// Example: NullableInputTextStartsWith(members.Note, StoredLabel("ada@example.test")).
func NullableInputTextStartsWith[M, I any, V ~string](c NullableInputText[M, I, V], v V) Predicate[M] {
	return comparison[M](c.nullableInputTextStorage.base, querycore.StartsWith, []V{v}, true)
}

// NullableInputTextEndsWith retains the stored text operation; Set alone accepts I.
// Example: NullableInputTextEndsWith(members.Note, StoredLabel("ada@example.test")).
func NullableInputTextEndsWith[M, I any, V ~string](c NullableInputText[M, I, V], v V) Predicate[M] {
	return comparison[M](c.nullableInputTextStorage.base, querycore.EndsWith, []V{v}, true)
}

// NullableInputTextLike retains the stored text operation; Set alone accepts I.
// Example: NullableInputTextLike(members.Note, StoredLabel("ada@example.test")).
func NullableInputTextLike[M, I any, V ~string](c NullableInputText[M, I, V], v V) Predicate[M] {
	return comparison[M](c.nullableInputTextStorage.base, querycore.Like, []V{v}, true)
}

// NullableInputTextILike retains the stored text operation; Set alone accepts I.
// Example: NullableInputTextILike(members.Note, StoredLabel("ada@example.test")).
func NullableInputTextILike[M, I any, V ~string](c NullableInputText[M, I, V], v V) Predicate[M] {
	return comparison[M](c.nullableInputTextStorage.base, querycore.InsensitiveLike, []V{v}, true)
}

// NullableInputTextIStartsWith retains the stored text operation; Set alone accepts I.
// Example: NullableInputTextIStartsWith(members.Note, StoredLabel("ada@example.test")).
func NullableInputTextIStartsWith[M, I any, V ~string](c NullableInputText[M, I, V], v V) Predicate[M] {
	return comparison[M](c.nullableInputTextStorage.base, querycore.InsensitiveStartsWith, []V{v}, true)
}

// NullableInputTextIEndsWith retains the stored text operation; Set alone accepts I.
// Example: NullableInputTextIEndsWith(members.Note, StoredLabel("ada@example.test")).
func NullableInputTextIEndsWith[M, I any, V ~string](c NullableInputText[M, I, V], v V) Predicate[M] {
	return comparison[M](c.nullableInputTextStorage.base, querycore.InsensitiveEndsWith, []V{v}, true)
}

// NullableInputTextIsNull retains the stored text operation; Set alone accepts I.
// Example: NullableInputTextIsNull(members.Note).
func NullableInputTextIsNull[M, I any, V ~string](c NullableInputText[M, I, V]) Predicate[M] {
	return comparison[M](c.nullableInputTextStorage.base, querycore.IsNull, nil)
}

// NullableInputTextIsNotNull retains the stored text operation; Set alone accepts I.
// Example: NullableInputTextIsNotNull(members.Note).
func NullableInputTextIsNotNull[M, I any, V ~string](c NullableInputText[M, I, V]) Predicate[M] {
	return comparison[M](c.nullableInputTextStorage.base, querycore.IsNotNull, nil)
}

// NullableInputTextSetNull retains the stored text operation; Set alone accepts I.
// Example: NullableInputTextSetNull(members.Note).
func NullableInputTextSetNull[M, I any, V ~string](c NullableInputText[M, I, V]) Assignment[M] {
	return nullAssignment[M](c.nullableInputTextStorage.base)
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

// InputNumberEq retains the stored number operation; Set alone accepts I.
// Example: InputNumberEq(invoices.Amount, int64(100)).
func InputNumberEq[M, I any, V NumberValue](c InputNumber[M, I, V], v V) Predicate[M] {
	return comparison[M](c.inputNumberStorage, querycore.Equal, []V{v})
}

// InputNumberNe retains the stored number operation; Set alone accepts I.
// Example: InputNumberNe(invoices.Amount, int64(100)).
func InputNumberNe[M, I any, V NumberValue](c InputNumber[M, I, V], v V) Predicate[M] {
	return comparison[M](c.inputNumberStorage, querycore.NotEqual, []V{v})
}

// InputNumberIn retains the stored number operation; Set alone accepts I.
// Example: InputNumberIn(invoices.Amount, int64(100)).
func InputNumberIn[M, I any, V NumberValue](c InputNumber[M, I, V], v ...V) Predicate[M] {
	return comparison[M](c.inputNumberStorage, querycore.In, v)
}

// InputNumberNotIn retains the stored number operation; Set alone accepts I.
// Example: InputNumberNotIn(invoices.Amount, int64(100)).
func InputNumberNotIn[M, I any, V NumberValue](c InputNumber[M, I, V], v ...V) Predicate[M] {
	return comparison[M](c.inputNumberStorage, querycore.NotIn, v)
}

// InputNumberAsc retains the stored number operation; Set alone accepts I.
// Example: InputNumberAsc(invoices.Amount).
func InputNumberAsc[M, I any, V NumberValue](c InputNumber[M, I, V]) Order[M] {
	return columnOrder[M](c.inputNumberStorage, false)
}

// InputNumberDesc retains the stored number operation; Set alone accepts I.
// Example: InputNumberDesc(invoices.Amount).
func InputNumberDesc[M, I any, V NumberValue](c InputNumber[M, I, V]) Order[M] {
	return columnOrder[M](c.inputNumberStorage, true)
}

// InputNumberSet captures a typed input without invoking its transform.
// Example: InputNumberSet(invoices.Amount, AmountInput{Cents: 100}).
func InputNumberSet[M, I any, V NumberValue](c InputNumber[M, I, V], v I) Assignment[M] {
	return inputAssignment[M](c.inputNumberStorage.core, c.input, v)
}

// InputNumberGt retains the stored number operation; Set alone accepts I.
// Example: InputNumberGt(invoices.Amount, int64(100)).
func InputNumberGt[M, I any, V NumberValue](c InputNumber[M, I, V], v V) Predicate[M] {
	return comparison[M](c.inputNumberStorage, querycore.Greater, []V{v})
}

// InputNumberGte retains the stored number operation; Set alone accepts I.
// Example: InputNumberGte(invoices.Amount, int64(100)).
func InputNumberGte[M, I any, V NumberValue](c InputNumber[M, I, V], v V) Predicate[M] {
	return comparison[M](c.inputNumberStorage, querycore.GreaterOrEqual, []V{v})
}

// InputNumberLt retains the stored number operation; Set alone accepts I.
// Example: InputNumberLt(invoices.Amount, int64(100)).
func InputNumberLt[M, I any, V NumberValue](c InputNumber[M, I, V], v V) Predicate[M] {
	return comparison[M](c.inputNumberStorage, querycore.Less, []V{v})
}

// InputNumberLte retains the stored number operation; Set alone accepts I.
// Example: InputNumberLte(invoices.Amount, int64(100)).
func InputNumberLte[M, I any, V NumberValue](c InputNumber[M, I, V], v V) Predicate[M] {
	return comparison[M](c.inputNumberStorage, querycore.LessOrEqual, []V{v})
}

// InputNumberBetween retains the stored number operation; Set alone accepts I.
// Example: InputNumberBetween(invoices.Amount, int64(0), int64(1000)).
func InputNumberBetween[M, I any, V NumberValue](c InputNumber[M, I, V], a, b V) Predicate[M] {
	return between[M](c.inputNumberStorage, a, b)
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

// NullableInputNumberEq retains the stored number operation; Set alone accepts I.
// Example: NullableInputNumberEq(invoices.OptionalAmount, int64(100)).
func NullableInputNumberEq[M, I any, V NumberValue](c NullableInputNumber[M, I, V], v V) Predicate[M] {
	return comparison[M](c.nullableInputNumberStorage.base, querycore.Equal, []V{v})
}

// NullableInputNumberNe retains the stored number operation; Set alone accepts I.
// Example: NullableInputNumberNe(invoices.OptionalAmount, int64(100)).
func NullableInputNumberNe[M, I any, V NumberValue](c NullableInputNumber[M, I, V], v V) Predicate[M] {
	return comparison[M](c.nullableInputNumberStorage.base, querycore.NotEqual, []V{v})
}

// NullableInputNumberIn retains the stored number operation; Set alone accepts I.
// Example: NullableInputNumberIn(invoices.OptionalAmount, int64(100)).
func NullableInputNumberIn[M, I any, V NumberValue](c NullableInputNumber[M, I, V], v ...V) Predicate[M] {
	return comparison[M](c.nullableInputNumberStorage.base, querycore.In, v)
}

// NullableInputNumberNotIn retains the stored number operation; Set alone accepts I.
// Example: NullableInputNumberNotIn(invoices.OptionalAmount, int64(100)).
func NullableInputNumberNotIn[M, I any, V NumberValue](c NullableInputNumber[M, I, V], v ...V) Predicate[M] {
	return comparison[M](c.nullableInputNumberStorage.base, querycore.NotIn, v)
}

// NullableInputNumberAsc retains the stored number operation; Set alone accepts I.
// Example: NullableInputNumberAsc(invoices.OptionalAmount).
func NullableInputNumberAsc[M, I any, V NumberValue](c NullableInputNumber[M, I, V]) Order[M] {
	return columnOrder[M](c.nullableInputNumberStorage.base, false)
}

// NullableInputNumberDesc retains the stored number operation; Set alone accepts I.
// Example: NullableInputNumberDesc(invoices.OptionalAmount).
func NullableInputNumberDesc[M, I any, V NumberValue](c NullableInputNumber[M, I, V]) Order[M] {
	return columnOrder[M](c.nullableInputNumberStorage.base, true)
}

// NullableInputNumberSet captures a typed input without invoking its transform.
// Example: NullableInputNumberSet(invoices.OptionalAmount, AmountInput{Cents: 100}).
func NullableInputNumberSet[M, I any, V NumberValue](c NullableInputNumber[M, I, V], v I) Assignment[M] {
	return inputAssignment[M](c.nullableInputNumberStorage.base.core, c.input, v)
}

// NullableInputNumberGt retains the stored number operation; Set alone accepts I.
// Example: NullableInputNumberGt(invoices.OptionalAmount, int64(100)).
func NullableInputNumberGt[M, I any, V NumberValue](c NullableInputNumber[M, I, V], v V) Predicate[M] {
	return comparison[M](c.nullableInputNumberStorage.base, querycore.Greater, []V{v})
}

// NullableInputNumberGte retains the stored number operation; Set alone accepts I.
// Example: NullableInputNumberGte(invoices.OptionalAmount, int64(100)).
func NullableInputNumberGte[M, I any, V NumberValue](c NullableInputNumber[M, I, V], v V) Predicate[M] {
	return comparison[M](c.nullableInputNumberStorage.base, querycore.GreaterOrEqual, []V{v})
}

// NullableInputNumberLt retains the stored number operation; Set alone accepts I.
// Example: NullableInputNumberLt(invoices.OptionalAmount, int64(100)).
func NullableInputNumberLt[M, I any, V NumberValue](c NullableInputNumber[M, I, V], v V) Predicate[M] {
	return comparison[M](c.nullableInputNumberStorage.base, querycore.Less, []V{v})
}

// NullableInputNumberLte retains the stored number operation; Set alone accepts I.
// Example: NullableInputNumberLte(invoices.OptionalAmount, int64(100)).
func NullableInputNumberLte[M, I any, V NumberValue](c NullableInputNumber[M, I, V], v V) Predicate[M] {
	return comparison[M](c.nullableInputNumberStorage.base, querycore.LessOrEqual, []V{v})
}

// NullableInputNumberBetween retains the stored number operation; Set alone accepts I.
// Example: NullableInputNumberBetween(invoices.OptionalAmount, int64(0), int64(1000)).
func NullableInputNumberBetween[M, I any, V NumberValue](c NullableInputNumber[M, I, V], a, b V) Predicate[M] {
	return between[M](c.nullableInputNumberStorage.base, a, b)
}

// NullableInputNumberIsNull retains the stored number operation; Set alone accepts I.
// Example: NullableInputNumberIsNull(invoices.OptionalAmount).
func NullableInputNumberIsNull[M, I any, V NumberValue](c NullableInputNumber[M, I, V]) Predicate[M] {
	return comparison[M](c.nullableInputNumberStorage.base, querycore.IsNull, nil)
}

// NullableInputNumberIsNotNull retains the stored number operation; Set alone accepts I.
// Example: NullableInputNumberIsNotNull(invoices.OptionalAmount).
func NullableInputNumberIsNotNull[M, I any, V NumberValue](c NullableInputNumber[M, I, V]) Predicate[M] {
	return comparison[M](c.nullableInputNumberStorage.base, querycore.IsNotNull, nil)
}

// NullableInputNumberSetNull retains the stored number operation; Set alone accepts I.
// Example: NullableInputNumberSetNull(invoices.OptionalAmount).
func NullableInputNumberSetNull[M, I any, V NumberValue](c NullableInputNumber[M, I, V]) Assignment[M] {
	return nullAssignment[M](c.nullableInputNumberStorage.base)
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

// InputTimeEq retains the stored time operation; Set alone accepts I.
// Example: InputTimeEq(events.StartsAt, time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)).
func InputTimeEq[M, I any, V TimeValue](c InputTime[M, I, V], v V) Predicate[M] {
	return comparison[M](c.inputTimeStorage, querycore.Equal, []V{v})
}

// InputTimeNe retains the stored time operation; Set alone accepts I.
// Example: InputTimeNe(events.StartsAt, time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)).
func InputTimeNe[M, I any, V TimeValue](c InputTime[M, I, V], v V) Predicate[M] {
	return comparison[M](c.inputTimeStorage, querycore.NotEqual, []V{v})
}

// InputTimeIn retains the stored time operation; Set alone accepts I.
// Example: InputTimeIn(events.StartsAt, time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)).
func InputTimeIn[M, I any, V TimeValue](c InputTime[M, I, V], v ...V) Predicate[M] {
	return comparison[M](c.inputTimeStorage, querycore.In, v)
}

// InputTimeNotIn retains the stored time operation; Set alone accepts I.
// Example: InputTimeNotIn(events.StartsAt, time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)).
func InputTimeNotIn[M, I any, V TimeValue](c InputTime[M, I, V], v ...V) Predicate[M] {
	return comparison[M](c.inputTimeStorage, querycore.NotIn, v)
}

// InputTimeAsc retains the stored time operation; Set alone accepts I.
// Example: InputTimeAsc(events.StartsAt).
func InputTimeAsc[M, I any, V TimeValue](c InputTime[M, I, V]) Order[M] {
	return columnOrder[M](c.inputTimeStorage, false)
}

// InputTimeDesc retains the stored time operation; Set alone accepts I.
// Example: InputTimeDesc(events.StartsAt).
func InputTimeDesc[M, I any, V TimeValue](c InputTime[M, I, V]) Order[M] {
	return columnOrder[M](c.inputTimeStorage, true)
}

// InputTimeSet captures a typed input without invoking its transform.
// Example: InputTimeSet(events.StartsAt, TimestampInput{Time: startsAt}).
func InputTimeSet[M, I any, V TimeValue](c InputTime[M, I, V], v I) Assignment[M] {
	return inputAssignment[M](c.inputTimeStorage.core, c.input, v)
}

// InputTimeGt retains the stored time operation; Set alone accepts I.
// Example: InputTimeGt(events.StartsAt, time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)).
func InputTimeGt[M, I any, V TimeValue](c InputTime[M, I, V], v V) Predicate[M] {
	return comparison[M](c.inputTimeStorage, querycore.Greater, []V{v})
}

// InputTimeGte retains the stored time operation; Set alone accepts I.
// Example: InputTimeGte(events.StartsAt, time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)).
func InputTimeGte[M, I any, V TimeValue](c InputTime[M, I, V], v V) Predicate[M] {
	return comparison[M](c.inputTimeStorage, querycore.GreaterOrEqual, []V{v})
}

// InputTimeLt retains the stored time operation; Set alone accepts I.
// Example: InputTimeLt(events.StartsAt, time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)).
func InputTimeLt[M, I any, V TimeValue](c InputTime[M, I, V], v V) Predicate[M] {
	return comparison[M](c.inputTimeStorage, querycore.Less, []V{v})
}

// InputTimeLte retains the stored time operation; Set alone accepts I.
// Example: InputTimeLte(events.StartsAt, time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)).
func InputTimeLte[M, I any, V TimeValue](c InputTime[M, I, V], v V) Predicate[M] {
	return comparison[M](c.inputTimeStorage, querycore.LessOrEqual, []V{v})
}

// InputTimeBetween retains the stored time operation; Set alone accepts I.
// Example: InputTimeBetween(events.StartsAt, startsAt, endsAt).
func InputTimeBetween[M, I any, V TimeValue](c InputTime[M, I, V], a, b V) Predicate[M] {
	return between[M](c.inputTimeStorage, a, b)
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

// NullableInputTimeEq retains the stored time operation; Set alone accepts I.
// Example: NullableInputTimeEq(events.OptionalStartsAt, time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)).
func NullableInputTimeEq[M, I any, V TimeValue](c NullableInputTime[M, I, V], v V) Predicate[M] {
	return comparison[M](c.nullableInputTimeStorage.base, querycore.Equal, []V{v})
}

// NullableInputTimeNe retains the stored time operation; Set alone accepts I.
// Example: NullableInputTimeNe(events.OptionalStartsAt, time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)).
func NullableInputTimeNe[M, I any, V TimeValue](c NullableInputTime[M, I, V], v V) Predicate[M] {
	return comparison[M](c.nullableInputTimeStorage.base, querycore.NotEqual, []V{v})
}

// NullableInputTimeIn retains the stored time operation; Set alone accepts I.
// Example: NullableInputTimeIn(events.OptionalStartsAt, time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)).
func NullableInputTimeIn[M, I any, V TimeValue](c NullableInputTime[M, I, V], v ...V) Predicate[M] {
	return comparison[M](c.nullableInputTimeStorage.base, querycore.In, v)
}

// NullableInputTimeNotIn retains the stored time operation; Set alone accepts I.
// Example: NullableInputTimeNotIn(events.OptionalStartsAt, time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)).
func NullableInputTimeNotIn[M, I any, V TimeValue](c NullableInputTime[M, I, V], v ...V) Predicate[M] {
	return comparison[M](c.nullableInputTimeStorage.base, querycore.NotIn, v)
}

// NullableInputTimeAsc retains the stored time operation; Set alone accepts I.
// Example: NullableInputTimeAsc(events.OptionalStartsAt).
func NullableInputTimeAsc[M, I any, V TimeValue](c NullableInputTime[M, I, V]) Order[M] {
	return columnOrder[M](c.nullableInputTimeStorage.base, false)
}

// NullableInputTimeDesc retains the stored time operation; Set alone accepts I.
// Example: NullableInputTimeDesc(events.OptionalStartsAt).
func NullableInputTimeDesc[M, I any, V TimeValue](c NullableInputTime[M, I, V]) Order[M] {
	return columnOrder[M](c.nullableInputTimeStorage.base, true)
}

// NullableInputTimeSet captures a typed input without invoking its transform.
// Example: NullableInputTimeSet(events.OptionalStartsAt, TimestampInput{Time: startsAt}).
func NullableInputTimeSet[M, I any, V TimeValue](c NullableInputTime[M, I, V], v I) Assignment[M] {
	return inputAssignment[M](c.nullableInputTimeStorage.base.core, c.input, v)
}

// NullableInputTimeGt retains the stored time operation; Set alone accepts I.
// Example: NullableInputTimeGt(events.OptionalStartsAt, time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)).
func NullableInputTimeGt[M, I any, V TimeValue](c NullableInputTime[M, I, V], v V) Predicate[M] {
	return comparison[M](c.nullableInputTimeStorage.base, querycore.Greater, []V{v})
}

// NullableInputTimeGte retains the stored time operation; Set alone accepts I.
// Example: NullableInputTimeGte(events.OptionalStartsAt, time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)).
func NullableInputTimeGte[M, I any, V TimeValue](c NullableInputTime[M, I, V], v V) Predicate[M] {
	return comparison[M](c.nullableInputTimeStorage.base, querycore.GreaterOrEqual, []V{v})
}

// NullableInputTimeLt retains the stored time operation; Set alone accepts I.
// Example: NullableInputTimeLt(events.OptionalStartsAt, time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)).
func NullableInputTimeLt[M, I any, V TimeValue](c NullableInputTime[M, I, V], v V) Predicate[M] {
	return comparison[M](c.nullableInputTimeStorage.base, querycore.Less, []V{v})
}

// NullableInputTimeLte retains the stored time operation; Set alone accepts I.
// Example: NullableInputTimeLte(events.OptionalStartsAt, time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)).
func NullableInputTimeLte[M, I any, V TimeValue](c NullableInputTime[M, I, V], v V) Predicate[M] {
	return comparison[M](c.nullableInputTimeStorage.base, querycore.LessOrEqual, []V{v})
}

// NullableInputTimeBetween retains the stored time operation; Set alone accepts I.
// Example: NullableInputTimeBetween(events.OptionalStartsAt, startsAt, endsAt).
func NullableInputTimeBetween[M, I any, V TimeValue](c NullableInputTime[M, I, V], a, b V) Predicate[M] {
	return between[M](c.nullableInputTimeStorage.base, a, b)
}

// NullableInputTimeIsNull retains the stored time operation; Set alone accepts I.
// Example: NullableInputTimeIsNull(events.OptionalStartsAt).
func NullableInputTimeIsNull[M, I any, V TimeValue](c NullableInputTime[M, I, V]) Predicate[M] {
	return comparison[M](c.nullableInputTimeStorage.base, querycore.IsNull, nil)
}

// NullableInputTimeIsNotNull retains the stored time operation; Set alone accepts I.
// Example: NullableInputTimeIsNotNull(events.OptionalStartsAt).
func NullableInputTimeIsNotNull[M, I any, V TimeValue](c NullableInputTime[M, I, V]) Predicate[M] {
	return comparison[M](c.nullableInputTimeStorage.base, querycore.IsNotNull, nil)
}

// NullableInputTimeSetNull retains the stored time operation; Set alone accepts I.
// Example: NullableInputTimeSetNull(events.OptionalStartsAt).
func NullableInputTimeSetNull[M, I any, V TimeValue](c NullableInputTime[M, I, V]) Assignment[M] {
	return nullAssignment[M](c.nullableInputTimeStorage.base)
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

// InputEnumEq retains the stored enum operation; Set alone accepts I.
// Example: InputEnumEq(members.Status, StatusActive).
func InputEnumEq[M, I any, E interface {
	enum.Scalar
	EnumDescriptor() enum.Descriptor[E]
}](c InputEnum[M, I, E], v E) Predicate[M] {
	return comparison[M](c.inputEnumStorage, querycore.Equal, []E{v})
}

// InputEnumNe retains the stored enum operation; Set alone accepts I.
// Example: InputEnumNe(members.Status, StatusActive).
func InputEnumNe[M, I any, E interface {
	enum.Scalar
	EnumDescriptor() enum.Descriptor[E]
}](c InputEnum[M, I, E], v E) Predicate[M] {
	return comparison[M](c.inputEnumStorage, querycore.NotEqual, []E{v})
}

// InputEnumIn retains the stored enum operation; Set alone accepts I.
// Example: InputEnumIn(members.Status, StatusActive).
func InputEnumIn[M, I any, E interface {
	enum.Scalar
	EnumDescriptor() enum.Descriptor[E]
}](c InputEnum[M, I, E], v ...E) Predicate[M] {
	return comparison[M](c.inputEnumStorage, querycore.In, v)
}

// InputEnumNotIn retains the stored enum operation; Set alone accepts I.
// Example: InputEnumNotIn(members.Status, StatusActive).
func InputEnumNotIn[M, I any, E interface {
	enum.Scalar
	EnumDescriptor() enum.Descriptor[E]
}](c InputEnum[M, I, E], v ...E) Predicate[M] {
	return comparison[M](c.inputEnumStorage, querycore.NotIn, v)
}

// InputEnumAsc retains the stored enum operation; Set alone accepts I.
// Example: InputEnumAsc(members.Status).
func InputEnumAsc[M, I any, E interface {
	enum.Scalar
	EnumDescriptor() enum.Descriptor[E]
}](c InputEnum[M, I, E]) Order[M] {
	return columnOrder[M](c.inputEnumStorage, false)
}

// InputEnumDesc retains the stored enum operation; Set alone accepts I.
// Example: InputEnumDesc(members.Status).
func InputEnumDesc[M, I any, E interface {
	enum.Scalar
	EnumDescriptor() enum.Descriptor[E]
}](c InputEnum[M, I, E]) Order[M] {
	return columnOrder[M](c.inputEnumStorage, true)
}

// InputEnumSet captures a typed input without invoking its transform.
// Example: InputEnumSet(members.Status, StatusInput{Value: StatusActive}).
func InputEnumSet[M, I any, E interface {
	enum.Scalar
	EnumDescriptor() enum.Descriptor[E]
}](c InputEnum[M, I, E], v I) Assignment[M] {
	return inputAssignment[M](c.inputEnumStorage.core, c.input, v)
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

// NullableInputEnumEq retains the stored enum operation; Set alone accepts I.
// Example: NullableInputEnumEq(members.OptionalStatus, StatusActive).
func NullableInputEnumEq[M, I any, E interface {
	enum.Scalar
	EnumDescriptor() enum.Descriptor[E]
}](c NullableInputEnum[M, I, E], v E) Predicate[M] {
	return comparison[M](c.nullableInputEnumStorage.base, querycore.Equal, []E{v})
}

// NullableInputEnumNe retains the stored enum operation; Set alone accepts I.
// Example: NullableInputEnumNe(members.OptionalStatus, StatusActive).
func NullableInputEnumNe[M, I any, E interface {
	enum.Scalar
	EnumDescriptor() enum.Descriptor[E]
}](c NullableInputEnum[M, I, E], v E) Predicate[M] {
	return comparison[M](c.nullableInputEnumStorage.base, querycore.NotEqual, []E{v})
}

// NullableInputEnumIn retains the stored enum operation; Set alone accepts I.
// Example: NullableInputEnumIn(members.OptionalStatus, StatusActive).
func NullableInputEnumIn[M, I any, E interface {
	enum.Scalar
	EnumDescriptor() enum.Descriptor[E]
}](c NullableInputEnum[M, I, E], v ...E) Predicate[M] {
	return comparison[M](c.nullableInputEnumStorage.base, querycore.In, v)
}

// NullableInputEnumNotIn retains the stored enum operation; Set alone accepts I.
// Example: NullableInputEnumNotIn(members.OptionalStatus, StatusActive).
func NullableInputEnumNotIn[M, I any, E interface {
	enum.Scalar
	EnumDescriptor() enum.Descriptor[E]
}](c NullableInputEnum[M, I, E], v ...E) Predicate[M] {
	return comparison[M](c.nullableInputEnumStorage.base, querycore.NotIn, v)
}

// NullableInputEnumAsc retains the stored enum operation; Set alone accepts I.
// Example: NullableInputEnumAsc(members.OptionalStatus).
func NullableInputEnumAsc[M, I any, E interface {
	enum.Scalar
	EnumDescriptor() enum.Descriptor[E]
}](c NullableInputEnum[M, I, E]) Order[M] {
	return columnOrder[M](c.nullableInputEnumStorage.base, false)
}

// NullableInputEnumDesc retains the stored enum operation; Set alone accepts I.
// Example: NullableInputEnumDesc(members.OptionalStatus).
func NullableInputEnumDesc[M, I any, E interface {
	enum.Scalar
	EnumDescriptor() enum.Descriptor[E]
}](c NullableInputEnum[M, I, E]) Order[M] {
	return columnOrder[M](c.nullableInputEnumStorage.base, true)
}

// NullableInputEnumSet captures a typed input without invoking its transform.
// Example: NullableInputEnumSet(members.OptionalStatus, StatusInput{Value: StatusActive}).
func NullableInputEnumSet[M, I any, E interface {
	enum.Scalar
	EnumDescriptor() enum.Descriptor[E]
}](c NullableInputEnum[M, I, E], v I) Assignment[M] {
	return inputAssignment[M](c.nullableInputEnumStorage.base.core, c.input, v)
}

// NullableInputEnumIsNull retains the stored enum operation; Set alone accepts I.
// Example: NullableInputEnumIsNull(members.OptionalStatus).
func NullableInputEnumIsNull[M, I any, E interface {
	enum.Scalar
	EnumDescriptor() enum.Descriptor[E]
}](c NullableInputEnum[M, I, E]) Predicate[M] {
	return comparison[M](c.nullableInputEnumStorage.base, querycore.IsNull, nil)
}

// NullableInputEnumIsNotNull retains the stored enum operation; Set alone accepts I.
// Example: NullableInputEnumIsNotNull(members.OptionalStatus).
func NullableInputEnumIsNotNull[M, I any, E interface {
	enum.Scalar
	EnumDescriptor() enum.Descriptor[E]
}](c NullableInputEnum[M, I, E]) Predicate[M] {
	return comparison[M](c.nullableInputEnumStorage.base, querycore.IsNotNull, nil)
}

// NullableInputEnumSetNull retains the stored enum operation; Set alone accepts I.
// Example: NullableInputEnumSetNull(members.OptionalStatus).
func NullableInputEnumSetNull[M, I any, E interface {
	enum.Scalar
	EnumDescriptor() enum.Descriptor[E]
}](c NullableInputEnum[M, I, E]) Assignment[M] {
	return nullAssignment[M](c.nullableInputEnumStorage.base)
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

// InputJSONEq retains the stored json operation; Set alone accepts I.
// Example: InputJSONEq(members.Settings, storedPreferences).
func InputJSONEq[M, I any, T any](c InputJSON[M, I, T], v value.JSON[T]) Predicate[M] {
	return comparison[M](c.inputJSONStorage, querycore.Equal, []value.JSON[T]{v})
}

// InputJSONNe retains the stored json operation; Set alone accepts I.
// Example: InputJSONNe(members.Settings, storedPreferences).
func InputJSONNe[M, I any, T any](c InputJSON[M, I, T], v value.JSON[T]) Predicate[M] {
	return comparison[M](c.inputJSONStorage, querycore.NotEqual, []value.JSON[T]{v})
}

// InputJSONIn retains the stored json operation; Set alone accepts I.
// Example: InputJSONIn(members.Settings, storedPreferences).
func InputJSONIn[M, I any, T any](c InputJSON[M, I, T], v ...value.JSON[T]) Predicate[M] {
	return comparison[M](c.inputJSONStorage, querycore.In, v)
}

// InputJSONNotIn retains the stored json operation; Set alone accepts I.
// Example: InputJSONNotIn(members.Settings, storedPreferences).
func InputJSONNotIn[M, I any, T any](c InputJSON[M, I, T], v ...value.JSON[T]) Predicate[M] {
	return comparison[M](c.inputJSONStorage, querycore.NotIn, v)
}

// InputJSONAsc retains the stored json operation; Set alone accepts I.
// Example: InputJSONAsc(members.Settings).
func InputJSONAsc[M, I any, T any](c InputJSON[M, I, T]) Order[M] {
	return columnOrder[M](c.inputJSONStorage, false)
}

// InputJSONDesc retains the stored json operation; Set alone accepts I.
// Example: InputJSONDesc(members.Settings).
func InputJSONDesc[M, I any, T any](c InputJSON[M, I, T]) Order[M] {
	return columnOrder[M](c.inputJSONStorage, true)
}

// InputJSONContains retains the stored json operation; Set alone accepts I.
// Example: InputJSONContains(members.Settings, storedPreferences).
func InputJSONContains[M, I any, T any](c InputJSON[M, I, T], v value.JSON[T]) Predicate[M] {
	return jsonContainment[M](c.inputJSONStorage, querycore.JsonContainsOperation, v)
}

// InputJSONContainedBy retains the stored json operation; Set alone accepts I.
// Example: InputJSONContainedBy(members.Settings, storedPreferences).
func InputJSONContainedBy[M, I any, T any](c InputJSON[M, I, T], v value.JSON[T]) Predicate[M] {
	return jsonContainment[M](c.inputJSONStorage, querycore.JsonContainedByOperation, v)
}

// InputJSONIsJSONNull retains the stored json operation; Set alone accepts I.
// Example: InputJSONIsJSONNull(members.Settings).
func InputJSONIsJSONNull[M, I any, T any](c InputJSON[M, I, T]) Predicate[M] {
	return jsonNull[M](c.inputJSONStorage)
}

// InputJSONSet captures a typed input without invoking its transform.
// Example: InputJSONSet(members.Settings, PreferencesInput{Theme: "dark"}).
func InputJSONSet[M, I any, T any](c InputJSON[M, I, T], v I) Assignment[M] {
	return inputAssignment[M](c.inputJSONStorage.core, c.input, v)
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

// NullableInputJSONEq retains the stored json operation; Set alone accepts I.
// Example: NullableInputJSONEq(members.BackupSettings, storedPreferences).
func NullableInputJSONEq[M, I any, T any](c NullableInputJSON[M, I, T], v value.JSON[T]) Predicate[M] {
	return comparison[M](c.nullableInputJSONStorage.base, querycore.Equal, []value.JSON[T]{v})
}

// NullableInputJSONNe retains the stored json operation; Set alone accepts I.
// Example: NullableInputJSONNe(members.BackupSettings, storedPreferences).
func NullableInputJSONNe[M, I any, T any](c NullableInputJSON[M, I, T], v value.JSON[T]) Predicate[M] {
	return comparison[M](c.nullableInputJSONStorage.base, querycore.NotEqual, []value.JSON[T]{v})
}

// NullableInputJSONIn retains the stored json operation; Set alone accepts I.
// Example: NullableInputJSONIn(members.BackupSettings, storedPreferences).
func NullableInputJSONIn[M, I any, T any](c NullableInputJSON[M, I, T], v ...value.JSON[T]) Predicate[M] {
	return comparison[M](c.nullableInputJSONStorage.base, querycore.In, v)
}

// NullableInputJSONNotIn retains the stored json operation; Set alone accepts I.
// Example: NullableInputJSONNotIn(members.BackupSettings, storedPreferences).
func NullableInputJSONNotIn[M, I any, T any](c NullableInputJSON[M, I, T], v ...value.JSON[T]) Predicate[M] {
	return comparison[M](c.nullableInputJSONStorage.base, querycore.NotIn, v)
}

// NullableInputJSONAsc retains the stored json operation; Set alone accepts I.
// Example: NullableInputJSONAsc(members.BackupSettings).
func NullableInputJSONAsc[M, I any, T any](c NullableInputJSON[M, I, T]) Order[M] {
	return columnOrder[M](c.nullableInputJSONStorage.base, false)
}

// NullableInputJSONDesc retains the stored json operation; Set alone accepts I.
// Example: NullableInputJSONDesc(members.BackupSettings).
func NullableInputJSONDesc[M, I any, T any](c NullableInputJSON[M, I, T]) Order[M] {
	return columnOrder[M](c.nullableInputJSONStorage.base, true)
}

// NullableInputJSONContains retains the stored json operation; Set alone accepts I.
// Example: NullableInputJSONContains(members.BackupSettings, storedPreferences).
func NullableInputJSONContains[M, I any, T any](c NullableInputJSON[M, I, T], v value.JSON[T]) Predicate[M] {
	return jsonContainment[M](c.nullableInputJSONStorage.base, querycore.JsonContainsOperation, v)
}

// NullableInputJSONContainedBy retains the stored json operation; Set alone accepts I.
// Example: NullableInputJSONContainedBy(members.BackupSettings, storedPreferences).
func NullableInputJSONContainedBy[M, I any, T any](c NullableInputJSON[M, I, T], v value.JSON[T]) Predicate[M] {
	return jsonContainment[M](c.nullableInputJSONStorage.base, querycore.JsonContainedByOperation, v)
}

// NullableInputJSONIsJSONNull retains the stored json operation; Set alone accepts I.
// Example: NullableInputJSONIsJSONNull(members.BackupSettings).
func NullableInputJSONIsJSONNull[M, I any, T any](c NullableInputJSON[M, I, T]) Predicate[M] {
	return jsonNull[M](c.nullableInputJSONStorage.base)
}

// NullableInputJSONIsNull retains the stored json operation; Set alone accepts I.
// Example: NullableInputJSONIsNull(members.BackupSettings).
func NullableInputJSONIsNull[M, I any, T any](c NullableInputJSON[M, I, T]) Predicate[M] {
	return comparison[M](c.nullableInputJSONStorage.base, querycore.IsNull, nil)
}

// NullableInputJSONIsNotNull retains the stored json operation; Set alone accepts I.
// Example: NullableInputJSONIsNotNull(members.BackupSettings).
func NullableInputJSONIsNotNull[M, I any, T any](c NullableInputJSON[M, I, T]) Predicate[M] {
	return comparison[M](c.nullableInputJSONStorage.base, querycore.IsNotNull, nil)
}

// NullableInputJSONSet captures a typed input without invoking its transform.
// Example: NullableInputJSONSet(members.BackupSettings, PreferencesInput{Theme: "dark"}).
func NullableInputJSONSet[M, I any, T any](c NullableInputJSON[M, I, T], v I) Assignment[M] {
	return inputAssignment[M](c.nullableInputJSONStorage.base.core, c.input, v)
}

// NullableInputJSONSetNull retains the stored json operation; Set alone accepts I.
// Example: NullableInputJSONSetNull(members.BackupSettings).
func NullableInputJSONSetNull[M, I any, T any](c NullableInputJSON[M, I, T]) Assignment[M] {
	return nullAssignment[M](c.nullableInputJSONStorage.base)
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

// InputIntervalEq retains the stored interval operation; Set alone accepts I.
// Example: InputIntervalEq(bookings.Period, temporal.Days(1)).
func InputIntervalEq[M, I any](c InputInterval[M, I], v temporal.Interval) Predicate[M] {
	return comparison[M](c.inputIntervalStorage, querycore.Equal, []temporal.Interval{v})
}

// InputIntervalNe retains the stored interval operation; Set alone accepts I.
// Example: InputIntervalNe(bookings.Period, temporal.Days(1)).
func InputIntervalNe[M, I any](c InputInterval[M, I], v temporal.Interval) Predicate[M] {
	return comparison[M](c.inputIntervalStorage, querycore.NotEqual, []temporal.Interval{v})
}

// InputIntervalIn retains the stored interval operation; Set alone accepts I.
// Example: InputIntervalIn(bookings.Period, temporal.Days(1)).
func InputIntervalIn[M, I any](c InputInterval[M, I], v ...temporal.Interval) Predicate[M] {
	return comparison[M](c.inputIntervalStorage, querycore.In, v)
}

// InputIntervalNotIn retains the stored interval operation; Set alone accepts I.
// Example: InputIntervalNotIn(bookings.Period, temporal.Days(1)).
func InputIntervalNotIn[M, I any](c InputInterval[M, I], v ...temporal.Interval) Predicate[M] {
	return comparison[M](c.inputIntervalStorage, querycore.NotIn, v)
}

// InputIntervalAsc retains the stored interval operation; Set alone accepts I.
// Example: InputIntervalAsc(bookings.Period).
func InputIntervalAsc[M, I any](c InputInterval[M, I]) Order[M] {
	return columnOrder[M](c.inputIntervalStorage, false)
}

// InputIntervalDesc retains the stored interval operation; Set alone accepts I.
// Example: InputIntervalDesc(bookings.Period).
func InputIntervalDesc[M, I any](c InputInterval[M, I]) Order[M] {
	return columnOrder[M](c.inputIntervalStorage, true)
}

// InputIntervalSet captures a typed input without invoking its transform.
// Example: InputIntervalSet(bookings.Period, PeriodInput{Days: 1}).
func InputIntervalSet[M, I any](c InputInterval[M, I], v I) Assignment[M] {
	return inputAssignment[M](c.inputIntervalStorage.core, c.input, v)
}

// InputIntervalGt retains the stored interval operation; Set alone accepts I.
// Example: InputIntervalGt(bookings.Period, temporal.Days(1)).
func InputIntervalGt[M, I any](c InputInterval[M, I], v temporal.Interval) Predicate[M] {
	return comparison[M](c.inputIntervalStorage, querycore.Greater, []temporal.Interval{v})
}

// InputIntervalGte retains the stored interval operation; Set alone accepts I.
// Example: InputIntervalGte(bookings.Period, temporal.Days(1)).
func InputIntervalGte[M, I any](c InputInterval[M, I], v temporal.Interval) Predicate[M] {
	return comparison[M](c.inputIntervalStorage, querycore.GreaterOrEqual, []temporal.Interval{v})
}

// InputIntervalLt retains the stored interval operation; Set alone accepts I.
// Example: InputIntervalLt(bookings.Period, temporal.Days(1)).
func InputIntervalLt[M, I any](c InputInterval[M, I], v temporal.Interval) Predicate[M] {
	return comparison[M](c.inputIntervalStorage, querycore.Less, []temporal.Interval{v})
}

// InputIntervalLte retains the stored interval operation; Set alone accepts I.
// Example: InputIntervalLte(bookings.Period, temporal.Days(1)).
func InputIntervalLte[M, I any](c InputInterval[M, I], v temporal.Interval) Predicate[M] {
	return comparison[M](c.inputIntervalStorage, querycore.LessOrEqual, []temporal.Interval{v})
}

// InputIntervalBetween retains the stored interval operation; Set alone accepts I.
// Example: InputIntervalBetween(bookings.Period, temporal.Days(1), temporal.Days(7)).
func InputIntervalBetween[M, I any](c InputInterval[M, I], a, b temporal.Interval) Predicate[M] {
	return between[M](c.inputIntervalStorage, a, b)
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

// NullableInputIntervalEq retains the stored interval operation; Set alone accepts I.
// Example: NullableInputIntervalEq(bookings.OptionalPeriod, temporal.Days(1)).
func NullableInputIntervalEq[M, I any](c NullableInputInterval[M, I], v temporal.Interval) Predicate[M] {
	return comparison[M](c.nullableInputIntervalStorage.base, querycore.Equal, []temporal.Interval{v})
}

// NullableInputIntervalNe retains the stored interval operation; Set alone accepts I.
// Example: NullableInputIntervalNe(bookings.OptionalPeriod, temporal.Days(1)).
func NullableInputIntervalNe[M, I any](c NullableInputInterval[M, I], v temporal.Interval) Predicate[M] {
	return comparison[M](c.nullableInputIntervalStorage.base, querycore.NotEqual, []temporal.Interval{v})
}

// NullableInputIntervalIn retains the stored interval operation; Set alone accepts I.
// Example: NullableInputIntervalIn(bookings.OptionalPeriod, temporal.Days(1)).
func NullableInputIntervalIn[M, I any](c NullableInputInterval[M, I], v ...temporal.Interval) Predicate[M] {
	return comparison[M](c.nullableInputIntervalStorage.base, querycore.In, v)
}

// NullableInputIntervalNotIn retains the stored interval operation; Set alone accepts I.
// Example: NullableInputIntervalNotIn(bookings.OptionalPeriod, temporal.Days(1)).
func NullableInputIntervalNotIn[M, I any](c NullableInputInterval[M, I], v ...temporal.Interval) Predicate[M] {
	return comparison[M](c.nullableInputIntervalStorage.base, querycore.NotIn, v)
}

// NullableInputIntervalAsc retains the stored interval operation; Set alone accepts I.
// Example: NullableInputIntervalAsc(bookings.OptionalPeriod).
func NullableInputIntervalAsc[M, I any](c NullableInputInterval[M, I]) Order[M] {
	return columnOrder[M](c.nullableInputIntervalStorage.base, false)
}

// NullableInputIntervalDesc retains the stored interval operation; Set alone accepts I.
// Example: NullableInputIntervalDesc(bookings.OptionalPeriod).
func NullableInputIntervalDesc[M, I any](c NullableInputInterval[M, I]) Order[M] {
	return columnOrder[M](c.nullableInputIntervalStorage.base, true)
}

// NullableInputIntervalSet captures a typed input without invoking its transform.
// Example: NullableInputIntervalSet(bookings.OptionalPeriod, PeriodInput{Days: 1}).
func NullableInputIntervalSet[M, I any](c NullableInputInterval[M, I], v I) Assignment[M] {
	return inputAssignment[M](c.nullableInputIntervalStorage.base.core, c.input, v)
}

// NullableInputIntervalGt retains the stored interval operation; Set alone accepts I.
// Example: NullableInputIntervalGt(bookings.OptionalPeriod, temporal.Days(1)).
func NullableInputIntervalGt[M, I any](c NullableInputInterval[M, I], v temporal.Interval) Predicate[M] {
	return comparison[M](c.nullableInputIntervalStorage.base, querycore.Greater, []temporal.Interval{v})
}

// NullableInputIntervalGte retains the stored interval operation; Set alone accepts I.
// Example: NullableInputIntervalGte(bookings.OptionalPeriod, temporal.Days(1)).
func NullableInputIntervalGte[M, I any](c NullableInputInterval[M, I], v temporal.Interval) Predicate[M] {
	return comparison[M](c.nullableInputIntervalStorage.base, querycore.GreaterOrEqual, []temporal.Interval{v})
}

// NullableInputIntervalLt retains the stored interval operation; Set alone accepts I.
// Example: NullableInputIntervalLt(bookings.OptionalPeriod, temporal.Days(1)).
func NullableInputIntervalLt[M, I any](c NullableInputInterval[M, I], v temporal.Interval) Predicate[M] {
	return comparison[M](c.nullableInputIntervalStorage.base, querycore.Less, []temporal.Interval{v})
}

// NullableInputIntervalLte retains the stored interval operation; Set alone accepts I.
// Example: NullableInputIntervalLte(bookings.OptionalPeriod, temporal.Days(1)).
func NullableInputIntervalLte[M, I any](c NullableInputInterval[M, I], v temporal.Interval) Predicate[M] {
	return comparison[M](c.nullableInputIntervalStorage.base, querycore.LessOrEqual, []temporal.Interval{v})
}

// NullableInputIntervalBetween retains the stored interval operation; Set alone accepts I.
// Example: NullableInputIntervalBetween(bookings.OptionalPeriod, temporal.Days(1), temporal.Days(7)).
func NullableInputIntervalBetween[M, I any](c NullableInputInterval[M, I], a, b temporal.Interval) Predicate[M] {
	return between[M](c.nullableInputIntervalStorage.base, a, b)
}

// NullableInputIntervalIsNull retains the stored interval operation; Set alone accepts I.
// Example: NullableInputIntervalIsNull(bookings.OptionalPeriod).
func NullableInputIntervalIsNull[M, I any](c NullableInputInterval[M, I]) Predicate[M] {
	return comparison[M](c.nullableInputIntervalStorage.base, querycore.IsNull, nil)
}

// NullableInputIntervalIsNotNull retains the stored interval operation; Set alone accepts I.
// Example: NullableInputIntervalIsNotNull(bookings.OptionalPeriod).
func NullableInputIntervalIsNotNull[M, I any](c NullableInputInterval[M, I]) Predicate[M] {
	return comparison[M](c.nullableInputIntervalStorage.base, querycore.IsNotNull, nil)
}

// NullableInputIntervalSetNull retains the stored interval operation; Set alone accepts I.
// Example: NullableInputIntervalSetNull(bookings.OptionalPeriod).
func NullableInputIntervalSetNull[M, I any](c NullableInputInterval[M, I]) Assignment[M] {
	return nullAssignment[M](c.nullableInputIntervalStorage.base)
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
