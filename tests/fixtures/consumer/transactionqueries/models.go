package transactionqueries

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/value"
)

//foundry:model table=records primary=ID
type Record struct {
	ID   int64
	Name string
}

//foundry:projection
type Summary struct {
	ID   int64
	Name string
}

//foundry:projection
type Totals struct{ Count int64 }

//foundry:projection
type Pair struct {
	Left  value.Nullable[int64]
	Right value.Nullable[int64]
}

type claimedAlias struct{}
type otherAlias struct{}

func Claimed(ctx context.Context, tx *database.Tx) ([]Summary, error) {
	definition := rows.TransactionCTE("claimed", rows.LockedRecord(Records.Query().OrderBy(Records.ID.Asc()).Limit(1).ForUpdate().SkipLocked()))
	source := rows.AsTransaction[claimedAlias](definition, "candidate")
	fields := recordAt(rows.TransactionScope(source))
	return rows.SelectTransaction(source, Summaries, rows.MapSelected(Summaries.ID, expr.Select(fields.ID)), rows.MapSelected(Summaries.Name, expr.Select(fields.Name))).All(ctx, tx)
}

func Joined(ctx context.Context, tx *database.Tx) ([]Summary, error) {
	definition := rows.TransactionCTE("claimed", rows.LockedRecord(Records.Query().ForShare().Limit(1)))
	left := rows.AsTransaction[claimedAlias](definition, "candidate")
	right := rows.AsTransaction[otherAlias](rows.TransactionOf(rows.ModelRecord(Records.Query())), "other")
	a, b := recordAt(rows.TransactionScope(left)), recordAt(rows.TransactionScope(right))
	joined := rows.TransactionInnerJoin(left, right, rows.OnEqual(a.ID, b.ID))
	fields := recordAt(rows.LeftScope(joined, rows.TransactionScope(left)))
	return rows.SelectTransaction(joined, Summaries, rows.MapSelected(Summaries.ID, expr.Select(fields.ID)), rows.MapSelected(Summaries.Name, expr.Select(fields.Name))).All(ctx, tx)
}

func CountClaimed(ctx context.Context, tx *database.Tx) (Totals, error) {
	definition := rows.TransactionCTE("claimed", rows.LockedRecord(Records.Query().ForUpdate()))
	source := rows.AsTransaction[claimedAlias](definition, "candidate")
	fields := recordAt(rows.TransactionScope(source))
	return rows.SelectTransaction(source, TotalRows, rows.MapSelected(TotalRows.Count, expr.CountFieldAt(fields.scope, Records.ID).Value())).RequireFirst(ctx, tx)
}
