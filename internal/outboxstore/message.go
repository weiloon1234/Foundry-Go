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
	// Table provides Message queries and writes. Example: Messages.Query().Limit(10).
	orm.Table[Message, model.ID[Message]]
	// ID binds the persisted Message.ID column.
	// Example: orm.KeyEq(Messages.ID, id).
	ID orm.Key[Message, model.ID[Message]]
	// Kind binds the persisted Message.Kind column.
	// Example: orm.TextSet(Messages.Kind, kind).
	Kind orm.Text[Message, string]
	// Destination binds the persisted Message.Destination column.
	// Example: orm.TextSet(Messages.Destination, destination).
	Destination orm.Text[Message, outbox.Destination]
	// Name binds the persisted Message.Name column.
	// Example: orm.TextSet(Messages.Name, name).
	Name orm.Text[Message, string]
	// Version binds the persisted Message.Version column.
	// Example: orm.NumberSet(Messages.Version, version).
	Version orm.Number[Message, uint32]
	// Payload binds the persisted Message.Payload column.
	// Example: encoded, err := value.NewJSON(payload); if err == nil { _ = orm.JSONSet(Messages.Payload, encoded) }.
	Payload orm.JSON[Message, json.RawMessage]
	// Origin binds the persisted Message.Origin column.
	// Example: encoded, err := value.NewJSON(origin); if err == nil { _ = orm.JSONSet(Messages.Origin, encoded) }.
	Origin orm.JSON[Message, attribution.Origin]
	// CreatedAt preserves an explicitly assigned creation time; when omitted on insert, the database supplies its default.
	// Example: orm.TimeSet(Messages.CreatedAt, createdAt).
	CreatedAt orm.Time[Message, temporal.DateTime]
	// PublishState binds the persisted Message.PublishState column.
	// Example: orm.TextSet(Messages.PublishState, publishState).
	PublishState orm.Text[Message, outbox.PublicationState]
	// PublishAttempts binds the persisted Message.PublishAttempts column.
	// Example: orm.NumberSet(Messages.PublishAttempts, publishAttempts).
	PublishAttempts orm.Number[Message, uint32]
	// PublishAfter binds the persisted Message.PublishAfter column.
	// Example: orm.TimeSet(Messages.PublishAfter, publishAfter).
	PublishAfter orm.Time[Message, temporal.DateTime]
	// PublishedAt binds the persisted Message.PublishedAt column.
	// Example: orm.NullableTimeSetNull(Messages.PublishedAt).
	PublishedAt orm.NullableTime[Message, temporal.DateTime]
	// PublishReason binds the persisted Message.PublishReason column.
	// Example: orm.TextSet(Messages.PublishReason, publishReason).
	PublishReason orm.Text[Message, string]
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
