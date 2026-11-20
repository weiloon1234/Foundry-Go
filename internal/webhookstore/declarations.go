package webhookstore

import (
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/temporal"
)

// EndpointTable binds the persisted Endpoint columns. Example: Endpoints.Query().Limit(10).
type EndpointTable struct {
	// Table provides model queries and writes. Example: orm.First(ctx, tx, Endpoints.Query()).
	orm.Table[Endpoint, model.ID[Endpoint]]
	// ID binds Endpoint.ID.
	// Example: orm.Set(Endpoints.ID, id).
	ID orm.Key[Endpoint, model.ID[Endpoint]]
	// URL binds Endpoint.URL.
	// Example: orm.Set(Endpoints.URL, url).
	URL orm.Text[Endpoint, string]
	// Events binds Endpoint.Events.
	// Example: encoded, err := value.NewJSON(events); if err == nil { _ = orm.Set(Endpoints.Events, encoded) }.
	Events orm.JSON[Endpoint, []string]
	// Active binds Endpoint.Active.
	// Example: orm.Set(Endpoints.Active, active).
	Active orm.Column[Endpoint, bool]
	// CreatedAt preserves an explicit creation time; the owning clock supplies omitted values.
	// Example: orm.Set(Endpoints.CreatedAt, createdAt).
	CreatedAt orm.CreatedAt[Endpoint, temporal.DateTime]
	// UpdatedAt is replaced by the owning clock after before-write hooks and before mutators.
	// Example: orm.Set(Endpoints.UpdatedAt, updatedAt).
	UpdatedAt orm.UpdatedAt[Endpoint, temporal.DateTime]
}

// Endpoints owns the immutable Endpoint declaration. Example: orm.First(ctx, tx, Endpoints.Query()).
var Endpoints = orm.Define("foundry_webhook_endpoints", func(b orm.Builder[Endpoint], m *Endpoint) EndpointTable {
	table := orm.TableOf(b, &m.ID)
	return EndpointTable{Table: table, ID: table.Key(), URL: orm.TextOf(b, &m.URL), Events: orm.JSONOf(b, &m.Events), Active: orm.ColumnOf(b, &m.Active), CreatedAt: orm.CreatedAtOf(b, &m.CreatedAt), UpdatedAt: orm.UpdatedAtOf(b, &m.UpdatedAt)}
})

// SecretTable binds the persisted Secret columns. Example: Secrets.Query().Limit(10).
type SecretTable struct {
	// Table provides model queries and writes. Example: orm.First(ctx, tx, Secrets.Query()).
	orm.Table[Secret, model.ID[Secret]]
	// ID binds Secret.ID.
	// Example: orm.Set(Secrets.ID, id).
	ID orm.Key[Secret, model.ID[Secret]]
	// EndpointID binds Secret.EndpointID.
	// Example: orm.Set(Secrets.EndpointID, endpointID).
	EndpointID orm.Column[Secret, model.ID[Endpoint]]
	// Ciphertext binds Secret.Ciphertext.
	// Example: orm.Set(Secrets.Ciphertext, ciphertext).
	Ciphertext orm.Text[Secret, string]
	// ExpiresAt binds Secret.ExpiresAt.
	// Example: orm.SetNull(Secrets.ExpiresAt).
	ExpiresAt orm.NullableTime[Secret, temporal.DateTime]
	// CreatedAt preserves an explicit creation time; callers supply this timestamp.
	// Example: orm.Set(Secrets.CreatedAt, createdAt).
	CreatedAt orm.Time[Secret, temporal.DateTime]
}

// Secrets owns the immutable Secret declaration. Example: orm.First(ctx, tx, Secrets.Query()).
var Secrets = orm.Define("foundry_webhook_secrets", func(b orm.Builder[Secret], m *Secret) SecretTable {
	table := orm.TableOf(b, &m.ID)
	return SecretTable{Table: table, ID: table.Key(), EndpointID: orm.ColumnOf(b, &m.EndpointID), Ciphertext: orm.TextOf(b, &m.Ciphertext), ExpiresAt: orm.NullableTimeOf(b, &m.ExpiresAt), CreatedAt: orm.TimeOf(b, &m.CreatedAt)}
})

// DeliveryTable binds the persisted Delivery columns. Example: Deliveries.Query().Limit(10).
type DeliveryTable struct {
	// Table provides model queries and writes. Example: orm.First(ctx, tx, Deliveries.Query()).
	orm.Table[Delivery, model.ID[Delivery]]
	// ID binds Delivery.ID.
	// Example: orm.Set(Deliveries.ID, id).
	ID orm.Key[Delivery, model.ID[Delivery]]
	// EndpointID binds Delivery.EndpointID.
	// Example: orm.Set(Deliveries.EndpointID, endpointID).
	EndpointID orm.Column[Delivery, model.ID[Endpoint]]
	// Event binds Delivery.Event.
	// Example: orm.Set(Deliveries.Event, event).
	Event orm.Text[Delivery, string]
	// Payload binds Delivery.Payload.
	// Example: orm.Set(Deliveries.Payload, payload).
	Payload orm.Text[Delivery, string]
	// State binds Delivery.State.
	// Example: orm.Set(Deliveries.State, state).
	State orm.Text[Delivery, string]
	// Attempts binds Delivery.Attempts.
	// Example: orm.Set(Deliveries.Attempts, attempts).
	Attempts orm.Number[Delivery, uint32]
	// LastStatus binds Delivery.LastStatus.
	// Example: orm.Set(Deliveries.LastStatus, lastStatus).
	LastStatus orm.Number[Delivery, int32]
	// LastFailure binds Delivery.LastFailure.
	// Example: orm.Set(Deliveries.LastFailure, lastFailure).
	LastFailure orm.Text[Delivery, string]
	// CreatedAt preserves an explicit creation time; the owning clock supplies omitted values.
	// Example: orm.Set(Deliveries.CreatedAt, createdAt).
	CreatedAt orm.CreatedAt[Delivery, temporal.DateTime]
	// UpdatedAt is replaced by the owning clock after before-write hooks and before mutators.
	// Example: orm.Set(Deliveries.UpdatedAt, updatedAt).
	UpdatedAt orm.UpdatedAt[Delivery, temporal.DateTime]
	// DeliveredAt binds Delivery.DeliveredAt.
	// Example: orm.SetNull(Deliveries.DeliveredAt).
	DeliveredAt orm.NullableTime[Delivery, temporal.DateTime]
}

// Deliveries owns the immutable Delivery declaration. Example: orm.First(ctx, tx, Deliveries.Query()).
var Deliveries = orm.Define("foundry_webhook_deliveries", func(b orm.Builder[Delivery], m *Delivery) DeliveryTable {
	table := orm.TableOf(b, &m.ID)
	return DeliveryTable{Table: table, ID: table.Key(), EndpointID: orm.ColumnOf(b, &m.EndpointID), Event: orm.TextOf(b, &m.Event), Payload: orm.TextOf(b, &m.Payload), State: orm.TextOf(b, &m.State), Attempts: orm.NumberOf(b, &m.Attempts), LastStatus: orm.NumberOf(b, &m.LastStatus), LastFailure: orm.TextOf(b, &m.LastFailure), CreatedAt: orm.CreatedAtOf(b, &m.CreatedAt), UpdatedAt: orm.UpdatedAtOf(b, &m.UpdatedAt), DeliveredAt: orm.NullableTimeOf(b, &m.DeliveredAt)}
})
