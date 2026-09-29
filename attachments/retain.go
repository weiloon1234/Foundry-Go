package attachments

import (
	"context"
	"fmt"

	"github.com/weiloon1234/Foundry-Go/database"
	store "github.com/weiloon1234/Foundry-Go/internal/attachmentstore"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/storage"
	"github.com/weiloon1234/Foundry-Go/value"
)

// RetainedFile transfers cleanup responsibility to the application. Its exact
// storage pins are explicit; automatic attachment maintenance never deletes it.
type RetainedFile struct {
	Operation OperationID
	Disk      storage.DiskID
	Key       storage.ObjectKey
	ETag      storage.ETag
	Version   storage.VersionID
}

func (RetainedFile) Format(s fmt.State, _ rune)   { _, _ = s.Write([]byte("retained attachment object")) }
func (RetainedFile) MarshalJSON() ([]byte, error) { return nil, invalid() }

type RetainResult struct {
	Operation   OperationID
	Publication Publication
	File        value.Optional[RetainedFile]
}

func retainedFile(row store.File) (RetainedFile, error) {
	if row.State != string(Retained) {
		return RetainedFile{}, invalid()
	}
	key, err := storage.ParseKey(row.ObjectKey)
	if err != nil {
		return RetainedFile{}, err
	}
	return RetainedFile{Operation: operationID(row.ID), Disk: storage.DiskID(row.Disk), Key: key, ETag: storage.ETag(row.ETag), Version: storage.VersionID(row.ObjectVersion)}, nil
}

// DetachKeepFile removes collection membership without deleting storage. The
// durable retained state survives owner deletion and ordinary orphan pruning.
func (c Collection[M, K]) DetachKeepFile(ctx context.Context, m *Manager, owner model.Reference[M, K], id ID[M]) (RetainResult, error) {
	if err := c.check(m); err != nil {
		return RetainResult{}, err
	}
	if id.IsZero() {
		return RetainResult{}, invalid()
	}
	result := RetainResult{Operation: model.IDFromBytes[Operation](id.Bytes())}
	err := m.calls.Run(ctx, "retain detached attachment", func(ctx context.Context) error {
		locale, err := c.localeName(ctx, m)
		if err != nil {
			return err
		}
		var retained RetainedFile
		err = m.store.Write(ctx, func(ctx context.Context, tx *database.Tx) error {
			subject, err := c.definition.owner.Lock(ctx, tx, m.store.Registry(), owner)
			if err != nil {
				return err
			}
			f := store.FileFields()
			row, err := store.QueryFoundryAttachments().Where(f.Scope.Eq(subject.Scope), f.SubjectKey.Eq(subject.Key), f.Collection.Eq(string(c.Name())), f.Locale.Eq(locale), f.State.Eq(string(Ready))).RequireFind(ctx, tx, model.IDFromBytes[store.File](id.Bytes()))
			if err != nil {
				return err
			}
			if _, err := c.attachment(m, row); err != nil {
				return err
			}
			now, err := m.store.Now()
			if err != nil {
				return err
			}
			row, err = store.QueryFoundryAttachments().Update(ctx, tx, row.ID, store.FileDraft{}.SetState(string(Retained)).SetUpdatedAt(now))
			if err != nil {
				return err
			}
			// The retained original is handed over; its derived variants are not.
			if _, err := m.retireVariants(ctx, tx, row.ID); err != nil {
				return err
			}
			retained, err = retainedFile(row)
			return err
		})
		switch transactionOutcome(err) {
		case database.Committed:
			result.Publication = Published
			result.File = value.Set(retained)
			if err == nil {
				cleanupCtx, cancel := m.cleanupContext(ctx)
				defer cancel()
				// A failed deletion stays in cleanup for ReconcilePending.
				err = m.cleanupVariantsOf(cleanupCtx, model.IDFromBytes[store.File](id.Bytes()))
			}
		case database.Unknown:
			result.Publication = PublicationUnknown
		}
		return err
	})
	return result, err
}

// RetainedObject is an administrative recovery lookup after an uncertain
// DetachKeepFile commit. It grants no access to ready or unsettled upload pins.
func (m *Manager) RetainedObject(ctx context.Context, id OperationID) (RetainedFile, error) {
	if err := m.Validate(); err != nil {
		return RetainedFile{}, err
	}
	if id.IsZero() {
		return RetainedFile{}, invalid()
	}
	var result RetainedFile
	err := m.reads.Run(ctx, "retained attachment lookup", func(ctx context.Context) error {
		return m.store.Read(ctx, func(ctx context.Context, tx *database.Tx) error {
			row, err := store.QueryFoundryAttachments().RequireFind(ctx, tx, fileID(id))
			if err != nil {
				return err
			}
			if _, err := m.validateRow(row); err != nil {
				return err
			}
			result, err = retainedFile(row)
			return err
		})
	})
	if err != nil {
		return RetainedFile{}, err
	}
	return result, nil
}
