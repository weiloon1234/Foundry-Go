package attachments

import (
	"context"
	"crypto/sha256"
	"errors"
	"math"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/fault"
	store "github.com/weiloon1234/Foundry-Go/internal/attachmentstore"
	"github.com/weiloon1234/Foundry-Go/internal/errorgraph"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/storage"
	"github.com/weiloon1234/Foundry-Go/temporal"
)

type ReconcileResult struct {
	Operation OperationID
	State     State
	Attempts  uint32
	Failure   string
}

func status(row store.File) ReconcileResult {
	return ReconcileResult{Operation: operationID(row.ID), State: State(row.State), Attempts: row.Attempts, Failure: row.LastFailure}
}
func (m *Manager) Reconcile(ctx context.Context, id OperationID) (ReconcileResult, error) {
	if err := m.Validate(); err != nil {
		return ReconcileResult{}, err
	}
	if id.IsZero() {
		return ReconcileResult{}, invalid()
	}
	var result ReconcileResult
	err := m.calls.Run(ctx, "attachment reconciliation", func(ctx context.Context) error {
		var err error
		result, err = m.reconcile(ctx, fileID(id), true)
		return err
	})
	return result, err
}

// reconcile only deletes a known published object of a completed storage write.
// Moving Stored to Cleanup fences finalization in every process before I/O.
func (m *Manager) reconcile(ctx context.Context, id model.ID[store.File], allowStored bool) (ReconcileResult, error) {
	var row store.File
	err := m.store.Write(ctx, func(ctx context.Context, tx *database.Tx) error {
		var err error
		row, err = store.QueryFoundryAttachments().ForUpdate().RequireFind(ctx, tx, id)
		if err != nil {
			return err
		}
		if _, err := m.validateRow(row); err != nil {
			return err
		}
		switch State(row.State) {
		case Ready, Cleaned, Retained:
			return nil
		case Writing, Uncertain:
			return fault.New(fault.Conflict, "upload needs explicit writer and storage settlement")
		case Stored:
			now, err := m.store.Now()
			if err != nil {
				return err
			}
			if !allowStored || m.isWriting(id) || now.UTC().Sub(row.UpdatedAt.UTC()) < m.config.StoredGrace {
				return fault.New(fault.Conflict, "stored upload is still reserved for publication")
			}
			row, err = store.QueryFoundryAttachments().Update(ctx, tx, id, store.FileDraft{}.SetState(string(CleanupPending)).SetLastFailure("publication_abandoned").SetUpdatedAt(now))
			return err
		case CleanupPending:
			return nil
		default:
			return invalid()
		}
	})
	if err != nil {
		return status(row), err
	}
	if row.State == string(Ready) || row.State == string(Cleaned) || row.State == string(Retained) {
		return status(row), nil
	}
	disk, err := m.disks.Disk(storage.DiskID(row.Disk))
	if err != nil {
		return status(row), err
	}
	key, err := storage.ParseKey(row.ObjectKey)
	if err != nil {
		return status(row), err
	}
	deleteErr := disk.Delete(ctx, key, storage.DeleteOptions{IfMatch: storage.ETag(row.ETag), Version: storage.VersionID(row.ObjectVersion)})
	if deleteErr != nil && (errorgraph.Is(deleteErr, storage.PreconditionFailed) || errorgraph.Is(deleteErr, storage.NotFound)) {
		// Conditional deletion of an already absent version may conflict. Only
		// confirmed absence is success; a changed current object is never deleted.
		_, statErr := disk.Stat(ctx, key, storage.ReadOptions{Version: storage.VersionID(row.ObjectVersion)})
		if errorgraph.Is(statErr, storage.NotFound) {
			deleteErr = nil
		} else if statErr != nil {
			deleteErr = errors.Join(deleteErr, statErr)
		}
	}
	journalCtx, cancel := m.cleanupContext(ctx)
	defer cancel()
	var result ReconcileResult
	journalErr := m.store.Write(journalCtx, func(ctx context.Context, tx *database.Tx) error {
		current, err := store.QueryFoundryAttachments().ForUpdate().RequireFind(ctx, tx, id)
		if err != nil {
			return err
		}
		if _, err := m.validateRow(current); err != nil {
			return err
		}
		if current.State == string(Cleaned) {
			result = status(current)
			deleteErr = nil
			return nil
		}
		if current.State != string(CleanupPending) || current.ETag != row.ETag || current.ObjectVersion != row.ObjectVersion {
			return invalid()
		}
		now, err := m.store.Now()
		if err != nil {
			return err
		}
		attempts := current.Attempts
		if attempts < math.MaxUint32 {
			attempts++
		}
		draft := store.FileDraft{}.SetAttempts(attempts).SetUpdatedAt(now)
		if deleteErr == nil {
			draft = draft.SetState(string(Cleaned)).SetLastFailure("")
		} else {
			draft = draft.SetLastFailure("storage_delete_failed")
		}
		current, err = store.QueryFoundryAttachments().Update(ctx, tx, id, draft)
		if err != nil {
			return err
		}
		result = status(current)
		return nil
	})
	if journalErr != nil {
		return status(row), errors.Join(deleteErr, journalErr)
	}
	return result, deleteErr
}

// ReconcilePending is an explicit bounded sweep, suitable for an ordinary
// scheduled job. It retries Cleanup and abandons old Stored intents after the
// configured grace. It never reclaims Writing/Uncertain based on age alone.
func (m *Manager) ReconcilePending(ctx context.Context, limit int) ([]ReconcileResult, error) {
	if err := m.Validate(); err != nil {
		return nil, err
	}
	if limit < 1 || limit > 1000 {
		return nil, invalid()
	}
	var result []ReconcileResult
	err := m.calls.Run(ctx, "attachment reconciliation sweep", func(ctx context.Context) error {
		now, err := m.store.Now()
		if err != nil {
			return err
		}
		cutoff, err := temporal.NewDateTime(now.UTC().Add(-m.config.StoredGrace))
		if err != nil {
			return err
		}
		var ids []model.ID[store.File]
		m.mu.Lock()
		active := make(map[model.ID[store.File]]bool, len(m.writing))
		for id := range m.writing {
			active[id] = true
		}
		m.mu.Unlock()
		err = m.store.Read(ctx, func(ctx context.Context, tx *database.Tx) error {
			f := store.FileFields()
			q := store.QueryFoundryAttachments().Where(query.Or(f.State.Eq(string(CleanupPending)), query.And(f.State.Eq(string(Stored)), f.UpdatedAt.Lte(cutoff)))).OrderBy(f.UpdatedAt.Asc(), f.ID.Asc()).Limit(limit + len(active))
			rows, err := query.SelectValue(q, f.ID.Value()).All(ctx, tx)
			if err != nil {
				return err
			}
			for _, id := range rows {
				if !active[id] {
					ids = append(ids, id)
					if len(ids) == limit {
						break
					}
				}
			}
			return nil
		})
		if err != nil {
			return err
		}
		var failures error
		for _, id := range ids {
			if err := ctx.Err(); err != nil {
				return errors.Join(failures, err)
			}
			state, err := m.reconcile(ctx, id, true)
			result = append(result, state)
			failures = errors.Join(failures, err)
		}
		return failures
	})
	return result, err
}

// Settle abandons an unresolved upload after an explicit operator assertion.
// It never publishes attachment ownership. A matching object is read through a
// pinned validator and checksum-verified before being scheduled for cleanup.
func (m *Manager) Settle(ctx context.Context, id OperationID, settlement Settlement, queue *Queue) (ReconcileResult, error) {
	if err := m.Validate(); err != nil {
		return ReconcileResult{}, err
	}
	if id.IsZero() || settlement != WriterStoppedAndStorageSettled {
		return ReconcileResult{}, invalid()
	}
	if queue != nil {
		if err := queue.Validate(); err != nil {
			return ReconcileResult{}, err
		}
	}
	var result ReconcileResult
	err := m.calls.Run(ctx, "attachment upload settlement", func(ctx context.Context) error {
		target := fileID(id)
		if m.isWriting(target) {
			return fault.New(fault.Conflict, "upload writer is still running")
		}
		var row store.File
		err := m.store.Write(ctx, func(ctx context.Context, tx *database.Tx) error {
			var err error
			row, err = store.QueryFoundryAttachments().ForUpdate().RequireFind(ctx, tx, target)
			if err != nil {
				return err
			}
			if _, err := m.validateRow(row); err != nil {
				return err
			}
			if row.State != string(Writing) && row.State != string(Uncertain) {
				return fault.New(fault.Conflict, "upload does not require settlement")
			}
			now, err := m.store.Now()
			if err != nil {
				return err
			}
			row, err = store.QueryFoundryAttachments().Update(ctx, tx, target, store.FileDraft{}.SetState(string(Uncertain)).SetWriterID(m.writer).SetLastFailure("operator_settlement").SetUpdatedAt(now))
			return err
		})
		if err != nil {
			return err
		}
		result = status(row)
		disk, err := m.disks.Disk(storage.DiskID(row.Disk))
		if err != nil {
			return err
		}
		key, err := storage.ParseKey(row.ObjectKey)
		if err != nil {
			return err
		}
		info, statErr := disk.Stat(ctx, key, storage.ReadOptions{})
		absent := errorgraph.Is(statErr, storage.NotFound)
		if statErr != nil && !absent {
			return statErr
		}
		if !absent {
			if info.ETag == "" || info.Size != row.Size {
				return storage.Failure(storage.IntegrityFailed, storage.StatOperation, storage.NotApplicable, nil)
			}
			data, _, err := disk.ReadBytes(ctx, key, MaxUploadBytes, storage.ReadOptions{IfMatch: info.ETag, Version: info.Version})
			if err != nil {
				return err
			}
			sum := storage.SHA256(sha256.Sum256(data))
			if sum.String() != row.Digest || int64(len(data)) != row.Size {
				return storage.Failure(storage.IntegrityFailed, storage.StatOperation, storage.NotApplicable, nil)
			}
		}
		err = m.store.Write(ctx, func(ctx context.Context, tx *database.Tx) error {
			current, err := store.QueryFoundryAttachments().ForUpdate().RequireFind(ctx, tx, target)
			if err != nil {
				return err
			}
			if current.State != string(Uncertain) || current.WriterID != m.writer {
				return fault.New(fault.Conflict, "upload settlement changed concurrently")
			}
			now, err := m.store.Now()
			if err != nil {
				return err
			}
			draft := store.FileDraft{}.SetUpdatedAt(now).SetLastFailure("")
			if absent {
				draft = draft.SetState(string(Cleaned))
			} else {
				draft = draft.SetState(string(CleanupPending)).SetETag(string(info.ETag)).SetObjectVersion(string(info.Version))
			}
			current, err = store.QueryFoundryAttachments().Update(ctx, tx, target, draft)
			if err != nil {
				return err
			}
			if !absent {
				if err := queue.enqueue(ctx, tx, id); err != nil {
					return err
				}
			}
			result = status(current)
			return nil
		})
		if err != nil {
			result = status(row)
			return err
		}
		if absent {
			return nil
		}
		result, err = m.reconcile(ctx, target, false)
		return err
	})
	return result, err
}
