// Package webhookstore owns ordinary declared outbound webhook endpoint,
// signing-secret and delivery records. The public outbound package owns every
// state transition, encryption and network effect.
package webhookstore

import (
	"fmt"

	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

type Endpoint struct {
	ID     model.ID[Endpoint]
	URL    string
	Events value.JSON[[]string]
	Active bool
	// CreatedAt preserves explicit input; persistence supplies the owning clock when omitted.
	CreatedAt temporal.DateTime
	// UpdatedAt is refreshed from the owning clock after before-write hooks and before mutators.
	UpdatedAt temporal.DateTime
}

func (Endpoint) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("webhook endpoint")) }

// Secret stores one encrypted signing secret. A null ExpiresAt marks the
// current secret; a rotated secret keeps signing until it expires.
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
type Delivery struct {
	ID          model.ID[Delivery]
	EndpointID  model.ID[Endpoint]
	Event       string
	Payload     string
	State       string
	Attempts    uint32
	LastStatus  int32
	LastFailure string
	// CreatedAt preserves explicit input; persistence supplies the owning clock when omitted.
	CreatedAt temporal.DateTime
	// UpdatedAt is refreshed from the owning clock after before-write hooks and before mutators.
	UpdatedAt   temporal.DateTime
	DeliveredAt value.Nullable[temporal.DateTime]
}

func (Delivery) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("webhook delivery")) }
