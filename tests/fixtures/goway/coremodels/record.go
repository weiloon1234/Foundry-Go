// Package coremodels exercises production column declarations without changing the Stage 1 golden schema.
package coremodels

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/auth/challenge"
	"github.com/weiloon1234/Foundry-Go/database/codec"

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

//foundry:model table=core_records var=Records key=Code identity=false
type Record struct {
	Code          Code `foundry:"codec=codeCodec,member=Code,key-role=lookup"`
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

//foundry:model table=core_durations var=Durations key=Period identity=false
type Duration struct {
	Period temporal.Interval `foundry:"codec=intervalCodec,member=Period"`
	Label  string
}

func codeCodec() codec.Codec[Code]                  { return codec.String[Code]() }
func intervalCodec() codec.Codec[temporal.Interval] { return codec.Interval() }
