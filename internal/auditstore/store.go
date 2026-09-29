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
	source := destination.OrderBy(f.CreatedAt.Asc(), f.Sequence.Asc()).Limit(limit)
	return DeleteEntryUsing(destination, source).MatchID(f.ID.Value()).Exec(ctx, tx)
}

// CountBefore counts the entries Prune would remove for area and cutoff without
// reading payloads. It uses the same predicate as Prune.
func CountBefore(ctx context.Context, executor database.Executor, area string, cutoff temporal.DateTime) (int64, error) {
	f := EntryFields()
	return QueryFoundryAudit().Where(f.Area.Eq(area), f.CreatedAt.Lt(cutoff)).Count(ctx, executor)
}

// Page retains the ordinary query layer's count/snapshot and row-limit contract.
// Rows are newest first by database insertion sequence, so writes made in one
// transaction keep their actual order despite sharing a transaction timestamp.
func Page(ctx context.Context, executor database.Executor, q EntryQuery, request query.PageRequest) (query.Page[Entry], error) {
	f := EntryFields()
	return q.OrderBy(f.Sequence.Desc()).Paginate(ctx, executor, request)
}

// Recent reads at most limit complete rows newest first, strictly before the
// supplied sequence when one is given. It is the keyset step of typed history.
func Recent(ctx context.Context, executor database.Executor, q EntryQuery, before int64, limit int) ([]Entry, error) {
	f := EntryFields()
	if before > 0 {
		q = q.Where(f.Sequence.Lt(before))
	}
	return q.OrderBy(f.Sequence.Desc()).Limit(limit).All(ctx, executor)
}

// RecentActivity is Recent without selecting or decoding stored payloads.
func RecentActivity(ctx context.Context, executor database.Executor, q EntryQuery, before int64, limit int) ([]Activity, error) {
	f := EntryFields()
	if before > 0 {
		q = q.Where(f.Sequence.Lt(before))
	}
	return SelectActivity(q.OrderBy(f.Sequence.Desc()).Limit(limit), ActivitySelection[Entry]{
		ID: f.ID.Value(), Sequence: f.Sequence.Value(), Area: f.Area.Value(), Operation: f.Operation.Value(),
		Action: f.Action.Value(), Version: f.Version.Value(), Subject: f.Subject.Value(), Origin: f.Origin.Value(),
		Correlation: f.Correlation.Value(), RequestMethod: f.RequestMethod.Value(), RequestRoute: f.RequestRoute.Value(),
		CreatedAt: f.CreatedAt.Value(),
	}).All(ctx, executor)
}
