package attachments

import (
	"context"
	"errors"
	"fmt"
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

// write stores the upload, then publishes it in the manager's own transaction.
func (c Collection[M, K]) write(ctx context.Context, m *Manager, owner model.Reference[M, K], upload Upload, replace bool) (Result[M, K], error) {
	if err := c.check(m); err != nil {
		return Result[M, K]{}, err
	}
	var result Result[M, K]
	err := m.calls.Run(ctx, "attachment upload", func(ctx context.Context) error {
		id, release, err := c.storeUpload(ctx, m, owner, upload, true)
		if release != nil {
			defer release()
		}
		if !id.IsZero() {
			result.Operation = operationID(id)
		}
		if err != nil {
			return err
		}
		attachment, old, err := c.publish(ctx, nil, m, id, owner, replace || c.definition.policy.Cardinality == Single)
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
		if len(c.definition.policy.Variants) > 0 {
			// Queued variants were enqueued by the publication transaction.
			// Otherwise generate them now; a failure never unpublishes.
			result.PendingVariants = c.variantQueue != nil
			if c.variantQueue == nil {
				if variantErr := m.generate(ctx, id, false); variantErr != nil {
					result.PendingVariants = true
					cleanupErr = errors.Join(cleanupErr, variantErr)
				} else if refreshed, err := c.withVariants(ctx, m, attachment); err == nil {
					result.Attachment = value.Set(refreshed)
				}
			}
		}
		return cleanupErr
	})
	return result, err
}

// storeUpload reads and checks upload, records its intent and stores its
// object, all in the manager's own transactions, and returns the stored,
// unpublished upload with the writing mark the caller releases. A required
// owner is locked while staging; otherwise an existing owner is locked and a
// missing one, such as a model a later transaction creates, is accepted.
func (c Collection[M, K]) storeUpload(ctx context.Context, m *Manager, owner model.Reference[M, K], upload Upload, ownerRequired bool) (model.ID[store.File], func(), error) {
	locale, err := c.localeName(ctx, m)
	if err != nil {
		return model.ID[store.File]{}, nil, err
	}
	candidate, err := prepare(ctx, m, c.definition.policy, upload)
	if err != nil {
		return model.ID[store.File]{}, nil, err
	}
	hookContext := BeforeContext[M, K]{Owner: owner, Collection: c.Name(), Locale: c.locale, Upload: candidate.info}
	for _, hook := range c.definition.hooks {
		if hook.Before != nil {
			if err := callback.Isolated("attachment before-store hook", func() error { return hook.Before(ctx, hookContext) }); err != nil {
				return model.ID[store.File]{}, nil, err
			}
		}
	}
	id, err := model.NewID[store.File]()
	if err != nil {
		return model.ID[store.File]{}, nil, err
	}
	release := m.markWriting(id)
	row, err := c.stage(ctx, m, id, owner, locale, candidate, ownerRequired)
	if err != nil {
		return id, release, err
	}
	disk, err := m.disks.Disk(storage.DiskID(row.Disk))
	if err != nil {
		return id, release, err
	}
	key, err := storage.ParseKey(row.ObjectKey)
	if err != nil {
		return id, release, err
	}
	stored, putErr := disk.Put(ctx, key, candidate.open(), storage.PutOptions{ContentType: candidate.info.MediaType, Size: value.Set(candidate.info.Size), Checksum: value.Set(candidate.digest), Condition: storage.IfAbsent()})
	if putErr != nil {
		return id, release, errors.Join(putErr, m.recordPutFailure(ctx, id, putErr))
	}
	if stored.Object.ETag == "" {
		cause := storage.Failure(storage.IntegrityFailed, storage.PutOperation, storage.Applied, nil)
		return id, release, errors.Join(cause, m.recordPutFailure(ctx, id, cause))
	}
	pinCtx, pinCancel := m.cleanupContext(ctx)
	defer pinCancel()
	return id, release, m.pinStored(pinCtx, id, stored.Object)
}

// Prepared is an upload read, checked and stored for one owner's collection
// but not yet published. Publish it with ReplaceIn or AddIn inside a
// transaction that begins after Prepare returned, within StoredGrace;
// otherwise ReconcilePending reclaims it. Discard reclaims it at once.
type Prepared[M any, K comparable] struct {
	operation  OperationID
	collection Name
	locale     string
}

// Operation is the durable upload intent, for Manager.Inspect.
func (p Prepared[M, K]) Operation() OperationID { return p.operation }
func (p Prepared[M, K]) IsZero() bool           { return p.operation.IsZero() }
func (Prepared[M, K]) Format(s fmt.State, _ rune) {
	_, _ = s.Write([]byte("prepared attachment upload"))
}

// Prepare reads, checks and stores upload for owner's collection without
// publishing it, in the manager's own transactions. Call it before the
// transaction that publishes it begins, so that transaction needs only its own
// connection and sees the stored upload. An existing owner is locked while the
// upload is recorded, keeping its intent bound exact; owner may also be a model
// the publishing transaction creates, with its key chosen beforehand. On an
// error the returned value names any recorded intent.
func (c Collection[M, K]) Prepare(ctx context.Context, m *Manager, owner model.Reference[M, K], upload Upload) (Prepared[M, K], error) {
	if err := c.check(m); err != nil {
		return Prepared[M, K]{}, err
	}
	var prepared Prepared[M, K]
	err := m.calls.Run(ctx, "attachment upload", func(ctx context.Context) error {
		locale, err := c.localeName(ctx, m)
		if err != nil {
			return err
		}
		id, release, err := c.storeUpload(ctx, m, owner, upload, false)
		if release != nil {
			defer release()
		}
		if !id.IsZero() {
			prepared = Prepared[M, K]{operation: operationID(id), collection: c.Name(), locale: locale}
		}
		return err
	})
	return prepared, err
}

// AddIn publishes a prepared upload into owner's collection inside tx, a
// transaction of the extension store's pool, using only that transaction: the
// owner may be one tx created, and the attachment becomes visible when tx
// commits. A failure, including a rollback, leaves the prepared upload for
// Discard or ReconcilePending. Old-object cleanup and unqueued variant
// generation run after tx commits; their failure is the transaction's
// after-commit error and leaves work for ReconcilePending.
func (c Collection[M, K]) AddIn(ctx context.Context, tx *database.Tx, m *Manager, owner model.Reference[M, K], prepared Prepared[M, K]) (Result[M, K], error) {
	return c.publishPrepared(ctx, tx, m, owner, prepared, false)
}

// ReplaceIn publishes a prepared upload as owner's complete collection
// inside tx; see AddIn.
func (c Collection[M, K]) ReplaceIn(ctx context.Context, tx *database.Tx, m *Manager, owner model.Reference[M, K], prepared Prepared[M, K]) (Result[M, K], error) {
	return c.publishPrepared(ctx, tx, m, owner, prepared, true)
}

func (c Collection[M, K]) publishPrepared(ctx context.Context, tx *database.Tx, m *Manager, owner model.Reference[M, K], prepared Prepared[M, K], replace bool) (Result[M, K], error) {
	if err := c.check(m); err != nil {
		return Result[M, K]{}, err
	}
	if err := m.joinable(tx); err != nil {
		return Result[M, K]{}, err
	}
	result := Result[M, K]{Operation: prepared.operation}
	err := m.calls.Run(ctx, "attachment publication", func(ctx context.Context) error {
		locale, err := c.localeName(ctx, m)
		if err != nil {
			return err
		}
		if prepared.IsZero() || prepared.collection != c.Name() || prepared.locale != locale {
			return fault.New(fault.Invalid, "prepared upload belongs to another collection")
		}
		id := model.IDFromBytes[store.File](prepared.operation.Bytes())
		attachment, old, err := c.publish(ctx, tx, m, id, owner, replace || c.definition.policy.Cardinality == Single)
		if err != nil {
			return err
		}
		result.Publication = Published
		result.Attachment = value.Set(attachment)
		return c.afterCommit(tx, m, id, old, &result)
	})
	return result, err
}

// Discard reclaims a prepared upload that will not be published, instead of
// leaving it to ReconcilePending. A published upload is refused and kept.
func (c Collection[M, K]) Discard(ctx context.Context, m *Manager, prepared Prepared[M, K]) error {
	if err := c.check(m); err != nil {
		return err
	}
	if prepared.IsZero() || prepared.collection != c.Name() {
		return fault.New(fault.Invalid, "prepared upload belongs to another collection")
	}
	return m.calls.Run(ctx, "attachment discard", func(ctx context.Context) error {
		cleanupCtx, cancel := m.cleanupContext(ctx)
		defer cancel()
		id := model.IDFromBytes[store.File](prepared.operation.Bytes())
		if err := m.scheduleCleanup(cleanupCtx, id, c.queue, "discarded"); err != nil {
			return err
		}
		_, err := m.reconcile(cleanupCtx, id, false)
		return err
	})
}

// ownerIntentBound refuses more than MaxOwnerIntents unresolved intents for one
// owner, counting adding intents about to be recorded. Hold the owner lock.
func ownerIntentBound(ctx context.Context, tx *database.Tx, scope, subject string, adding int64) error {
	f := store.FileFields()
	count, err := store.QueryFoundryAttachments().Where(f.Scope.Eq(scope), f.SubjectKey.Eq(subject), f.State.Ne(string(Cleaned)), f.State.Ne(string(Retained))).Count(ctx, tx)
	if err != nil {
		return err
	}
	if count+adding > MaxOwnerIntents {
		return fault.New(fault.Conflict, "attachment owner has too many unresolved intents")
	}
	return nil
}

// joinable accepts a transaction of the extension store's pool, which a
// publication can join.
func (m *Manager) joinable(tx *database.Tx) error {
	if tx == nil || !tx.BelongsTo(m.store.Database()) {
		return invalid()
	}
	return nil
}

// afterCommit schedules the work that follows a publication inside outer. It
// runs only after outer commits: old objects stay owned by outer's pending
// changes until then, and this call must not wait on outer's locks.
func (c Collection[M, K]) afterCommit(outer *database.Tx, m *Manager, id model.ID[store.File], old []model.ID[store.File], result *Result[M, K]) error {
	for _, previous := range old {
		result.PendingCleanup = append(result.PendingCleanup, operationID(previous))
	}
	generate := len(c.definition.policy.Variants) > 0 && c.variantQueue == nil
	result.PendingVariants = len(c.definition.policy.Variants) > 0
	if len(old) == 0 && !generate {
		return nil
	}
	return outer.AfterCommit(func(ctx context.Context) error {
		// The publication is committed: a caller that stops waiting does not
		// cancel this work, which the manager's call timeout and shutdown bound.
		return m.calls.Run(context.WithoutCancel(ctx), "attachment after-commit work", func(ctx context.Context) error {
			cleanupCtx, cancel := m.cleanupContext(ctx)
			defer cancel()
			_, err := m.cleanupMany(cleanupCtx, old)
			if generate {
				err = errors.Join(err, m.generate(ctx, id, false))
			}
			return err
		})
	})
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

// stage records the upload intent in the manager's own transaction, so it
// stays visible for settlement whatever happens next. A publication inside a
// caller's transaction may be for an owner only that transaction can see, so
// the owner is then locked and checked when publishing instead.
func (c Collection[M, K]) stage(ctx context.Context, m *Manager, id model.ID[store.File], owner model.Reference[M, K], locale string, candidate prepared, ownerRequired bool) (store.File, error) {
	var result store.File
	err := m.store.Write(ctx, func(ctx context.Context, tx *database.Tx) error {
		// The owner lock serializes intent counting for one owner. A prepared
		// upload may be for a model its publishing transaction creates later;
		// publication then recounts under that lock.
		subject, err := c.definition.owner.Lock(ctx, tx, m.store.Registry(), owner)
		if !ownerRequired && errors.Is(err, database.NotFound) {
			subject, err = c.definition.owner.Subject(owner)
		}
		if err != nil {
			return err
		}
		key, err := objectKey(subject.Scope, id)
		if err != nil {
			return err
		}
		if err := ownerIntentBound(ctx, tx, subject.Scope, subject.Key, 1); err != nil {
			return err
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

func (c Collection[M, K]) publish(ctx context.Context, outer *database.Tx, m *Manager, id model.ID[store.File], owner model.Reference[M, K], replace bool) (Attachment[M, K], []model.ID[store.File], error) {
	var result Attachment[M, K]
	var removed []model.ID[store.File]
	body := func(ctx context.Context, tx *database.Tx) error {
		subject, err := c.definition.owner.Lock(ctx, tx, m.store.Registry(), owner)
		if err != nil {
			return err
		}
		row, err := store.QueryFoundryAttachments().ForUpdate().RequireFind(ctx, tx, id)
		if outer != nil && errors.Is(err, database.NotFound) {
			return fault.New(fault.Invalid, "prepared upload is not visible to this transaction; prepare it before the transaction begins")
		}
		if err != nil {
			return err
		}
		if _, err := m.validateRow(row); err != nil {
			return err
		}
		if row.State != string(Stored) || row.WriterID != m.writer || row.Scope != subject.Scope || row.SubjectKey != subject.Key || row.Collection != string(c.Name()) {
			return fault.New(fault.Conflict, "upload is no longer available for publication")
		}
		f := store.FileFields()
		if outer != nil {
			// Staging locked only an owner that already existed; recount
			// under this lock, including the upload being published.
			if err := ownerIntentBound(ctx, tx, subject.Scope, subject.Key, 0); err != nil {
				return err
			}
		}
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
		if len(c.definition.policy.Variants) > 0 {
			if err := c.variantQueue.enqueue(ctx, tx, operationID(id)); err != nil {
				return err
			}
		}
		result, err = c.attachment(m, final)
		return err
	}
	var err error
	if outer != nil {
		err = m.store.Join(ctx, outer, body)
	} else {
		err = m.store.Write(ctx, body)
	}
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
