package attachments

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	store "github.com/weiloon1234/Foundry-Go/internal/attachmentstore"
	"github.com/weiloon1234/Foundry-Go/internal/attachmenttest"
	"github.com/weiloon1234/Foundry-Go/storage"
)

// RemoveBackingObject is the internal-token-gated seam used by testkit/attachments.
// It deletes only the ready file's pinned original object, preserving its record
// and variants. Applications cannot obtain a valid token outside the module.
func (m *Manager) RemoveBackingObject(ctx context.Context, token attachmenttest.Token, id OperationID) error {
	if !token.Valid() || id.IsZero() {
		return invalid()
	}
	if err := m.Validate(); err != nil {
		return err
	}
	return m.calls.Run(ctx, "remove test attachment object", func(ctx context.Context) error {
		var row store.File
		if err := m.store.Read(ctx, func(ctx context.Context, tx *database.Tx) error {
			var err error
			row, err = orm.Require(ctx, tx, store.Files.Query().Where(orm.KeyEq(store.Files.Key(), fileID(id))))
			if err != nil {
				return err
			}
			if _, err := m.validateRow(row); err != nil {
				return err
			}
			if row.State != string(Ready) {
				return invalid()
			}
			return nil
		}); err != nil {
			return err
		}
		key, err := storage.ParseKey(row.ObjectKey)
		if err != nil {
			return err
		}
		disk, err := m.disks.Disk(storage.DiskID(row.Disk))
		if err != nil {
			return err
		}
		return disk.Delete(ctx, key, pinnedDelete(row.ObjectVersion, row.ETag))
	})
}
