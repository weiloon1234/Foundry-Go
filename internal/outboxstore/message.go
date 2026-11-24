// Package outboxstore owns the heterogeneous persistence boundary shared by
// framework message producers. Application payloads stay typed in their feature.
package outboxstore

import (
	"encoding/json"
	"fmt"

	"github.com/weiloon1234/Foundry-Go/attribution"

	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/outbox"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Message is infrastructure storage, never a public response DTO. Payload has
// already passed its producer's concrete schema before reaching this boundary;
// the consumer must apply that schema again when restoring a persisted message.
// Publication progress is owned by the shared outbox publisher.
//
//foundry:model table=foundry_outbox var=Messages key=ID identity=false
type Message struct {
	// Table provides Message queries and writes. Example: Messages.Query().Limit(10).
	//
	// ID binds the persisted Message.ID column.
	// Example: orm.Eq(Messages.ID, id).
	ID model.ID[Message] `foundry:"member=ID"`
	// Kind binds the persisted Message.Kind column.
	// Example: orm.Set(Messages.Kind, kind).
	Kind string
	// Destination binds the persisted Message.Destination column.
	// Example: orm.Set(Messages.Destination, destination).
	Destination outbox.Destination
	// Name binds the persisted Message.Name column.
	// Example: orm.Set(Messages.Name, name).
	Name string
	// Version binds the persisted Message.Version column.
	// Example: orm.Set(Messages.Version, version).
	Version uint32
	// Payload binds the persisted Message.Payload column.
	// Example: encoded, err := value.NewJSON(payload); if err == nil { _ = orm.Set(Messages.Payload, encoded) }.
	Payload value.JSON[json.RawMessage]
	// Origin binds the persisted Message.Origin column.
	// Example: encoded, err := value.NewJSON(origin); if err == nil { _ = orm.Set(Messages.Origin, encoded) }.
	Origin value.JSON[attribution.Origin]
	// CreatedAt preserves an explicitly assigned creation time; when omitted on insert, the database supplies its default.
	// Example: orm.Set(Messages.CreatedAt, createdAt).
	CreatedAt temporal.DateTime `foundry:"default=database,role=time"`
	// PublishState binds the persisted Message.PublishState column.
	// Example: orm.Set(Messages.PublishState, publishState).
	PublishState outbox.PublicationState `foundry:"default=database"`
	// PublishAttempts binds the persisted Message.PublishAttempts column.
	// Example: orm.Set(Messages.PublishAttempts, publishAttempts).
	PublishAttempts uint32 `foundry:"default=database"`
	// PublishAfter binds the persisted Message.PublishAfter column.
	// Example: orm.Set(Messages.PublishAfter, publishAfter).
	PublishAfter temporal.DateTime `foundry:"default=database"`
	// PublishedAt binds the persisted Message.PublishedAt column.
	// Example: orm.SetNull(Messages.PublishedAt).
	PublishedAt value.Nullable[temporal.DateTime]
	// PublishReason binds the persisted Message.PublishReason column.
	// Example: orm.Set(Messages.PublishReason, publishReason).
	PublishReason string `foundry:"default=database"`
}

// Format keeps persisted payload and provenance out of routine diagnostics.
func (Message) Format(state fmt.State, _ rune) { _, _ = state.Write([]byte("outbox message")) }
