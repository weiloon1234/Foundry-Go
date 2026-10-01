package attachments

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/extensions"
	"github.com/weiloon1234/Foundry-Go/fault"
	store "github.com/weiloon1234/Foundry-Go/internal/attachmentstore"
	"github.com/weiloon1234/Foundry-Go/model"
)

// Cleanup joins a generated owner's transactional Deleted/ForceDeleted observer.
// Soft deletion preserves files for restoration. Ready/stored ownership changes
// and optional job outbox rows roll back with the owner. Writing/uncertain intents
// remain visible for settlement; they cannot be deleted based on owner absence.
func Cleanup[M any, K comparable](ctx context.Context, tx *database.Tx, m *Manager, owner extensions.Owner[M, K], reference model.Reference[M, K], operation lifecycle.Operation, queue *Queue) error {
	if err := m.Validate(); err != nil {
		return err
	}
	if err := owner.Check(m.store.Registry()); err != nil {
		return err
	}
	if operation == lifecycle.SoftDelete {
		return nil
	}
	if operation != lifecycle.Delete && operation != lifecycle.ForceDelete {
		return invalid()
	}
	subject, err := owner.Subject(reference)
	if err != nil {
		return err
	}
	identity, err := subject.Identity.Decode()
	if err != nil {
		return err
	}
	return m.cleanupSubject(ctx, tx, owner.Name(), subject, identity, queue)
}

// CleanupIdentity is Cleanup for an owner known by its registered name and
// persisted identity, such as a durable cleanup job's request. It joins tx,
// rejects an owner that still exists and deletes stored objects after commit.
func CleanupIdentity(ctx context.Context, tx *database.Tx, m *Manager, owner extensions.OwnerName, identity model.Identity, queue *Queue) error {
	if err := m.Validate(); err != nil {
		return err
	}
	subject, err := m.store.Registry().Subject(owner, identity)
	if err != nil {
		return err
	}
	return m.cleanupSubject(ctx, tx, owner, subject, identity, queue)
}
func (m *Manager) cleanupSubject(ctx context.Context, tx *database.Tx, owner extensions.OwnerName, subject extensions.Subject, identity model.Identity, queue *Queue) error {
	if queue != nil {
		if err := queue.Validate(); err != nil {
			return err
		}
	}
	return m.owners.Run(ctx, "attachment owner cleanup", func(ctx context.Context) error {
		return m.store.Join(ctx, tx, func(ctx context.Context, child *database.Tx) error {
			retained, err := m.store.Registry().RetainedSubjects(ctx, child, owner, []model.Identity{identity})
			if err != nil {
				return err
			}
			if retained[subject.Key] {
				return fault.New(fault.Conflict, "attachment cleanup requires a deleted owner")
			}
			ids, err := m.retireOwner(ctx, child, subject, queue)
			if err != nil {
				return err
			}
			return child.AfterCommit(func(ctx context.Context) error { return m.afterCommitCleanup(ctx, ids) })
		})
	})
}
func (m *Manager) retireOwner(ctx context.Context, tx *database.Tx, subject extensions.Subject, queue *Queue) ([]model.ID[store.File], error) {
	f := store.FileFields()
	rows, err := store.Index(store.QueryFoundryAttachments().Where(f.Scope.Eq(subject.Scope), f.SubjectKey.Eq(subject.Key), f.State.In(string(Ready), string(Stored))).OrderBy(f.ID.Asc()).Limit(MaxOwnerIntents+1)).ForUpdate().All(ctx, tx)
	if err != nil {
		return nil, err
	}
	if len(rows) > MaxOwnerIntents {
		return nil, invalid()
	}
	now, err := m.store.Now()
	if err != nil {
		return nil, err
	}
	ids := make([]model.ID[store.File], 0, len(rows))
	for _, row := range rows {
		if _, err := m.validateIndex(row); err != nil {
			return nil, err
		}
		if _, err := store.QueryFoundryAttachments().Update(ctx, tx, row.ID, store.FileDraft{}.SetState(string(CleanupPending)).SetUpdatedAt(now)); err != nil {
			return nil, err
		}
		if err := queue.enqueue(ctx, tx, operationID(row.ID)); err != nil {
			return nil, err
		}
		ids = append(ids, row.ID)
	}
	return ids, nil
}
func (m *Manager) afterCommitCleanup(ctx context.Context, ids []model.ID[store.File]) error {
	if len(ids) == 0 {
		return nil
	}
	return m.owners.Run(ctx, "attachment committed cleanup", func(ctx context.Context) error {
		cleanupCtx, cancel := m.cleanupContext(ctx)
		defer cancel()
		_, err := m.cleanupMany(cleanupCtx, ids)
		return err
	})
}
