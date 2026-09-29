package attachments

import (
	"context"
	"errors"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/extensions"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/i18n"
	store "github.com/weiloon1234/Foundry-Go/internal/attachmentstore"
	"github.com/weiloon1234/Foundry-Go/internal/extensionmaintenance"
	"github.com/weiloon1234/Foundry-Go/model"
)

type Cursor = extensionmaintenance.Cursor
type Orphan struct {
	Operation  OperationID   `json:"operation"`
	Collection Name          `json:"collection"`
	Locale     i18n.LocaleID `json:"locale"`
	State      State         `json:"state"`
}
type OrphanPage struct {
	Orphans []Orphan
	Scanned int
	Next    Cursor
}

func (m *Manager) InspectOrphans(ctx context.Context, owner extensions.OwnerName, cursor Cursor, limit int) (OrphanPage, error) {
	if err := m.Validate(); err != nil {
		return OrphanPage{}, err
	}
	var result OrphanPage
	err := m.reads.Run(ctx, "attachment orphan inspection", func(ctx context.Context) error {
		page, err := extensionmaintenance.Inspect(ctx, m.store, owner, cursor, limit, m.orphanTable(nil, nil))
		if err != nil {
			return err
		}
		result = OrphanPage{Scanned: page.Scanned, Next: page.Next}
		for _, row := range page.Rows {
			result.Orphans = append(result.Orphans, Orphan{Operation: operationID(row.ID), Collection: Name(row.Collection), Locale: i18n.LocaleID(row.Locale), State: State(row.State)})
		}
		return nil
	})
	if err != nil {
		return OrphanPage{}, err
	}
	return result, nil
}

// PruneOrphans only retires ready/stored orphan rows selected explicitly. It
// preserves soft-deleted owners, and refuses unsettled writers instead of
// treating owner absence as permission to delete a possibly in-flight upload.
func (m *Manager) PruneOrphans(ctx context.Context, owner extensions.OwnerName, ids []OperationID, queue *Queue) (ChangeResult, error) {
	if err := m.Validate(); err != nil {
		return ChangeResult{}, err
	}
	if queue != nil {
		if err := queue.Validate(); err != nil {
			return ChangeResult{}, err
		}
	}
	keys := make([]string, len(ids))
	for i, id := range ids {
		if id.IsZero() {
			return ChangeResult{}, invalid()
		}
		keys[i] = id.String()
	}
	var result ChangeResult
	err := m.calls.Run(ctx, "attachment orphan pruning", func(ctx context.Context) error {
		var scheduled []model.ID[store.File]
		count, err := extensionmaintenance.Prune(ctx, m.store, owner, keys, m.orphanTable(queue, &scheduled))
		outcome := transactionOutcome(err)
		if err != nil && outcome != database.Committed {
			if outcome == database.Unknown {
				result.Publication = PublicationUnknown
			}
			return err
		}
		result.Publication = Published
		result.Affected = count
		cleanupCtx, cancel := m.cleanupContext(ctx)
		defer cancel()
		pending, cleanupErr := m.cleanupMany(cleanupCtx, scheduled)
		result.PendingCleanup = pending
		return errors.Join(err, cleanupErr)
	})
	return result, err
}
func (m *Manager) orphanTable(queue *Queue, scheduled *[]model.ID[store.File]) extensionmaintenance.Table[store.FileIndex] {
	return extensionmaintenance.Table[store.FileIndex]{
		Name: "attachments",
		ValidKey: func(key string) bool {
			id, err := model.ParseID[store.File](key)
			return err == nil && !id.IsZero() && id.String() == key
		},
		Scan: func(ctx context.Context, tx *database.Tx, owner extensions.OwnerName, scope, after string, limit int) ([]store.FileIndex, error) {
			f := store.FileFields()
			q := store.QueryFoundryAttachments().Where(f.Owner.Eq(string(owner)), f.Scope.Eq(scope), f.State.Ne(string(Cleaned)), f.State.Ne(string(Retained))).OrderBy(f.ID.Asc()).Limit(limit)
			if after != "" {
				id, err := model.ParseID[store.File](after)
				if err != nil {
					return nil, err
				}
				// PostgreSQL orders UUID keys bytewise. Reuse the generated field's
				// exact owner/codec metadata at this explicit range boundary.
				ordered := query.OrderedField[store.File, model.ID[store.File]]{ScalarField: f.ID}
				q = q.Where(ordered.Gt(id))
			}
			return store.Index(q).All(ctx, tx)
		},
		Lock: func(ctx context.Context, tx *database.Tx, owner extensions.OwnerName, scope string, keys []string) ([]store.FileIndex, error) {
			ids := make([]model.ID[store.File], len(keys))
			for i, key := range keys {
				id, err := model.ParseID[store.File](key)
				if err != nil {
					return nil, err
				}
				ids[i] = id
			}
			f := store.FileFields()
			return store.Index(store.QueryFoundryAttachments().Where(f.Owner.Eq(string(owner)), f.Scope.Eq(scope), f.ID.In(ids...), f.State.Ne(string(Cleaned)), f.State.Ne(string(Retained))).OrderBy(f.ID.Asc())).ForUpdate().All(ctx, tx)
		},
		Identity: func(_ *extensions.Registry, owner extensions.OwnerName, scope string, row store.FileIndex) (model.Identity, error) {
			if row.Owner != string(owner) || row.Scope != scope {
				return model.Identity{}, invalid()
			}
			if _, err := m.validateIndex(row); err != nil {
				return model.Identity{}, err
			}
			return row.Identity.Decode()
		},
		Key: func(row store.FileIndex) string { return row.ID.String() }, SubjectKey: func(row store.FileIndex) string { return row.SubjectKey },
		Delete: func(ctx context.Context, tx *database.Tx, row store.FileIndex) error {
			if scheduled == nil {
				return invalid()
			}
			if row.State == string(Writing) || row.State == string(Uncertain) {
				return fault.New(fault.Conflict, "orphaned upload requires writer and storage settlement")
			}
			now, err := m.store.Now()
			if err != nil {
				return err
			}
			if _, err := store.QueryFoundryAttachments().Update(ctx, tx, row.ID, store.FileDraft{}.SetState(string(CleanupPending)).SetUpdatedAt(now)); err != nil {
				return err
			}
			if err := queue.enqueue(ctx, tx, operationID(row.ID)); err != nil {
				return err
			}
			*scheduled = append(*scheduled, row.ID)
			return nil
		},
	}
}
