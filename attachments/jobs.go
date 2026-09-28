package attachments

import (
	"context"
	"fmt"

	"github.com/weiloon1234/Foundry-Go/database"
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
