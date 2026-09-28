package attachments

import (
	"context"
	"time"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/extensions"
	store "github.com/weiloon1234/Foundry-Go/internal/attachmentstore"
	"github.com/weiloon1234/Foundry-Go/storage"
	"github.com/weiloon1234/Foundry-Go/temporal"
)

// Inspect is an administrative, read-only lookup of a durable upload operation.
// Ready confirms publication; cleanup/cleaned confirms retirement. A writing or
// uncertain state cannot establish that a remote storage request has settled.
func (m *Manager) Inspect(ctx context.Context, id OperationID) (ReconcileResult, error) {
	if err := m.Validate(); err != nil {
		return ReconcileResult{}, err
	}
	if id.IsZero() {
		return ReconcileResult{}, invalid()
	}
	var result ReconcileResult
	err := m.calls.Run(ctx, "attachment intent inspection", func(ctx context.Context) error {
		return m.store.Read(ctx, func(ctx context.Context, tx *database.Tx) error {
			row, err := store.QueryFoundryAttachments().RequireFind(ctx, tx, fileID(id))
			if err != nil {
				return err
			}
			if _, err := m.validateRow(row); err != nil {
				return err
			}
			result = status(row)
			return nil
		})
	})
	if err != nil {
		return ReconcileResult{}, err
	}
	return result, nil
}

// StorageCursor preserves the disk/owner/age policy and first-page cutoff.
// Listings are not transactional snapshots. Copy only a returned Next cursor.
type StorageCursor struct {
	disk       storage.DiskID
	scope      string
	minimumAge time.Duration
	cutoff     temporal.DateTime
	next       storage.Cursor
}

func (c StorageCursor) IsZero() bool { return c.disk == "" }

type StorageInspection struct {
	Disk       storage.Declaration
	Owner      extensions.OwnerName
	MinimumAge time.Duration
	Limit      int
	Cursor     StorageCursor
}
type StorageOrphanPage struct {
	Candidates []storage.ObjectInfo
	Scanned    int
	Next       StorageCursor
}

// InspectStorage lists one bounded page under this owner's exact framework
// prefix on a registered collection disk, then checks those keys in one query.
// Every journal state protects its object, including unsettled writes. Untracked
// candidates are inspection results only: their age never authorizes deletion.
func (m *Manager) InspectStorage(ctx context.Context, options StorageInspection) (StorageOrphanPage, error) {
	if err := m.Validate(); err != nil {
		return StorageOrphanPage{}, err
	}
	if options.Disk.Validate() != nil || options.MinimumAge < time.Minute || options.MinimumAge > 365*24*time.Hour || options.Limit < 1 || options.Limit > storage.MaxPageSize {
		return StorageOrphanPage{}, invalid()
	}
	scope, err := m.store.Registry().Scope(options.Owner)
	if err != nil {
		return StorageOrphanPage{}, err
	}
	registered := false
	for _, collection := range m.collections {
		if collection.policy.Disk.ID() == options.Disk.ID() {
			registered = true
			break
		}
	}
	if !registered {
		return StorageOrphanPage{}, invalid()
	}
	cursor := options.Cursor
	if !cursor.IsZero() && (cursor.disk != options.Disk.ID() || cursor.scope != scope || cursor.minimumAge != options.MinimumAge || cursor.next.IsZero()) {
		return StorageOrphanPage{}, invalid()
	}
	var result StorageOrphanPage
	err = m.calls.Run(ctx, "attachment storage inspection", func(ctx context.Context) error {
		disk, err := options.Disk.Resolve(m.disks)
		if err != nil {
			return err
		}
		prefix, err := storage.ParsePrefix(objectPrefix + scope + "/")
		if err != nil {
			return err
		}
		if cursor.IsZero() {
			now, err := m.store.Now()
			if err != nil {
				return err
			}
			cutoff, err := temporal.NewDateTime(now.UTC().Add(-options.MinimumAge))
			if err != nil {
				return err
			}
			cursor = StorageCursor{disk: options.Disk.ID(), scope: scope, minimumAge: options.MinimumAge, cutoff: cutoff}
		}
		page, err := disk.List(ctx, storage.ListOptions{Prefix: prefix, Limit: options.Limit, Cursor: cursor.next})
		if err != nil {
			return err
		}
		result.Scanned = len(page.Objects)
		keys := make([]string, len(page.Objects))
		for i, object := range page.Objects {
			keys[i] = object.Key.String()
		}
		referenced := make(map[string]bool, len(keys))
		if len(keys) > 0 {
			if err := m.store.Read(ctx, func(ctx context.Context, tx *database.Tx) error {
				f := store.FileFields()
				rows, err := query.SelectValue(store.QueryFoundryAttachments().Where(f.Disk.Eq(string(options.Disk.ID())), f.ObjectKey.In(keys...)).Limit(storage.MaxPageSize+1), f.ObjectKey.Value()).All(ctx, tx)
				if err != nil {
					return err
				}
				for _, key := range rows {
					referenced[key] = true
				}
				return nil
			}); err != nil {
				return err
			}
		}
		for _, object := range page.Objects {
			if !referenced[object.Key.String()] && !object.Modified.UTC().After(cursor.cutoff.UTC()) {
				result.Candidates = append(result.Candidates, object)
			}
		}
		if !page.Next.IsZero() {
			cursor.next = page.Next
			result.Next = cursor
		}
		return nil
	})
	if err != nil {
		return StorageOrphanPage{}, err
	}
	return result, nil
}
