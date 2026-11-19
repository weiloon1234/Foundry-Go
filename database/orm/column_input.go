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

// InputColumnAsc retains the stored column operation; Set alone accepts I.
// Example: InputColumnAsc(members.Email).
func InputColumnAsc[M, I any, V any](c InputColumn[M, I, V]) Order[M] {
	return columnOrder[M](c.inputColumnStorage, false)
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

// InputTextAsc retains the stored text operation; Set alone accepts I.
// Example: InputTextAsc(members.Email).
func InputTextAsc[M, I any, V ~string](c InputText[M, I, V]) Order[M] {
	return columnOrder[M](c.inputTextStorage, false)
}

// InputTextSet captures a typed input without invoking its transform.
// Example: InputTextSet(members.Email, EmailInput{Address: "ada@example.test"}).
func InputTextSet[M, I any, V ~string](c InputText[M, I, V], v I) Assignment[M] {
	return inputAssignment[M](c.inputTextStorage.core, c.input, v)
}

// InputTextContains retains the stored text operation; Set alone accepts I.
// Example: InputTextContains(members.Email, StoredEmail("ada@example.test")).
func InputTextContains[M, I any, V ~string](c InputText[M, I, V], v V) Predicate[M] {
	return comparison[M](c.inputTextStorage, querycore.Contains, []V{v}, true)
}

// InputTextLike retains the stored text operation; Set alone accepts I.
// Example: InputTextLike(members.Email, StoredEmail("ada@example.test")).
func InputTextLike[M, I any, V ~string](c InputText[M, I, V], v V) Predicate[M] {
	return comparison[M](c.inputTextStorage, querycore.Like, []V{v}, true)
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

// NullableInputTextSet captures a typed input without invoking its transform.
// Example: NullableInputTextSet(members.Note, LabelInput{Text: "ada@example.test"}).
func NullableInputTextSet[M, I any, V ~string](c NullableInputText[M, I, V], v I) Assignment[M] {
	return inputAssignment[M](c.nullableInputTextStorage.base.core, c.input, v)
}

// NullableInputTextContains retains the stored text operation; Set alone accepts I.
// Example: NullableInputTextContains(members.Note, StoredLabel("ada@example.test")).
func NullableInputTextContains[M, I any, V ~string](c NullableInputText[M, I, V], v V) Predicate[M] {
	return comparison[M](c.nullableInputTextStorage.base, querycore.Contains, []V{v}, true)
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

// InputNumberAsc retains the stored number operation; Set alone accepts I.
// Example: InputNumberAsc(invoices.Amount).
func InputNumberAsc[M, I any, V NumberValue](c InputNumber[M, I, V]) Order[M] {
	return columnOrder[M](c.inputNumberStorage, false)
}

// InputNumberSet captures a typed input without invoking its transform.
// Example: InputNumberSet(invoices.Amount, AmountInput{Cents: 100}).
func InputNumberSet[M, I any, V NumberValue](c InputNumber[M, I, V], v I) Assignment[M] {
	return inputAssignment[M](c.inputNumberStorage.core, c.input, v)
}

// InputNumberGte retains the stored number operation; Set alone accepts I.
// Example: InputNumberGte(invoices.Amount, int64(100)).
func InputNumberGte[M, I any, V NumberValue](c InputNumber[M, I, V], v V) Predicate[M] {
	return comparison[M](c.inputNumberStorage, querycore.GreaterOrEqual, []V{v})
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

// NullableInputNumberSet captures a typed input without invoking its transform.
// Example: NullableInputNumberSet(invoices.OptionalAmount, AmountInput{Cents: 100}).
func NullableInputNumberSet[M, I any, V NumberValue](c NullableInputNumber[M, I, V], v I) Assignment[M] {
	return inputAssignment[M](c.nullableInputNumberStorage.base.core, c.input, v)
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

// InputTimeSet captures a typed input without invoking its transform.
// Example: InputTimeSet(events.StartsAt, TimestampInput{Time: startsAt}).
func InputTimeSet[M, I any, V TimeValue](c InputTime[M, I, V], v I) Assignment[M] {
	return inputAssignment[M](c.inputTimeStorage.core, c.input, v)
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

// NullableInputTimeSet captures a typed input without invoking its transform.
// Example: NullableInputTimeSet(events.OptionalStartsAt, TimestampInput{Time: startsAt}).
func NullableInputTimeSet[M, I any, V TimeValue](c NullableInputTime[M, I, V], v I) Assignment[M] {
	return inputAssignment[M](c.nullableInputTimeStorage.base.core, c.input, v)
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

// NullableInputEnumSet captures a typed input without invoking its transform.
// Example: NullableInputEnumSet(members.OptionalStatus, StatusInput{Value: StatusActive}).
func NullableInputEnumSet[M, I any, E interface {
	enum.Scalar
	EnumDescriptor() enum.Descriptor[E]
}](c NullableInputEnum[M, I, E], v I) Assignment[M] {
	return inputAssignment[M](c.nullableInputEnumStorage.base.core, c.input, v)
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

// InputIntervalSet captures a typed input without invoking its transform.
// Example: InputIntervalSet(bookings.Period, PeriodInput{Days: 1}).
func InputIntervalSet[M, I any](c InputInterval[M, I], v I) Assignment[M] {
	return inputAssignment[M](c.inputIntervalStorage.core, c.input, v)
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

// NullableInputIntervalSet captures a typed input without invoking its transform.
// Example: NullableInputIntervalSet(bookings.OptionalPeriod, PeriodInput{Days: 1}).
func NullableInputIntervalSet[M, I any](c NullableInputInterval[M, I], v I) Assignment[M] {
	return inputAssignment[M](c.nullableInputIntervalStorage.base.core, c.input, v)
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
