package attachments

import (
	"context"
	"crypto/sha256"
	"errors"
	"math"
	"time"

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
	// Derived variants are removed first; the original stays pending (and is
	// retried) until every variant is confirmed deleted.
	deleteErr := m.cleanupVariantsOf(ctx, id)
	if deleteErr == nil {
		deleteErr = disk.Delete(ctx, key, cleanupDelete(row))
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

// cleanupDelete selects exactly the pinned object. A retained version ID is
// immutable, so it is deleted by version alone: that removes the version (no
// delete marker is left on a versioned bucket) and needs no ETag condition,
// which providers such as AWS cannot combine with a version. Without a version
// the ETag condition preserves a concurrent replacement.
func cleanupDelete(row store.File) storage.DeleteOptions {
	return pinnedDelete(row.ObjectVersion, row.ETag)
}

// pinnedDelete is shared by originals and variants.
func pinnedDelete(version, etag string) storage.DeleteOptions {
	if version != "" {
		return storage.DeleteOptions{Version: storage.VersionID(version)}
	}
	return storage.DeleteOptions{IfMatch: storage.ETag(etag)}
}

// settleCutoff is the newest update time an unresolved write may have before
// automatic settlement inspects it: the longest registered disk operation
// Timeout (after which the writer's request context ended) plus SettleAfter
// (the provider's bound on completing a request it already received).
func (m *Manager) settleCutoff(now temporal.DateTime) (temporal.DateTime, error) {
	var longest time.Duration
	for _, collection := range m.collections {
		disk, err := collection.policy.Disk.Resolve(m.disks)
		if err != nil {
			return temporal.DateTime{}, err
		}
		longest = max(longest, disk.Config().Timeout)
	}
	return temporal.NewDateTime(now.UTC().Add(-(longest + m.config.SettleAfter)))
}

// ReconcilePending is an explicit bounded sweep, suitable for an ordinary
// scheduled job. It retries Cleanup and abandons old Stored intents after the
// configured grace. Writing/Uncertain intents older than the settlement cutoff
// are inspected with Stat and settled only when storage is unambiguous: absent,
// or present with this intent's exact size and full checksum. Anything else
// stays Uncertain for operator Settle. Locally active writers are excluded.
// The same sweep retries variant cleanup and settles aged variant intents.
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
		settleCutoff, err := m.settleCutoff(now)
		if err != nil {
			return err
		}
		var rows []store.FileIndex
		m.mu.Lock()
		active := make(map[model.ID[store.File]]bool, len(m.writing))
		for id := range m.writing {
			active[id] = true
		}
		m.mu.Unlock()
		err = m.store.Read(ctx, func(ctx context.Context, tx *database.Tx) error {
			f := store.FileFields()
			q := store.QueryFoundryAttachments().Where(query.Or(f.State.Eq(string(CleanupPending)), query.And(f.State.Eq(string(Stored)), f.UpdatedAt.Lte(cutoff)), query.And(f.State.In(string(Writing), string(Uncertain)), f.UpdatedAt.Lte(settleCutoff)))).OrderBy(f.UpdatedAt.Asc(), f.ID.Asc()).Limit(limit + len(active))
			found, err := store.Index(q).All(ctx, tx)
			if err != nil {
				return err
			}
			for _, row := range found {
				if !active[row.ID] {
					rows = append(rows, row)
					if len(rows) == limit {
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
		for _, row := range rows {
			if err := ctx.Err(); err != nil {
				return errors.Join(failures, err)
			}
			var state ReconcileResult
			var err error
			if row.State == string(Writing) || row.State == string(Uncertain) {
				state, err = m.settle(ctx, row.ID, automaticSettlement(settleCutoff), nil)
			} else {
				state, err = m.reconcile(ctx, row.ID, true)
			}
			result = append(result, state)
			failures = errors.Join(failures, err)
		}
		// Variant journal rows share the same bounded sweep and cutoff.
		return errors.Join(failures, m.sweepVariants(ctx, settleCutoff, limit))
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
		var err error
		result, err = m.settle(ctx, fileID(id), operatorSettlement(), queue)
		return err
	})
	return result, err
}

// settlementPolicy distinguishes an operator assertion from the bounded
// automatic path, which requires an aged intent and unambiguous storage.
type settlementPolicy struct {
	automatic bool
	cutoff    temporal.DateTime
}

func operatorSettlement() settlementPolicy { return settlementPolicy{} }
func automaticSettlement(cutoff temporal.DateTime) settlementPolicy {
	return settlementPolicy{automatic: true, cutoff: cutoff}
}

func (m *Manager) settle(ctx context.Context, target model.ID[store.File], policy settlementPolicy, queue *Queue) (ReconcileResult, error) {
	if m.isWriting(target) {
		return ReconcileResult{}, fault.New(fault.Conflict, "upload writer is still running")
	}
	marker := "operator_settlement"
	if policy.automatic {
		marker = "automatic_settlement"
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
		if policy.automatic && row.UpdatedAt.UTC().After(policy.cutoff.UTC()) {
			return fault.New(fault.Conflict, "upload is too recent for automatic settlement")
		}
		now, err := m.store.Now()
		if err != nil {
			return err
		}
		// Fence the original writer: it can no longer pin or record this intent.
		row, err = store.QueryFoundryAttachments().Update(ctx, tx, target, store.FileDraft{}.SetState(string(Uncertain)).SetWriterID(m.writer).SetLastFailure(marker).SetUpdatedAt(now))
		return err
	})
	if err != nil {
		return status(row), err
	}
	result := status(row)
	disk, err := m.disks.Disk(storage.DiskID(row.Disk))
	if err != nil {
		return result, err
	}
	key, err := storage.ParseKey(row.ObjectKey)
	if err != nil {
		return result, err
	}
	info, statErr := disk.Stat(ctx, key, storage.ReadOptions{})
	absent := errorgraph.Is(statErr, storage.NotFound)
	if statErr != nil && !absent {
		return result, statErr
	}
	if !absent {
		verified, err := m.matchesIntent(ctx, disk, key, row, info, policy.automatic)
		if err != nil {
			return result, err
		}
		if !verified {
			// Ambiguous storage stays Uncertain for an operator; never guess.
			return m.markAmbiguous(ctx, target, result)
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
			if err := queue.enqueue(ctx, tx, operationID(target)); err != nil {
				return err
			}
		}
		result = status(current)
		return nil
	})
	if err != nil {
		return status(row), err
	}
	if absent {
		return result, nil
	}
	return m.reconcile(ctx, target, false)
}

// matchesIntent proves a present object is exactly this intent's upload. The
// automatic path accepts the provider's full SHA-256 metadata when present and
// otherwise reads the pinned bytes, like an operator settlement. A different
// size or digest is ambiguous (false), never deleted.
func (m *Manager) matchesIntent(ctx context.Context, disk *storage.Disk, key storage.ObjectKey, row store.File, info storage.ObjectInfo, automatic bool) (bool, error) {
	if info.ETag == "" || info.Size != row.Size {
		if automatic {
			return false, nil
		}
		return false, storage.Failure(storage.IntegrityFailed, storage.StatOperation, storage.NotApplicable, nil)
	}
	if checksum, ok := info.Checksum.Get(); ok && automatic {
		return checksum.String() == row.Digest, nil
	}
	data, _, err := disk.ReadBytes(ctx, key, MaxUploadBytes, storage.ReadOptions{IfMatch: info.ETag, Version: info.Version})
	if err != nil {
		return false, err
	}
	sum := storage.SHA256(sha256.Sum256(data))
	if sum.String() != row.Digest || int64(len(data)) != row.Size {
		if automatic {
			return false, nil
		}
		return false, storage.Failure(storage.IntegrityFailed, storage.StatOperation, storage.NotApplicable, nil)
	}
	return true, nil
}
func (m *Manager) markAmbiguous(ctx context.Context, target model.ID[store.File], fallback ReconcileResult) (ReconcileResult, error) {
	result := fallback
	err := m.store.Write(ctx, func(ctx context.Context, tx *database.Tx) error {
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
		current, err = store.QueryFoundryAttachments().Update(ctx, tx, target, store.FileDraft{}.SetLastFailure("settlement_ambiguous").SetUpdatedAt(now))
		if err != nil {
			return err
		}
		result = status(current)
		return nil
	})
	return result, err
}
