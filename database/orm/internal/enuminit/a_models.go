// Package enuminit deliberately declares models before generated enum variables.
package enuminit

import (
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/value"
)

//foundry:enum
type Status string

const Active Status = "active"

type Model struct {
	Code     Status
	State    Status
	Nullable value.Nullable[Status]
}
type Fields struct {
	orm.Table[Model, Status]
	State    orm.Enum[Model, Status]
	Nullable orm.NullableEnum[Model, Status]
}

var Models = orm.Define("enum_init", func(b orm.Builder[Model], m *Model) Fields {
	return Fields{orm.TableOf(b, &m.Code), orm.EnumOf(b, &m.State), orm.NullableEnumOf(b, &m.Nullable)}
})

type DTO struct {
	State    Status
	Nullable value.Nullable[Status]
}

type RowFields struct {
	rows.Row[DTO]
	State    rows.Field[DTO, Status]
	Nullable rows.Field[DTO, value.Nullable[Status]]
}

var Rows = rows.Define(func(b rows.Builder[DTO], r *DTO) RowFields {
	return RowFields{b.Row(), rows.EnumOf(b, &r.State), rows.NullableEnumOf(b, &r.Nullable)}
})

type Side struct{}

var Alias = rows.As[Side](Models.Query(), "e")
var State = expr.At(rows.Scope(Alias), Models.State)
var Projection = rows.Select(Alias, Rows, rows.Map(Rows.State, State), rows.Map(Rows.Nullable, expr.At(rows.Scope(Alias), Models.Nullable)))
var Values = rows.SelectValue(Models.Query(), Models.State)
