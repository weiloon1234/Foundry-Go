package outboxstore

import (
	"context"
	"encoding/json"

	"github.com/weiloon1234/Foundry-Go/attribution"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/identifier"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/outbox"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Address is the explicit internal heterogeneous routing boundary. The owning
// feature derives it from its registered typed descriptor and destination.
type Address struct {
	Kind, Name  string
	Destination outbox.Destination
	Version     uint32
}

func (a Address) Validate() error {
	if !identifier.Semantic(a.Kind) || !identifier.Semantic(a.Name) || a.Version == 0 {
		return fault.New(fault.Invalid, "outbox message requires a declared address and nonzero version")
	}
	return a.Destination.Validate()
}

// Append records already captured payload/provenance through the actual business
// transaction. It neither commits the outer transaction nor dispatches listeners.
// Unknown commit outcomes remain the caller's transaction responsibility.
func Append(ctx context.Context, tx *database.Tx, address Address, payload string, origin attribution.Origin) (Message, error) {
	if ctx == nil || tx == nil {
		return Message{}, fault.New(fault.Invalid, "outbox enqueue requires a context and transaction")
	}
	if err := ctx.Err(); err != nil {
		return Message{}, err
	}
	if err := address.Validate(); err != nil {
		return Message{}, err
	}
	body, err := value.ParseJSON[json.RawMessage](payload)
	if err != nil {
		return Message{}, err
	}
	capturedOrigin, err := value.NewJSON(origin)
	if err != nil {
		return Message{}, err
	}
	return QueryFoundryOutbox().Create(ctx, tx, MessageDraft{}.
		SetKind(address.Kind).
		SetDestination(address.Destination).
		SetName(address.Name).
		SetVersion(address.Version).
		SetPayload(body).
		SetOrigin(capturedOrigin))
}

// Find requires the entire routing address in addition to the physical ID. An
// ID from another destination/topic/version does not select a foreign message.
// The feature must still decode the returned payload using its concrete schema.
func Find(ctx context.Context, executor database.Executor, address Address, id model.ID[Message]) (value.Optional[Message], error) {
	if err := address.Validate(); err != nil {
		return value.Optional[Message]{}, err
	}
	if id.IsZero() {
		return value.Optional[Message]{}, fault.New(fault.Invalid, "outbox lookup requires a nonzero message ID")
	}
	fields := MessageFields()
	return QueryFoundryOutbox().Where(
		fields.Kind.Eq(address.Kind),
		fields.Destination.Eq(address.Destination),
		fields.Name.Eq(address.Name),
		fields.Version.Eq(address.Version),
	).Find(ctx, executor, id)
}
