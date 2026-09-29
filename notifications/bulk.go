package notifications

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/attribution"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/sqlscope"
	"github.com/weiloon1234/Foundry-Go/jobs"
	"github.com/weiloon1234/Foundry-Go/model"
)

// MaxBulkRecipients bounds one EnqueueMany call; MaxBulkBatch bounds the
// notifications stored per transaction.
const (
	MaxBulkRecipients = 10000
	MaxBulkBatch      = 500
)

// BulkResult reports every notification ID in recipient order, grouped by
// outcome. Stored batches committed. Unconfirmed is the batch whose
// transaction failed: its commit outcome may be unknown, so reconcile those
// IDs (for example with Inbox.Status or Manager.Deliveries) or retry them with
// EnqueueManyWithIDs, which is idempotent per ID. NotAttempted lists IDs after
// the failed batch that were never stored.
type BulkResult[M any] struct {
	Stored       []ID[M]
	Unconfirmed  []ID[M]
	NotAttempted []ID[M]
}

// EnqueueMany notifies many recipients with the same input. Each recipient
// gets its own captured notification and delivery job; each batch of up to
// batch (default 100, at most MaxBulkBatch) notifications is stored together
// with its delivery-job outbox rows in one transaction of its own, never the
// caller's. Delivery then runs through the job worker. Capture validates every
// recipient before the first batch is stored. IDs are generated; use
// EnqueueManyWithIDs to supply them for retries.
func (b Binding[M, K, P]) EnqueueMany(ctx context.Context, manager *Manager, job DeliveryJob, producer *jobs.Outbox, recipients []model.Reference[M, K], input P, batch int) (BulkResult[M], error) {
	return b.EnqueueManyWithIDs(ctx, manager, job, producer, recipients, nil, input, batch)
}

// EnqueueManyWithIDs is EnqueueMany with caller-supplied notification IDs, one
// per recipient (a zero ID generates one; nil generates all). Re-running it
// with the same IDs and input after an uncertain batch stores each
// notification at most once.
func (b Binding[M, K, P]) EnqueueManyWithIDs(ctx context.Context, manager *Manager, job DeliveryJob, producer *jobs.Outbox, recipients []model.Reference[M, K], ids []ID[M], input P, batch int) (BulkResult[M], error) {
	if batch == 0 {
		batch = 100
	}
	if manager == nil || producer == nil || len(recipients) == 0 || len(recipients) > MaxBulkRecipients || batch < 1 || batch > MaxBulkBatch || ids != nil && len(ids) != len(recipients) {
		return BulkResult[M]{}, fault.New(fault.Invalid, "bulk notification requires a manager, producer, 1 to 10000 recipients, one ID per recipient when supplied and a valid batch size")
	}
	pending := make([]PendingNotification[M, P], len(recipients))
	for i, recipient := range recipients {
		var id ID[M]
		if ids != nil {
			id = ids[i]
		}
		captured, err := b.Capture(ctx, recipient, input, id)
		if err != nil {
			return BulkResult[M]{}, err
		}
		pending[i] = captured
	}
	var result BulkResult[M]
	for start := 0; start < len(pending); start += batch {
		chunk := pending[start:min(start+batch, len(pending))]
		records := make([]*captured, len(chunk))
		for i, item := range chunk {
			records[i] = item.capture
		}
		err := manager.enqueueBatch(ctx, job, producer, records)
		for _, item := range chunk {
			if err != nil {
				result.Unconfirmed = append(result.Unconfirmed, item.ID())
			} else {
				result.Stored = append(result.Stored, item.ID())
			}
		}
		if err != nil {
			for _, item := range pending[start+len(chunk):] {
				result.NotAttempted = append(result.NotAttempted, item.ID())
			}
			return result, err
		}
	}
	return result, nil
}

// enqueueBatch stores one batch and its delivery jobs atomically.
func (m *Manager) enqueueBatch(ctx context.Context, job DeliveryJob, producer *jobs.Outbox, items []*captured) error {
	ctx, release, err := m.begin(ctx)
	if err != nil {
		return err
	}
	defer release()
	return m.db.Transaction(ctx, func(tx *database.Tx) error {
		return sqlscope.InSchema(ctx, tx, m.db, m.config.Schema, func(child *database.Tx) error {
			for _, captured := range items {
				if captured == nil || m.registry.entries[captured.registration.key] != captured.registration {
					return invalid()
				}
				row, err := m.persist(ctx, child, captured)
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
				request := DeliveryRequest{ID: model.IDFromBytes[Notification](captured.id.Bytes())}
				pending, err := job.definition.Capture(publishContext, request, jobs.Options[DeliveryRequest]{ID: model.IDFromBytes[jobs.ExecutionOf[DeliveryRequest]](captured.id.Bytes())})
				if err != nil {
					return err
				}
				if _, err := pending.Enqueue(publishContext, child, producer); err != nil {
					return err
				}
			}
			return nil
		})
	})
}
