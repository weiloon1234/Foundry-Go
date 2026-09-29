package notifications

import (
	"context"
	"time"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/query"
	store "github.com/weiloon1234/Foundry-Go/internal/notificationstore"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/temporal"
)

// inboxBatch bounds the records one bulk inbox transaction changes.
const inboxBatch = 500

// MarkAllRead marks every unread record of the current recipient read in
// bounded transactions, oldest first, and returns the number changed. Records
// that were already read keep their first read instant. A failed batch leaves
// earlier committed batches in place.
func (i Inbox[M, K]) MarkAllRead(ctx context.Context) (int, error) {
	ctx, release, err := i.manager.begin(ctx)
	if err != nil {
		return 0, err
	}
	defer release()
	subject, err := i.subject(ctx)
	if err != nil {
		return 0, err
	}
	f := store.InboxFields()
	total := 0
	for {
		changed := 0
		err := i.manager.within(ctx, func(tx *database.Tx) error {
			rows, err := i.rows(subject).Where(f.ReadAt.IsNull()).OrderBy(f.CreatedAt.Asc(), f.ID.Asc()).Limit(inboxBatch).All(ctx, tx)
			if err != nil || len(rows) == 0 {
				return err
			}
			ids := make([]model.ID[store.Inbox], len(rows))
			for index, row := range rows {
				ids[index] = row.ID
			}
			now, err := i.manager.now()
			if err != nil {
				return err
			}
			updated, err := i.rows(subject).Where(f.ID.In(ids...), f.ReadAt.IsNull()).UpdateEach(ctx, tx, len(ids), func(context.Context, *database.Tx, store.Inbox) (store.InboxDraft, error) {
				return store.InboxDraft{}.SetReadAt(now), nil
			})
			changed = len(updated)
			return err
		})
		if err != nil {
			return total, err
		}
		total += changed
		if changed < inboxBatch {
			return total, nil
		}
	}
}

// Delete removes one inbox record of the current recipient. It returns false
// for both an absent ID and another recipient's ID. The notification and its
// delivery states are kept, so a retry never recreates the record.
func (i Inbox[M, K]) Delete(ctx context.Context, id ID[M]) (bool, error) {
	if id.IsZero() {
		return false, invalid()
	}
	ctx, release, err := i.manager.begin(ctx)
	if err != nil {
		return false, err
	}
	defer release()
	subject, err := i.subject(ctx)
	if err != nil {
		return false, err
	}
	deleted := false
	err = i.manager.within(ctx, func(tx *database.Tx) error {
		removed, err := i.rows(subject).Where(store.InboxFields().ID.Eq(model.IDFromBytes[store.Inbox](id.Bytes()))).DeleteEach(ctx, tx, 1)
		deleted = len(removed) == 1
		return err
	})
	return deleted && err == nil, err
}

// MaxPruneBatch bounds the inbox records one PruneInbox transaction deletes.
const MaxPruneBatch = 10000

// PruneInbox deletes up to limit inbox records created before cutoff across
// every recipient, oldest first; readOnly keeps unread records. It returns the
// number deleted; repeat until fewer than limit are deleted. The notifications
// and their delivery states are kept, so retries never recreate a pruned
// record. It is an operator operation: authorize its callers.
func (m *Manager) PruneInbox(ctx context.Context, cutoff time.Time, readOnly bool, limit int) (int, error) {
	if limit == 0 {
		limit = 1000
	}
	if cutoff.IsZero() || limit < 1 || limit > MaxPruneBatch {
		return 0, invalid()
	}
	before, err := temporal.NewDateTime(cutoff.UTC().Truncate(time.Microsecond))
	if err != nil {
		return 0, invalid()
	}
	ctx, release, err := m.begin(ctx)
	if err != nil {
		return 0, err
	}
	defer release()
	f := store.InboxFields()
	deleted := 0
	err = m.within(ctx, func(tx *database.Tx) error {
		deleted = 0
		predicates := []query.Predicate[store.Inbox]{f.CreatedAt.Lt(before)}
		if readOnly {
			predicates = append(predicates, f.ReadAt.IsNotNull())
		}
		// One bounded DELETE ... WHERE id IN (SELECT id ... LIMIT n): no
		// rendered inbox data is loaded to choose the batch.
		oldest := query.SelectValue(store.QueryFoundryNotificationInbox().Where(predicates...).OrderBy(f.CreatedAt.Asc(), f.ID.Asc()).Limit(limit), f.ID.Value())
		removed, err := store.QueryFoundryNotificationInbox().Where(f.ID.InQuery(oldest)).DeleteAll(ctx, tx)
		deleted = int(removed)
		return err
	})
	return deleted, err
}
