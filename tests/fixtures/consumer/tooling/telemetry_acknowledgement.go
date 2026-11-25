package tooling

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/telemetry"
)

// doc-example:begin persisted-telemetry-1
// TelemetryConsoleActor identifies the operator whose current authority is checked.
type TelemetryConsoleActor struct {
	// ID retains the actor's concrete model owner.
	ID model.ID[TelemetryConsoleActor]
}

// TelemetryActorAttribution records which episode the operator acknowledged.
type TelemetryActorAttribution struct {
	// Key identifies the alert being acknowledged.
	Key telemetry.AlertKey
	// Episode distinguishes a reopened alert from a previous acknowledgement.
	Episode telemetry.Episode
	// Actor identifies the authorized operator.
	Actor model.ID[TelemetryConsoleActor]
}

// TelemetryActorRepository owns current authorization and durable actor attribution.
type TelemetryActorRepository interface {
	// LockAuthorized locks and checks the actor using the caller's transaction.
	LockAuthorized(context.Context, *database.Tx, model.ID[TelemetryConsoleActor]) (TelemetryConsoleActor, error)
	// RecordAcknowledgement persists actor attribution in the same transaction.
	RecordAcknowledgement(context.Context, *database.Tx, TelemetryActorAttribution) error
}

// AcknowledgeAlert runs authority checks and attribution in the acknowledgement transaction.
func AcknowledgeAlert(ctx context.Context, db *database.DB, engine *telemetry.Engine, actors TelemetryActorRepository, actorID model.ID[TelemetryConsoleActor], key telemetry.AlertKey) error {
	return db.Transaction(ctx, func(tx *database.Tx) error {
		actor, err := actors.LockAuthorized(ctx, tx, actorID)
		if err != nil {
			return err
		}
		ack, err := engine.AcknowledgeIn(ctx, tx, key)
		if err != nil {
			return err
		}
		return actors.RecordAcknowledgement(ctx, tx, TelemetryActorAttribution{
			Key: ack.Key, Episode: ack.Episode, Actor: actor.ID,
		})
	})
}

// doc-example:end persisted-telemetry-1
