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
//foundry:model table=foundry_outbox
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
