package input_column_kinds

import (
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/enum"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
	"time"
)

type Status string

func (Status) EnumDescriptor() enum.Descriptor[Status] {
	return enum.Describe("footprint", "Status", enum.Case[Status]{Name: "Active", Value: "active"})
}

type Input struct{ Value string }
type Model struct {
	ID            int64
	Column        bool
	MaybeColumn   value.Nullable[bool]
	Text          string
	MaybeText     value.Nullable[string]
	Number        int64
	MaybeNumber   value.Nullable[int64]
	Time          time.Time
	MaybeTime     value.Nullable[time.Time]
	Enum          Status
	MaybeEnum     value.Nullable[Status]
	JSON          value.JSON[string]
	MaybeJSON     value.Nullable[value.JSON[string]]
	Interval      temporal.Interval
	MaybeInterval value.Nullable[temporal.Interval]
	Bytes         []byte
	MaybeBytes    value.Nullable[[]byte]
}
type Fields struct {
	orm.Table[Model, int64]
	Column        orm.InputColumn[Model, Input, bool]
	MaybeColumn   orm.NullableInputColumn[Model, Input, bool]
	Text          orm.InputText[Model, Input, string]
	MaybeText     orm.NullableInputText[Model, Input, string]
	Number        orm.InputNumber[Model, Input, int64]
	MaybeNumber   orm.NullableInputNumber[Model, Input, int64]
	Time          orm.InputTime[Model, Input, time.Time]
	MaybeTime     orm.NullableInputTime[Model, Input, time.Time]
	Enum          orm.InputEnum[Model, Input, Status]
	MaybeEnum     orm.NullableInputEnum[Model, Input, Status]
	JSON          orm.InputJSON[Model, Input, string]
	MaybeJSON     orm.NullableInputJSON[Model, Input, string]
	Interval      orm.InputInterval[Model, Input]
	MaybeInterval orm.NullableInputInterval[Model, Input]
	Bytes         orm.InputColumn[Model, Input, []byte]
	MaybeBytes    orm.NullableInputColumn[Model, Input, []byte]
}

func transform[V any](Input) (v V, err error) { return }

var Models = orm.Define("input_kinds", func(b orm.Builder[Model], m *Model) Fields {
	return Fields{Table: orm.TableOf(b, &m.ID),
		Column:        orm.InputColumnOf(b, &m.Column, transform[bool]),
		MaybeColumn:   orm.NullableInputColumnOf(b, &m.MaybeColumn, transform[bool]),
		Text:          orm.InputTextOf(b, &m.Text, transform[string]),
		MaybeText:     orm.NullableInputTextOf(b, &m.MaybeText, transform[string]),
		Number:        orm.InputNumberOf(b, &m.Number, transform[int64]),
		MaybeNumber:   orm.NullableInputNumberOf(b, &m.MaybeNumber, transform[int64]),
		Time:          orm.InputTimeOf(b, &m.Time, transform[time.Time]),
		MaybeTime:     orm.NullableInputTimeOf(b, &m.MaybeTime, transform[time.Time]),
		Enum:          orm.InputEnumOf(b, &m.Enum, transform[Status]),
		MaybeEnum:     orm.NullableInputEnumOf(b, &m.MaybeEnum, transform[Status]),
		JSON:          orm.InputJSONOf(b, &m.JSON, transform[value.JSON[string]]),
		MaybeJSON:     orm.NullableInputJSONOf(b, &m.MaybeJSON, transform[value.JSON[string]]),
		Interval:      orm.InputIntervalOf(b, &m.Interval, transform[temporal.Interval]),
		MaybeInterval: orm.NullableInputIntervalOf(b, &m.MaybeInterval, transform[temporal.Interval]),
		Bytes:         orm.InputBytesOf(b, &m.Bytes, transform[[]byte]),
		MaybeBytes:    orm.NullableInputBytesOf(b, &m.MaybeBytes, transform[[]byte]),
	}
})
