// Package coremodels exercises production column declarations without changing the Stage 1 golden schema.
package coremodels

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/auth/challenge"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/outbox"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

type Code string
type Event struct{}
type Preferences struct {
	Theme string `json:"theme"`
}
type Payload []byte

type Record struct {
	Code          Code
	Settings      value.JSON[Preferences]
	MaybeSettings value.Nullable[value.JSON[Preferences]]
	Period        temporal.Interval
	MaybePeriod   value.Nullable[temporal.Interval]
	Body          Payload
	MaybeBody     value.Nullable[Payload]
	ForeignID     model.ID[models.User]
	Revision      challenge.Revision[Record]
	MessageID     outbox.ID[Event]
	Status        models.Status
}

type Fields struct {
	orm.Table[Record, Code]
	Code          orm.Key[Record, Code]
	Settings      orm.JSON[Record, Preferences]
	MaybeSettings orm.NullableJSON[Record, Preferences]
	Period        orm.Interval[Record]
	MaybePeriod   orm.NullableInterval[Record]
	Body          orm.Column[Record, Payload]
	MaybeBody     orm.NullableColumn[Record, Payload]
	ForeignID     orm.Column[Record, model.ID[models.User]]
	Revision      orm.Column[Record, challenge.Revision[Record]]
	MessageID     orm.Column[Record, outbox.ID[Event]]
	Status        orm.Enum[Record, models.Status]
}

var Records = orm.Define("core_records", func(b orm.Builder[Record], m *Record) Fields {
	table := orm.TableOf(b, &m.Code, orm.Codec(codec.String[Code]()))
	return Fields{Table: table, Code: table.Key(), Settings: orm.JSONOf(b, &m.Settings), MaybeSettings: orm.NullableJSONOf(b, &m.MaybeSettings), Period: orm.IntervalOf(b, &m.Period), MaybePeriod: orm.NullableIntervalOf(b, &m.MaybePeriod), Body: orm.BytesOf(b, &m.Body), MaybeBody: orm.NullableBytesOf(b, &m.MaybeBody), ForeignID: orm.IDOf(b, &m.ForeignID), Revision: orm.IDOf(b, &m.Revision), MessageID: orm.IDOf(b, &m.MessageID), Status: orm.EnumOf(b, &m.Status)}
})

type Duration struct {
	Period temporal.Interval
	Label  string
}
type DurationFields struct {
	orm.Table[Duration, temporal.Interval]
	Period orm.Key[Duration, temporal.Interval]
	Label  orm.Text[Duration, string]
}

var Durations = orm.Define("core_durations", func(b orm.Builder[Duration], m *Duration) DurationFields {
	table := orm.TableOf(b, &m.Period, orm.Codec(codec.Interval()))
	return DurationFields{table, table.Key(), orm.TextOf(b, &m.Label)}
})
