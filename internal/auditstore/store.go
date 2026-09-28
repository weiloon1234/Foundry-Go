package auditstore

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/temporal"
)

// Append requires already validated, captured input. Normal generated writes
// retain savepoint behavior and never commit the supplied outer transaction.
func Append(ctx context.Context, tx *database.Tx, draft EntryDraft) (Entry, error) {
	return QueryFoundryAudit().Create(ctx, tx, draft)
}

// Prune selects a bounded oldest window and removes only that window through
// the existing typed set-based write compiler. No payloads are hydrated and no
// per-model observers run. A concurrent pruner may remove fewer rows.
func Prune(ctx context.Context, tx *database.Tx, area string, cutoff temporal.DateTime, limit int) (int64, error) {
	f := EntryFields()
	destination := QueryFoundryAudit().Where(f.Area.Eq(area), f.CreatedAt.Lt(cutoff))
	source := destination.OrderBy(f.CreatedAt.Asc(), f.ID.Asc()).Limit(limit)
	return DeleteEntryUsing(destination, source).MatchID(f.ID.Value()).Exec(ctx, tx)
}

// Page retains the ordinary query layer's count/snapshot and row-limit contract.
func Page(ctx context.Context, executor database.Executor, q EntryQuery, request query.PageRequest) (query.Page[Entry], error) {
	f := EntryFields()
	return q.OrderBy(f.CreatedAt.Desc(), f.ID.Asc()).Paginate(ctx, executor, request)
}
