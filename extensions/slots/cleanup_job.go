package slots

import (
	"context"
	"fmt"
	"maps"

	"github.com/weiloon1234/Foundry-Go/attachments"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/extensions"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/errorgraph"
	"github.com/weiloon1234/Foundry-Go/jobs"
	"github.com/weiloon1234/Foundry-Go/metadata"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/translations"
)

// CleanupRequest identifies one model deleted through a connection other than
// the extension store's: its registered owner and persisted identity.
type CleanupRequest struct {
	Owner    extensions.OwnerName `json:"owner"`
	Identity model.Identity       `json:"identity"`
}

func (CleanupRequest) Format(s fmt.State, _ rune) {
	_, _ = s.Write([]byte("model extension cleanup request"))
}

// CleanupJob settles the extension data of models deleted through a
// connection other than the extension store's. The deletion enqueues it in its
// own transaction through the outbox, so a rollback leaves no job and a crash
// after commit loses none. Its handler is idempotent: in a store transaction
// it skips an owner that exists again and otherwise runs every configured
// manager's cleanup, which finds nothing left on a repeated delivery.
type CleanupJob struct {
	definition jobs.Definition[CleanupRequest]
}

func DefineCleanupJob(name jobs.Name, policy jobs.Policy) CleanupJob {
	return CleanupJob{definition: jobs.Define[CleanupRequest](name, 1, policy)}
}

// Definition is the typed job, for registration with a job connection.
func (j CleanupJob) Definition() jobs.Definition[CleanupRequest] { return j.definition }

// Handler binds the job to runtime's store and managers. It never uses
// runtime.CleanupQueue, so it can be declared before that queue exists.
func (j CleanupJob) Handler(runtime Runtime) (jobs.Handler[CleanupRequest], error) {
	if err := j.definition.Validate(); err != nil {
		return nil, err
	}
	if err := runtime.Store.Validate(); err != nil {
		return nil, err
	}
	// Without a manager every delivery would succeed without cleaning anything.
	if runtime.Metadata == nil && runtime.Translations == nil && runtime.Attachments == nil {
		return nil, fault.New(fault.Invalid, "model extension cleanup job requires a manager")
	}
	return func(ctx context.Context, request CleanupRequest) error {
		err := runtime.settle(ctx, request.Owner, request.Identity)
		// An unknown owner or a malformed identity cannot succeed on retry.
		if errorgraph.Is(err, fault.Invalid) {
			return jobs.Permanent(err)
		}
		return err
	}, nil
}

// Declare is the direct-assembly form of Handler.
func (j CleanupJob) Declare(runtime Runtime) (jobs.Declaration, error) {
	handler, err := j.Handler(runtime)
	if err != nil {
		return jobs.Declaration{}, err
	}
	return j.definition.Declare(handler)
}

// CleanupQueue enqueues CleanupJob through the outbox of the pool a deletion
// uses. Each producer must write the outbox that the application's publisher
// relays; applications give every pool that deletes owning models one.
type CleanupQueue struct {
	job       CleanupJob
	queue     jobs.Queue
	producers map[*database.DB]*jobs.Outbox
}

// ToOutbox binds one producer per pool, keyed by that pool, and the job queue
// its workers consume. The map is snapshotted.
func (j CleanupJob) ToOutbox(queue jobs.Queue, producers map[*database.DB]*jobs.Outbox) (*CleanupQueue, error) {
	q := &CleanupQueue{job: j, queue: queue, producers: maps.Clone(producers)}
	if err := q.Validate(); err != nil {
		return nil, err
	}
	return q, nil
}
func (q *CleanupQueue) Validate() error {
	if q == nil || len(q.producers) == 0 {
		return fault.New(fault.Invalid, "model extension cleanup queue requires outbox producers")
	}
	if err := q.job.definition.Validate(); err != nil {
		return err
	}
	if err := q.queue.Validate(); err != nil {
		return err
	}
	for db, producer := range q.producers {
		if db == nil || producer == nil {
			return fault.New(fault.Invalid, "model extension cleanup queue requires a pool and producer")
		}
		if err := producer.Destination().Validate(); err != nil {
			return err
		}
	}
	return nil
}
func (CleanupQueue) Format(s fmt.State, _ rune) {
	_, _ = s.Write([]byte("model extension cleanup queue"))
}

// enqueue writes the job inside tx through its pool's producer.
func (q *CleanupQueue) enqueue(ctx context.Context, tx *database.Tx, owner extensions.OwnerName, identity model.Identity) error {
	for db, producer := range q.producers {
		if tx.BelongsTo(db) {
			_, err := q.job.definition.Enqueue(ctx, tx, producer, CleanupRequest{Owner: owner, Identity: identity}, jobs.Options[CleanupRequest]{Queue: q.queue})
			return err
		}
	}
	return fault.New(fault.Invalid, "model deletion pool has no extension cleanup outbox")
}

// errOwnerExists rolls back a settlement whose owner was created again while
// it ran; settle then skips that owner as if it had found it first.
var errOwnerExists = fault.New(fault.Conflict, "model extension owner exists again")

// settle removes one deleted owner's extension data in one transaction of the
// store's pool, unless the owner exists again. Every step joins that plain
// transaction through its own store admission, as cleanup joins an owner's
// deletion transaction: an admission nested inside another of the same store
// does not wait for capacity and would fail whenever the store is busy.
func (r Runtime) settle(ctx context.Context, owner extensions.OwnerName, identity model.Identity) error {
	if err := r.Store.Validate(); err != nil {
		return err
	}
	registry := r.Store.Registry()
	key, err := registry.SubjectKey(owner, identity)
	if err != nil {
		return err
	}
	retained := func(ctx context.Context, tx *database.Tx) (bool, error) {
		var found bool
		err := r.Store.Join(ctx, tx, func(ctx context.Context, child *database.Tx) error {
			subjects, err := registry.RetainedSubjects(ctx, child, owner, []model.Identity{identity})
			found = subjects[key]
			return err
		})
		return found, err
	}
	err = r.Store.Database().Transaction(ctx, func(tx *database.Tx) error {
		if found, err := retained(ctx, tx); err != nil || found {
			return err
		}
		// A writer can create the owner again while settle runs: a manager then
		// refuses the existing owner, or a step waits on that writer's row
		// locks and READ COMMITTED matches its committed rows. Either way the
		// owner exists again, so roll back and skip it.
		settled := func(err error) error {
			found, checkErr := retained(ctx, tx)
			switch {
			case checkErr == nil && found:
				return errOwnerExists
			case err != nil:
				return err
			default:
				return checkErr
			}
		}
		if r.Metadata != nil {
			if err := metadata.CleanupIdentity(ctx, tx, r.Metadata, owner, identity); err != nil {
				return settled(err)
			}
		}
		if r.Translations != nil {
			if err := translations.CleanupIdentity(ctx, tx, r.Translations, owner, identity); err != nil {
				return settled(err)
			}
		}
		if r.Attachments != nil {
			if err := attachments.CleanupIdentity(ctx, tx, r.Attachments, owner, identity, nil); err != nil {
				return settled(err)
			}
		}
		// Commit only while the owner is still gone.
		return settled(nil)
	}, database.TxOptions{Isolation: database.ReadCommitted})
	if err == errOwnerExists {
		return nil
	}
	return err
}
