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

//foundry:model table=foundry_webhook_endpoints var=Endpoints key=ID identity=false
type Endpoint struct {
	// Table provides model queries and writes. Example: orm.First(ctx, tx, Endpoints.Query()).
	//
	// ID binds Endpoint.ID.
	// Example: orm.Set(Endpoints.ID, id).
	ID model.ID[Endpoint] `foundry:"member=ID"`
	// URL binds Endpoint.URL.
	// Example: orm.Set(Endpoints.URL, url).
	URL string
	// Events binds Endpoint.Events.
	// Example: encoded, err := value.NewJSON(events); if err == nil { _ = orm.Set(Endpoints.Events, encoded) }.
	Events value.JSON[[]string]
	// Active binds Endpoint.Active.
	// Example: orm.Set(Endpoints.Active, active).
	Active bool `foundry:"role=column"`
	// CreatedAt preserves explicit input; persistence supplies the owning clock when omitted.
	//
	// CreatedAt preserves an explicit creation time; the owning clock supplies omitted values.
	// Example: orm.Set(Endpoints.CreatedAt, createdAt).
	CreatedAt temporal.DateTime `foundry:"role=created-at"`
	// UpdatedAt is refreshed from the owning clock after before-write hooks and before mutators.
	//
	// UpdatedAt is replaced by the owning clock after before-write hooks and before mutators.
	// Example: orm.Set(Endpoints.UpdatedAt, updatedAt).
	UpdatedAt temporal.DateTime `foundry:"role=updated-at"`
}

func (Endpoint) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("webhook endpoint")) }

// Secret stores one encrypted signing secret. A null ExpiresAt marks the
// current secret; a rotated secret keeps signing until it expires.
//
//foundry:model table=foundry_webhook_secrets var=Secrets key=ID identity=false
type Secret struct {
	// Table provides model queries and writes. Example: orm.First(ctx, tx, Secrets.Query()).
	//
	// ID binds Secret.ID.
	// Example: orm.Set(Secrets.ID, id).
	ID model.ID[Secret] `foundry:"member=ID"`
	// EndpointID binds Secret.EndpointID.
	// Example: orm.Set(Secrets.EndpointID, endpointID).
	EndpointID model.ID[Endpoint] `foundry:"role=column"`
	// Ciphertext binds Secret.Ciphertext.
	// Example: orm.Set(Secrets.Ciphertext, ciphertext).
	Ciphertext string
	// ExpiresAt binds Secret.ExpiresAt.
	// Example: orm.SetNull(Secrets.ExpiresAt).
	ExpiresAt value.Nullable[temporal.DateTime]
	// CreatedAt preserves an explicit creation time; callers supply this timestamp.
	// Example: orm.Set(Secrets.CreatedAt, createdAt).
	CreatedAt temporal.DateTime `foundry:"role=time"`
}

func (Secret) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("webhook signing secret")) }

// Delivery is the delivery log and durable payload of one message. Its ID is
// the stable webhook-id across attempts and replays.
//
//foundry:model table=foundry_webhook_deliveries var=Deliveries key=ID identity=false
type Delivery struct {
	// Table provides model queries and writes. Example: orm.First(ctx, tx, Deliveries.Query()).
	//
	// ID binds Delivery.ID.
	// Example: orm.Set(Deliveries.ID, id).
	ID model.ID[Delivery] `foundry:"member=ID"`
	// EndpointID binds Delivery.EndpointID.
	// Example: orm.Set(Deliveries.EndpointID, endpointID).
	EndpointID model.ID[Endpoint] `foundry:"role=column"`
	// Event binds Delivery.Event.
	// Example: orm.Set(Deliveries.Event, event).
	Event string
	// Payload binds Delivery.Payload.
	// Example: orm.Set(Deliveries.Payload, payload).
	Payload string
	// State binds Delivery.State.
	// Example: orm.Set(Deliveries.State, state).
	State string
	// Attempts binds Delivery.Attempts.
	// Example: orm.Set(Deliveries.Attempts, attempts).
	Attempts uint32
	// LastStatus binds Delivery.LastStatus.
	// Example: orm.Set(Deliveries.LastStatus, lastStatus).
	LastStatus int32
	// LastFailure binds Delivery.LastFailure.
	// Example: orm.Set(Deliveries.LastFailure, lastFailure).
	LastFailure string
	// CreatedAt preserves explicit input; persistence supplies the owning clock when omitted.
	//
	// CreatedAt preserves an explicit creation time; the owning clock supplies omitted values.
	// Example: orm.Set(Deliveries.CreatedAt, createdAt).
	CreatedAt temporal.DateTime `foundry:"role=created-at"`
	// UpdatedAt is refreshed from the owning clock after before-write hooks and before mutators.
	//
	// UpdatedAt is replaced by the owning clock after before-write hooks and before mutators.
	// Example: orm.Set(Deliveries.UpdatedAt, updatedAt).
	UpdatedAt temporal.DateTime `foundry:"role=updated-at"`
	// DeliveredAt binds Delivery.DeliveredAt.
	// Example: orm.SetNull(Deliveries.DeliveredAt).
	DeliveredAt value.Nullable[temporal.DateTime]
}

func (Delivery) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("webhook delivery")) }
