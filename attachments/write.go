package attachments

import (
	"context"
	"errors"
	"math"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	store "github.com/weiloon1234/Foundry-Go/internal/attachmentstore"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/errorgraph"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/storage"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Add appends to a multiple collection and replaces a single collection.
func (c Collection[M, K]) Add(ctx context.Context, m *Manager, owner model.Reference[M, K], upload Upload) (Result[M, K], error) {
	return c.write(ctx, m, owner, upload, false)
}

// Replace atomically replaces the complete collection membership. Old objects
// are eligible for cleanup only after this ownership transaction commits.
func (c Collection[M, K]) Replace(ctx context.Context, m *Manager, owner model.Reference[M, K], upload Upload) (Result[M, K], error) {
	return c.write(ctx, m, owner, upload, true)
}
func (c Collection[M, K]) write(ctx context.Context, m *Manager, owner model.Reference[M, K], upload Upload, replace bool) (Result[M, K], error) {
	if err := c.check(m); err != nil {
		return Result[M, K]{}, err
	}
	var result Result[M, K]
	err := m.calls.Run(ctx, "attachment upload", func(ctx context.Context) error {
		locale, err := c.localeName(ctx, m)
		if err != nil {
			return err
		}
		candidate, err := prepare(ctx, m, c.definition.policy, upload)
		if err != nil {
			return err
		}
		hookContext := BeforeContext[M, K]{Owner: owner, Collection: c.Name(), Locale: c.locale, Upload: candidate.info}
		for _, hook := range c.definition.hooks {
			if hook.Before != nil {
				if err := callback.Isolated("attachment before-store hook", func() error { return hook.Before(ctx, hookContext) }); err != nil {
					return err
				}
			}
		}
		id, err := model.NewID[store.File]()
		if err != nil {
			return err
		}
		result.Operation = operationID(id)
		release := m.markWriting(id)
		defer release()
		row, err := c.stage(ctx, m, id, owner, locale, candidate)
		if err != nil {
			return err
		}
		disk, err := m.disks.Disk(storage.DiskID(row.Disk))
		if err != nil {
			return err
		}
		key, err := storage.ParseKey(row.ObjectKey)
		if err != nil {
			return err
		}
		stored, putErr := disk.Put(ctx, key, candidate.open(), storage.PutOptions{ContentType: candidate.info.MediaType, Size: value.Set(candidate.info.Size), Checksum: value.Set(candidate.digest), Condition: storage.IfAbsent()})
		if putErr != nil {
			return errors.Join(putErr, m.recordPutFailure(ctx, id, putErr))
		}
		if stored.Object.ETag == "" {
			cause := storage.Failure(storage.IntegrityFailed, storage.PutOperation, storage.Applied, nil)
			return errors.Join(cause, m.recordPutFailure(ctx, id, cause))
		}
		pinCtx, pinCancel := m.cleanupContext(ctx)
		pinErr := m.pinStored(pinCtx, id, stored.Object)
		pinCancel()
		if pinErr != nil {
			return pinErr
		}
		attachment, old, err := c.publish(ctx, m, id, owner, replace || c.definition.policy.Cardinality == Single)
		outcome := transactionOutcome(err)
		if err != nil && outcome != database.Committed {
			if outcome == database.Unknown {
				result.Publication = PublicationUnknown
				return err
			}
			cleanupCtx, cancel := m.cleanupContext(ctx)
			defer cancel()
			journalErr := m.scheduleCleanup(cleanupCtx, id, c.queue, "publication_failed")
			if journalErr != nil {
				result.PendingCleanup = append(result.PendingCleanup, operationID(id))
				return errors.Join(err, journalErr)
			}
			status, cleanupErr := m.reconcile(cleanupCtx, id, false)
			if status.State != Cleaned {
				result.PendingCleanup = append(result.PendingCleanup, operationID(id))
			}
			return errors.Join(err, cleanupErr)
		}
		result.Publication = Published
		result.Attachment = value.Set(attachment)
		cleanupCtx, cancel := m.cleanupContext(ctx)
		defer cancel()
		cleanupErr := err
		pending, nextErr := m.cleanupMany(cleanupCtx, old)
		result.PendingCleanup = append(result.PendingCleanup, pending...)
		cleanupErr = errors.Join(cleanupErr, nextErr)
		return cleanupErr
	})
	return result, err
}
func transactionOutcome(err error) database.Outcome {
	if err == nil {
		return database.Committed
	}
	outcome := database.Unknown
	if inspectionErr := callback.Isolated("attachment transaction outcome", func() error {
		found := false
		complete := errorgraph.Walk(err, func(current error) bool {
			detail, matched := errorgraph.AsShallow[*database.Error](current)
			if matched {
				found = true
				if detail != nil {
					outcome = detail.Outcome()
				}
			}
			return !matched
		})
		if complete && !found {
			outcome = database.NoCommit
		}
		return nil
	}); inspectionErr != nil {
		return database.Unknown
	}
	return outcome
}
func (m *Manager) cleanupContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), m.config.CleanupTimeout)
}

func (c Collection[M, K]) stage(ctx context.Context, m *Manager, id model.ID[store.File], owner model.Reference[M, K], locale string, candidate prepared) (store.File, error) {
	var result store.File
	err := m.store.Write(ctx, func(ctx context.Context, tx *database.Tx) error {
		subject, err := c.definition.owner.Lock(ctx, tx, m.store.Registry(), owner)
		if err != nil {
			return err
		}
		key, err := objectKey(subject.Scope, id)
		if err != nil {
			return err
		}
		f := store.FileFields()
		count, err := store.QueryFoundryAttachments().Where(f.Scope.Eq(subject.Scope), f.SubjectKey.Eq(subject.Key), f.State.Ne(string(Cleaned)), f.State.Ne(string(Retained))).Count(ctx, tx)
		if err != nil {
			return err
		}
		if count >= MaxOwnerIntents {
			return fault.New(fault.Conflict, "attachment owner has too many unresolved intents")
		}
		now, err := m.store.Now()
		if err != nil {
			return err
		}
		result, err = store.QueryFoundryAttachments().Create(ctx, tx, store.FileDraft{}.SetID(id).SetOwner(string(c.definition.owner.Name())).SetScope(subject.Scope).SetSubjectKey(subject.Key).SetIdentity(subject.Identity).SetCollection(string(c.Name())).SetLocale(locale).SetSingle(c.definition.policy.Cardinality == Single).SetDisk(string(c.definition.policy.Disk.ID())).SetObjectKey(key.String()).SetOriginalName(candidate.info.OriginalName).SetContentType(string(candidate.info.MediaType)).SetSize(candidate.info.Size).SetDigest(candidate.digest.String()).SetETag("").SetObjectVersion("").SetWidth(int32(candidate.info.Width)).SetHeight(int32(candidate.info.Height)).SetProperties(candidate.properties).SetSortOrder(0).SetState(string(Writing)).SetWriterID(m.writer).SetAttempts(0).SetLastFailure("").SetCreatedAt(now).SetUpdatedAt(now))
		return err
	})
	if err != nil {
		return store.File{}, err
	}
	return result, nil
}
func (m *Manager) pinStored(ctx context.Context, id model.ID[store.File], object storage.ObjectInfo) error {
	return m.store.Write(ctx, func(ctx context.Context, tx *database.Tx) error {
		row, err := store.QueryFoundryAttachments().ForUpdate().RequireFind(ctx, tx, id)
		if err != nil {
			return err
		}
		if _, err := m.validateRow(row); err != nil {
			return err
		}
		if row.State != string(Writing) || row.WriterID != m.writer || row.ObjectKey != object.Key.String() || row.Size != object.Size {
			return invalid()
		}
		checksum, ok := object.Checksum.Get()
		if !ok || checksum.String() != row.Digest {
			return invalid()
		}
		now, err := m.store.Now()
		if err != nil {
			return err
		}
		_, err = store.QueryFoundryAttachments().Update(ctx, tx, id, store.FileDraft{}.SetETag(string(object.ETag)).SetObjectVersion(string(object.Version)).SetState(string(Stored)).SetUpdatedAt(now))
		return err
	})
}
func (m *Manager) recordPutFailure(ctx context.Context, id model.ID[store.File], cause error) error {
	// Only an explicit Unchanged storage result proves this invocation published
	// nothing. Unknown, Applied and extension failures remain visible for settlement.
	next := Uncertain
	if detail, ok := cause.(*storage.Error); ok && detail.Outcome() == storage.Unchanged {
		next = Cleaned
	}
	cleanupCtx, cancel := m.cleanupContext(ctx)
	defer cancel()
	return m.store.Write(cleanupCtx, func(ctx context.Context, tx *database.Tx) error {
		row, err := store.QueryFoundryAttachments().ForUpdate().RequireFind(ctx, tx, id)
		if err != nil {
			return err
		}
		if row.WriterID != m.writer || row.State != string(Writing) {
			return invalid()
		}
		if _, err := m.validateRow(row); err != nil {
			return err
		}
		now, err := m.store.Now()
		if err != nil {
			return err
		}
		_, err = store.QueryFoundryAttachments().Update(ctx, tx, id, store.FileDraft{}.SetState(string(next)).SetLastFailure("upload_failed").SetUpdatedAt(now))
		return err
	})
}

func (c Collection[M, K]) publish(ctx context.Context, m *Manager, id model.ID[store.File], owner model.Reference[M, K], replace bool) (Attachment[M, K], []model.ID[store.File], error) {
	var result Attachment[M, K]
	var removed []model.ID[store.File]
	err := m.store.Write(ctx, func(ctx context.Context, tx *database.Tx) error {
		subject, err := c.definition.owner.Lock(ctx, tx, m.store.Registry(), owner)
		if err != nil {
			return err
		}
		row, err := store.QueryFoundryAttachments().ForUpdate().RequireFind(ctx, tx, id)
		if err != nil {
			return err
		}
		if _, err := m.validateRow(row); err != nil {
			return err
		}
		if row.State != string(Stored) || row.WriterID != m.writer || row.Scope != subject.Scope || row.SubjectKey != subject.Key {
			return fault.New(fault.Conflict, "upload is no longer available for publication")
		}
		f := store.FileFields()
		existing, err := store.QueryFoundryAttachments().Where(f.Scope.Eq(subject.Scope), f.SubjectKey.Eq(subject.Key), f.Collection.Eq(row.Collection), f.Locale.Eq(row.Locale), f.State.Eq(string(Ready))).OrderBy(f.SortOrder.Asc(), f.ID.Asc()).Limit(MaxCollectionFiles+1).All(ctx, tx)
		if err != nil {
			return err
		}
		if len(existing) > MaxCollectionFiles || !replace && len(existing) >= c.definition.policy.MaxFiles {
			return fault.New(fault.Conflict, "attachment collection is full")
		}
		position := int32(0)
		if len(existing) > 0 {
			if replace {
				position = existing[0].SortOrder
			} else {
				if existing[len(existing)-1].SortOrder == math.MaxInt32 {
					return fault.New(fault.Conflict, "attachment collection needs reordering")
				}
				position = existing[len(existing)-1].SortOrder + 1
			}
		}
		now, err := m.store.Now()
		if err != nil {
			return err
		}
		if replace {
			for _, previous := range existing {
				if _, err := m.validateRow(previous); err != nil {
					return err
				}
				if _, err := store.QueryFoundryAttachments().Update(ctx, tx, previous.ID, store.FileDraft{}.SetState(string(CleanupPending)).SetUpdatedAt(now)); err != nil {
					return err
				}
				if err := c.queue.enqueue(ctx, tx, operationID(previous.ID)); err != nil {
					return err
				}
				removed = append(removed, previous.ID)
			}
		}
		row, err = store.QueryFoundryAttachments().Update(ctx, tx, id, store.FileDraft{}.SetState(string(Ready)).SetSortOrder(position).SetUpdatedAt(now))
		if err != nil {
			return err
		}
		result, err = c.attachment(m, row)
		if err != nil {
			return err
		}
		for _, hook := range c.definition.hooks {
			if hook.AfterStored != nil {
				if err := callback.Isolated("attachment after-store hook", func() error { return hook.AfterStored(ctx, tx, result) }); err != nil {
					return err
				}
			}
		}
		// A hook may have modified the owner through this transaction. Publication
		// still requires an active owner after all hooks complete.
		if _, err = c.definition.owner.Lock(ctx, tx, m.store.Registry(), owner); err != nil {
			return err
		}
		final, err := store.QueryFoundryAttachments().RequireFind(ctx, tx, id)
		if err != nil {
			return err
		}
		// Hooks can issue SQL in this transaction. Revalidate ownership and the
		// exact acknowledged bytes before claiming a ready attachment.
		if final.Scope != row.Scope || final.SubjectKey != row.SubjectKey || final.Disk != row.Disk || final.ObjectKey != row.ObjectKey || final.ETag != row.ETag || final.ObjectVersion != row.ObjectVersion || final.Digest != row.Digest || final.Size != row.Size || final.WriterID != row.WriterID || final.Single != row.Single || final.ContentType != row.ContentType || final.OriginalName != row.OriginalName || final.Width != row.Width || final.Height != row.Height {
			return invalid()
		}
		result, err = c.attachment(m, final)
		return err
	})
	return result, removed, err
}

func (m *Manager) scheduleCleanup(ctx context.Context, id model.ID[store.File], queue *Queue, failure string) error {
	return m.store.Write(ctx, func(ctx context.Context, tx *database.Tx) error {
		row, err := store.QueryFoundryAttachments().ForUpdate().RequireFind(ctx, tx, id)
		if err != nil {
			return err
		}
		if _, err := m.validateRow(row); err != nil {
			return err
		}
		if row.State == string(CleanupPending) || row.State == string(Cleaned) {
			return nil
		}
		if row.State != string(Stored) {
			return fault.New(fault.Conflict, "upload cannot be scheduled for cleanup")
		}
		now, err := m.store.Now()
		if err != nil {
			return err
		}
		if _, err := store.QueryFoundryAttachments().Update(ctx, tx, id, store.FileDraft{}.SetState(string(CleanupPending)).SetLastFailure(failure).SetUpdatedAt(now)); err != nil {
			return err
		}
		return queue.enqueue(ctx, tx, operationID(id))
	})
}
