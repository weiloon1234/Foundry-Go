package attachments

import (
	"context"
	"fmt"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/errorgraph"
	"github.com/weiloon1234/Foundry-Go/jobs"
)

type ReconcileRequest struct {
	Operation OperationID `json:"operation"`
}

func (ReconcileRequest) Format(s fmt.State, _ rune) {
	_, _ = s.Write([]byte("attachment reconciliation request"))
}

type ReconcileJob struct {
	definition jobs.Definition[ReconcileRequest]
}

func DefineReconcileJob(name jobs.Name, policy jobs.Policy) ReconcileJob {
	return ReconcileJob{definition: jobs.Define[ReconcileRequest](name, 1, policy)}
}
func (j ReconcileJob) Declare(manager *Manager) (jobs.Declaration, error) {
	if err := manager.Validate(); err != nil {
		return jobs.Declaration{}, err
	}
	return j.definition.Declare(func(ctx context.Context, request ReconcileRequest) error {
		if request.Operation.IsZero() {
			return jobs.Permanent(invalid())
		}
		result, err := manager.Reconcile(ctx, request.Operation)
		if result.State == Writing || result.State == Uncertain {
			return jobs.Permanent(err)
		}
		return err
	})
}

// Queue is a borrowed job/outbox binding. Construct the manager, declare its
// reconcile job, assemble the ordinary dispatcher/outbox, then bind the queue
// to collections. This avoids constructor dependency cycles.
type Queue struct {
	job      ReconcileJob
	producer *jobs.Outbox
}

func (j ReconcileJob) ToOutbox(producer *jobs.Outbox) (Queue, error) {
	q := Queue{job: j, producer: producer}
	if err := q.Validate(); err != nil {
		return Queue{}, err
	}
	return q, nil
}
func (q Queue) Validate() error {
	if err := q.job.definition.Validate(); err != nil {
		return err
	}
	if q.producer == nil {
		return invalid()
	}
	return q.producer.Destination().Validate()
}
func (q *Queue) enqueue(ctx context.Context, tx *database.Tx, id OperationID) error {
	if q == nil {
		return nil
	}
	if err := q.Validate(); err != nil {
		return err
	}
	_, err := q.job.definition.Enqueue(ctx, tx, q.producer, ReconcileRequest{Operation: id}, jobs.Options[ReconcileRequest]{})
	return err
}

// Enqueue is available for explicit operator retries. The cleanup operation
// itself is idempotent; each requested enqueue receives a normal new execution ID.
func (q Queue) Enqueue(ctx context.Context, tx *database.Tx, id OperationID) error {
	if id.IsZero() {
		return invalid()
	}
	return q.enqueue(ctx, tx, id)
}

// VariantRequest identifies one attachment whose declared variants are generated.
type VariantRequest struct {
	Operation OperationID `json:"operation"`
}

func (VariantRequest) Format(s fmt.State, _ rune) {
	_, _ = s.Write([]byte("attachment variant request"))
}

// VariantJob generates missing variants of one ready attachment. Retries are
// idempotent: completed variants are not generated again.
type VariantJob struct {
	definition jobs.Definition[VariantRequest]
}

func DefineVariantJob(name jobs.Name, policy jobs.Policy) VariantJob {
	return VariantJob{definition: jobs.Define[VariantRequest](name, 1, policy)}
}
func (j VariantJob) Declare(manager *Manager) (jobs.Declaration, error) {
	if err := manager.Validate(); err != nil {
		return jobs.Declaration{}, err
	}
	return j.definition.Declare(func(ctx context.Context, request VariantRequest) error {
		if request.Operation.IsZero() {
			return jobs.Permanent(invalid())
		}
		err := manager.GenerateVariants(ctx, request.Operation)
		// A removed or corrupt intent cannot succeed on retry.
		if errorgraph.Is(err, fault.Invalid) || errorgraph.Is(err, database.NotFound) {
			return jobs.Permanent(err)
		}
		return err
	})
}

// VariantQueue is a borrowed job/outbox binding for queued variant generation.
// Bind it with Collection.WithVariantQueue after the manager and its job are
// declared; the publication transaction then enqueues one job per upload.
type VariantQueue struct {
	job      VariantJob
	producer *jobs.Outbox
}

func (j VariantJob) ToOutbox(producer *jobs.Outbox) (VariantQueue, error) {
	q := VariantQueue{job: j, producer: producer}
	if err := q.Validate(); err != nil {
		return VariantQueue{}, err
	}
	return q, nil
}
func (q VariantQueue) Validate() error {
	if err := q.job.definition.Validate(); err != nil {
		return err
	}
	if q.producer == nil {
		return invalid()
	}
	return q.producer.Destination().Validate()
}
func (q *VariantQueue) enqueue(ctx context.Context, tx *database.Tx, id OperationID) error {
	if q == nil {
		return nil
	}
	if err := q.Validate(); err != nil {
		return err
	}
	_, err := q.job.definition.Enqueue(ctx, tx, q.producer, VariantRequest{Operation: id}, jobs.Options[VariantRequest]{})
	return err
}

// WithVariantQueue generates this collection's variants through the queued
// job instead of synchronously after each upload. It does not change the
// collection's registered policy or identity.
func (c Collection[M, K]) WithVariantQueue(queue VariantQueue) Collection[M, K] {
	c.variantQueue = &queue
	return c
}
