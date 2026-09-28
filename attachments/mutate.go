package attachments

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	store "github.com/weiloon1234/Foundry-Go/internal/attachmentstore"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

type ChangeResult struct {
	Publication    Publication
	Affected       int
	PendingCleanup []OperationID
}

func (c Collection[M, K]) Detach(ctx context.Context, m *Manager, owner model.Reference[M, K], id ID[M]) (ChangeResult, error) {
	if id.IsZero() {
		return ChangeResult{}, invalid()
	}
	return c.detach(ctx, m, owner, value.Set(id))
}
func (c Collection[M, K]) Clear(ctx context.Context, m *Manager, owner model.Reference[M, K]) (ChangeResult, error) {
	return c.detach(ctx, m, owner, value.Optional[ID[M]]{})
}
func (c Collection[M, K]) detach(ctx context.Context, m *Manager, owner model.Reference[M, K], only value.Optional[ID[M]]) (ChangeResult, error) {
	if err := c.check(m); err != nil {
		return ChangeResult{}, err
	}
	var result ChangeResult
	err := m.calls.Run(ctx, "attachment detach", func(ctx context.Context) error {
		locale, err := c.localeName(ctx, m)
		if err != nil {
			return err
		}
		var ids []model.ID[store.File]
		err = m.store.Write(ctx, func(ctx context.Context, tx *database.Tx) error {
			subject, err := c.definition.owner.Lock(ctx, tx, m.store.Registry(), owner)
			if err != nil {
				return err
			}
			f := store.FileFields()
			q := store.QueryFoundryAttachments().Where(f.Scope.Eq(subject.Scope), f.SubjectKey.Eq(subject.Key), f.Collection.Eq(string(c.Name())), f.Locale.Eq(locale), f.State.Eq(string(Ready)))
			if id, ok := only.Get(); ok {
				q = q.Where(f.ID.Eq(model.IDFromBytes[store.File](id.Bytes())))
			}
			rows, err := q.OrderBy(f.ID.Asc()).Limit(MaxCollectionFiles+1).All(ctx, tx)
			if err != nil {
				return err
			}
			if len(rows) > MaxCollectionFiles {
				return invalid()
			}
			now, err := m.store.Now()
			if err != nil {
				return err
			}
			for _, row := range rows {
				if _, err := c.attachment(m, row); err != nil {
					return err
				}
				if _, err := store.QueryFoundryAttachments().Update(ctx, tx, row.ID, store.FileDraft{}.SetState(string(CleanupPending)).SetUpdatedAt(now)); err != nil {
					return err
				}
				if err := c.queue.enqueue(ctx, tx, operationID(row.ID)); err != nil {
					return err
				}
				ids = append(ids, row.ID)
			}
			return nil
		})
		outcome := transactionOutcome(err)
		if err != nil && outcome != database.Committed {
			if outcome == database.Unknown {
				result.Publication = PublicationUnknown
			}
			return err
		}
		result.Publication = Published
		result.Affected = len(ids)
		cleanupCtx, cancel := m.cleanupContext(ctx)
		defer cancel()
		pending, cleanupErr := m.cleanupMany(cleanupCtx, ids)
		result.PendingCleanup = pending
		return errors.Join(err, cleanupErr)
	})
	return result, err
}
func (m *Manager) cleanupMany(ctx context.Context, ids []model.ID[store.File]) ([]OperationID, error) {
	var pending []OperationID
	var failures error
	for i, id := range ids {
		if err := ctx.Err(); err != nil {
			for _, remaining := range ids[i:] {
				pending = append(pending, operationID(remaining))
			}
			return pending, errors.Join(failures, err)
		}
		result, err := m.reconcile(ctx, id, false)
		if result.State != Cleaned {
			pending = append(pending, operationID(id))
		}
		failures = errors.Join(failures, err)
	}
	return pending, failures
}

// Reorder accepts an exact permutation of current ready collection IDs. No
// omission, duplicate or foreign ID can partially change the stored ordering.
func (c Collection[M, K]) Reorder(ctx context.Context, m *Manager, owner model.Reference[M, K], order []ID[M]) ([]Attachment[M, K], error) {
	if err := c.check(m); err != nil {
		return nil, err
	}
	if len(order) > MaxCollectionFiles {
		return nil, invalid()
	}
	ids := make([]model.ID[store.File], len(order))
	seen := make(map[model.ID[store.File]]bool, len(order))
	for i, id := range order {
		if id.IsZero() {
			return nil, invalid()
		}
		ids[i] = model.IDFromBytes[store.File](id.Bytes())
		if seen[ids[i]] {
			return nil, fault.New(fault.Duplicate, "attachment order contains duplicate IDs")
		}
		seen[ids[i]] = true
	}
	var result []Attachment[M, K]
	err := m.calls.Run(ctx, "attachment reorder", func(ctx context.Context) error {
		locale, err := c.localeName(ctx, m)
		if err != nil {
			return err
		}
		return m.store.Write(ctx, func(ctx context.Context, tx *database.Tx) error {
			subject, err := c.definition.owner.Lock(ctx, tx, m.store.Registry(), owner)
			if err != nil {
				return err
			}
			f := store.FileFields()
			rows, err := store.QueryFoundryAttachments().Where(f.Scope.Eq(subject.Scope), f.SubjectKey.Eq(subject.Key), f.Collection.Eq(string(c.Name())), f.Locale.Eq(locale), f.State.Eq(string(Ready))).OrderBy(f.ID.Asc()).Limit(MaxCollectionFiles+1).All(ctx, tx)
			if err != nil {
				return err
			}
			if len(rows) != len(ids) {
				return fault.New(fault.Conflict, "attachment order is not an exact collection permutation")
			}
			for _, row := range rows {
				if !seen[row.ID] {
					return fault.New(fault.Conflict, "attachment order contains a foreign ID")
				}
				if _, err := c.attachment(m, row); err != nil {
					return err
				}
			}
			now, err := m.store.Now()
			if err != nil {
				return err
			}
			for i, id := range ids {
				row, err := store.QueryFoundryAttachments().Update(ctx, tx, id, store.FileDraft{}.SetSortOrder(int32(i)).SetUpdatedAt(now))
				if err != nil {
					return err
				}
				file, err := c.attachment(m, row)
				if err != nil {
					return err
				}
				result = append(result, file)
			}
			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// SetProperties replaces explicit dynamic object metadata, never storage pins,
// owner identity or collection policy.
func (c Collection[M, K]) SetProperties(ctx context.Context, m *Manager, owner model.Reference[M, K], id ID[M], properties value.JSON[json.RawMessage]) (Attachment[M, K], error) {
	if err := c.check(m); err != nil {
		return Attachment[M, K]{}, err
	}
	if id.IsZero() {
		return Attachment[M, K]{}, invalid()
	}
	properties, err := normalizeProperties(properties)
	if err != nil {
		return Attachment[M, K]{}, err
	}
	var result Attachment[M, K]
	err = m.calls.Run(ctx, "attachment custom properties", func(ctx context.Context) error {
		locale, err := c.localeName(ctx, m)
		if err != nil {
			return err
		}
		return m.store.Write(ctx, func(ctx context.Context, tx *database.Tx) error {
			subject, err := c.definition.owner.Lock(ctx, tx, m.store.Registry(), owner)
			if err != nil {
				return err
			}
			f := store.FileFields()
			q := store.QueryFoundryAttachments().Where(f.Scope.Eq(subject.Scope), f.SubjectKey.Eq(subject.Key), f.Collection.Eq(string(c.Name())), f.Locale.Eq(locale), f.State.Eq(string(Ready)))
			row, err := q.RequireFind(ctx, tx, model.IDFromBytes[store.File](id.Bytes()))
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
			row, err = q.Update(ctx, tx, row.ID, store.FileDraft{}.SetProperties(properties).SetUpdatedAt(now))
			if err != nil {
				return err
			}
			result, err = c.attachment(m, row)
			return err
		})
	})
	if err != nil {
		return Attachment[M, K]{}, err
	}
	return result, nil
}
