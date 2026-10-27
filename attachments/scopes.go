package attachments

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/fault"
	store "github.com/weiloon1234/Foundry-Go/internal/attachmentstore"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
)

// Matching returns a bounded materialized predicate for owners with a ready
// attachment in this collection and exact locale. The later owner query retains
// its normal visibility and authorization filters; this is not a live subquery.
func (c Collection[M, K]) matchingKeys(ctx context.Context, m *Manager) ([]K, error) {
	if err := c.check(m); err != nil {
		return nil, err
	}
	var keys []K
	err := m.reads.Run(ctx, "attachment owner scope", func(ctx context.Context) error {
		return m.store.Read(ctx, func(ctx context.Context, tx *database.Tx) error {
			locale, err := c.localeName(ctx, m)
			if err != nil {
				return err
			}
			f := store.Files
			rows, err := rows.Read(store.Index(store.Files.Query().Where(f.Scope.Eq(c.definition.owner.Scope()), f.Collection.Eq(string(c.Name())), f.Locale.Eq(locale), f.State.Eq(string(Ready))).OrderBy(f.SubjectKey.Asc(), f.ID.Asc()).Limit(MaxBatchFiles+1))).All(ctx, tx)
			if err != nil {
				return err
			}
			if len(rows) > MaxBatchFiles {
				return attachmentFailure(fault.Conflict, "attachment scope exceeds its file limit")
			}
			seen := make(map[string]bool)
			for _, row := range rows {
				subject, err := m.validateIndex(row)
				if err != nil {
					return err
				}
				if seen[subject] {
					continue
				}
				seen[subject] = true
				if len(seen) > querycore.MaxIdentityBatch {
					return attachmentFailure(fault.Conflict, "attachment scope exceeds its owner limit")
				}
				identity, err := row.Identity.Decode()
				if err != nil {
					return err
				}
				owner, err := c.definition.owner.Parse(identity)
				if err != nil {
					return err
				}
				keys = append(keys, owner.Key())
			}
			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	return keys, nil
}
