// Package webhookstore owns ordinary generated outbound webhook endpoint,
// signing-secret and delivery records. The public outbound package owns every
// state transition, encryption and network effect.
package webhookstore

import (
	"fmt"

	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

//foundry:model table=foundry_webhook_endpoints primary=ID
type Endpoint struct {
	ID     model.ID[Endpoint]
	URL    string
	Events value.JSON[[]string]
	Active bool
	// Foundry field behavior (generated): Managed creation timestamp: persistence supplies the owning application clock when omitted, after before-write hooks and before field mutators. An explicit input is preserved.
	CreatedAt temporal.DateTime
	// Foundry field behavior (generated): Managed update timestamp: persistence replaces assigned or omitted input with the owning application clock after before-write hooks and before field mutators. Conflict updates copy its normalized proposed value.
	UpdatedAt temporal.DateTime
}

func (Endpoint) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("webhook endpoint")) }

// Secret stores one encrypted signing secret. A null ExpiresAt marks the
// current secret; a rotated secret keeps signing until it expires.
//
//foundry:model table=foundry_webhook_secrets primary=ID
type Secret struct {
	ID         model.ID[Secret]
	EndpointID model.ID[Endpoint]
	Ciphertext string
	ExpiresAt  value.Nullable[temporal.DateTime]
	CreatedAt  temporal.DateTime
}

func (Secret) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("webhook signing secret")) }

// Delivery is the delivery log and durable payload of one message. Its ID is
// the stable webhook-id across attempts and replays.
//
//foundry:model table=foundry_webhook_deliveries primary=ID
type Delivery struct {
	ID          model.ID[Delivery]
	EndpointID  model.ID[Endpoint]
	Event       string
	Payload     string
	State       string
	Attempts    uint32
	LastStatus  int32
	LastFailure string
	// Foundry field behavior (generated): Managed creation timestamp: persistence supplies the owning application clock when omitted, after before-write hooks and before field mutators. An explicit input is preserved.
	CreatedAt temporal.DateTime
	// Foundry field behavior (generated): Managed update timestamp: persistence replaces assigned or omitted input with the owning application clock after before-write hooks and before field mutators. Conflict updates copy its normalized proposed value.
	UpdatedAt   temporal.DateTime
	DeliveredAt value.Nullable[temporal.DateTime]
}

func (Delivery) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("webhook delivery")) }
