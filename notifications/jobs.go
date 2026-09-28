package notifications

import (
	"context"
	"fmt"

	"github.com/weiloon1234/Foundry-Go/attribution"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	store "github.com/weiloon1234/Foundry-Go/internal/notificationstore"
	"github.com/weiloon1234/Foundry-Go/internal/sqlscope"
	"github.com/weiloon1234/Foundry-Go/jobs"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/outbox"
)

// DeliveryRequest is the job's small, versioned transport DTO. All private input,
// rendered content and per-channel outcomes remain in notification persistence.
type DeliveryRequest struct {
	ID NotificationID `json:"id"`
}

func (DeliveryRequest) Format(s fmt.State, _ rune) {
	_, _ = s.Write([]byte("notification delivery request"))
}

type DeliveryJob struct {
	definition jobs.Definition[DeliveryRequest]
}

func DefineDeliveryJob(name jobs.Name, policy jobs.Policy) DeliveryJob {
	return DeliveryJob{definition: jobs.Define[DeliveryRequest](name, 1, policy)}
}
func (d DeliveryJob) Declare(manager *Manager) (jobs.Declaration, error) {
	if manager == nil || manager.done == nil {
		return jobs.Declaration{}, invalid()
	}
	return d.definition.Declare(func(ctx context.Context, request DeliveryRequest) error {
		if request.ID.IsZero() {
			return jobs.Permanent(invalid())
		}
		ctx, release, err := manager.begin(ctx)
		if err != nil {
			return err
		}
		defer release()
		statuses, err := manager.deliver(ctx, model.IDFromBytes[store.Envelope](request.ID.Bytes()))
		for _, status := range statuses {
			if status.State.Retryable() {
				return fault.New(fault.Internal, "notification channels require retry")
			}
		}
		if len(statuses) == 0 {
			return err
		}
		if err != nil {
			return jobs.Permanent(fault.New(fault.Internal, "notification delivery is terminal"))
		}
		return nil
	})
}

// Enqueue atomically persists the captured notification and an ordinary job
// outbox row inside the business transaction. Use the SAME schema for notification
// and outbox migrations. The savepoint prevents partial work even if callers
// ignore an error. The returned ID is provisional until the outer commit.
func (p PendingNotification[M, P]) Enqueue(ctx context.Context, tx *database.Tx, manager *Manager, job DeliveryJob, producer *jobs.Outbox) (outbox.ID[DeliveryRequest], error) {
	ctx, release, err := manager.begin(ctx)
	if err != nil {
		return outbox.ID[DeliveryRequest]{}, err
	}
	defer release()
	if err := p.check(manager); err != nil {
		return outbox.ID[DeliveryRequest]{}, err
	}
	request := DeliveryRequest{ID: model.IDFromBytes[Notification](p.capture.id.Bytes())}
	var result outbox.ID[DeliveryRequest]
	err = sqlscope.InSchema(ctx, tx, manager.db, manager.config.Schema, func(child *database.Tx) error {
		row, err := manager.persist(ctx, child, p.capture)
		if err != nil {
			return err
		}
		origin, err := row.Origin.Decode()
		if err != nil {
			return err
		}
		publishContext, err := attribution.WithContext(ctx, origin)
		if err != nil {
			return err
		}
		pending, err := job.definition.Capture(publishContext, request, jobs.Options[DeliveryRequest]{ID: model.IDFromBytes[jobs.ExecutionOf[DeliveryRequest]](p.capture.id.Bytes())})
		if err != nil {
			return err
		}
		result, err = pending.Enqueue(publishContext, child, producer)
		return err
	})
	if err != nil {
		return outbox.ID[DeliveryRequest]{}, err
	}
	return result, nil
}
