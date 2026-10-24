// Package outboxstore owns the heterogeneous persistence boundary shared by
// framework message producers. Application payloads stay typed in their feature.
package outboxstore

import (
	"encoding/json"
	"fmt"

	"github.com/weiloon1234/Foundry-Go/attribution"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/outbox"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Message is infrastructure storage, never a public response DTO. Payload has
// already passed its producer's concrete schema before reaching this boundary;
// the consumer must apply that schema again when restoring a persisted message.
// Publication progress is owned by the shared outbox publisher.
type Message struct {
	ID              model.ID[Message]
	Kind            string
	Destination     outbox.Destination
	Name            string
	Version         uint32
	Payload         value.JSON[json.RawMessage]
	Origin          value.JSON[attribution.Origin]
	CreatedAt       temporal.DateTime       `foundry:"default=database"`
	PublishState    outbox.PublicationState `foundry:"default=database"`
	PublishAttempts uint32                  `foundry:"default=database"`
	PublishAfter    temporal.DateTime       `foundry:"default=database"`
	PublishedAt     value.Nullable[temporal.DateTime]
	PublishReason   string `foundry:"default=database"`
}

// Format keeps persisted payload and provenance out of routine diagnostics.
func (Message) Format(state fmt.State, _ rune) { _, _ = state.Write([]byte("outbox message")) }

// MessageTable declares the outbox storage codecs. Example: Messages.Query().Limit(10).
type MessageTable struct {
	orm.Table[Message, model.ID[Message]]
	ID              orm.Key[Message, model.ID[Message]]
	Kind            orm.Text[Message, string]
	Destination     orm.Text[Message, outbox.Destination]
	Name            orm.Text[Message, string]
	Version         orm.Number[Message, uint32]
	Payload         orm.JSON[Message, json.RawMessage]
	Origin          orm.JSON[Message, attribution.Origin]
	CreatedAt       orm.Time[Message, temporal.DateTime]
	PublishState    orm.Text[Message, outbox.PublicationState]
	PublishAttempts orm.Number[Message, uint32]
	PublishAfter    orm.Time[Message, temporal.DateTime]
	PublishedAt     orm.NullableTime[Message, temporal.DateTime]
	PublishReason   orm.Text[Message, string]
}

// Messages owns the immutable outbox declaration. Example: Messages.Find(ctx, tx, id).
var Messages = orm.Define("foundry_outbox", func(b orm.Builder[Message], m *Message) MessageTable {
	table := orm.TableOf(b, &m.ID)
	return MessageTable{
		Table: table, ID: table.Key(), Kind: orm.TextOf(b, &m.Kind),
		Destination: orm.TextOf(b, &m.Destination), Name: orm.TextOf(b, &m.Name),
		Version: orm.NumberOf(b, &m.Version), Payload: orm.JSONOf(b, &m.Payload),
		Origin: orm.JSONOf(b, &m.Origin), CreatedAt: orm.TimeOf(b, &m.CreatedAt),
		PublishState: orm.TextOf(b, &m.PublishState), PublishAttempts: orm.NumberOf(b, &m.PublishAttempts),
		PublishAfter: orm.TimeOf(b, &m.PublishAfter), PublishedAt: orm.NullableTimeOf(b, &m.PublishedAt),
		PublishReason: orm.TextOf(b, &m.PublishReason),
	}
})
