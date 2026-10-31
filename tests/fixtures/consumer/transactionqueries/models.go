package transactionqueries

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/value"
)

type Record struct {
	ID   int64
	Name string
}

type Summary struct {
	ID   int64
	Name string
}

type Totals struct{ Count int64 }

type Pair struct {
	Left  value.Nullable[int64]
	Right value.Nullable[int64]
}

type claimedAlias struct{}
type otherAlias struct{}

func Claimed(ctx context.Context, tx *database.Tx) ([]Summary, error) {
	definition := rows.TransactionCTE("claimed", rows.LockedRecord(orm.Lock(Records.Query().OrderBy(orm.KeyAsc(Records.ID)).Limit(1)).SkipLocked()))
	source := rows.AsTransaction[claimedAlias](definition, "candidate")
	fields := recordAt(rows.TransactionScope(source))
	return rows.SelectTransaction(source, Summaries, rows.MapSelected(Summaries.ID, expr.AsSelected(expr.Row(fields.ID))), rows.MapSelected(Summaries.Name, expr.AsSelected(expr.Row(fields.Name)))).All(ctx, tx)
}

func Joined(ctx context.Context, tx *database.Tx) ([]Summary, error) {
	definition := rows.TransactionCTE("claimed", rows.LockedRecord(orm.ForShare(Records.Query()).Limit(1)))
	left := rows.AsTransaction[claimedAlias](definition, "candidate")
	right := rows.AsTransaction[otherAlias](rows.TransactionOf(rows.ModelRecord(Records.Query())), "other")
	a, b := recordAt(rows.TransactionScope(left)), recordAt(rows.TransactionScope(right))
	joined := rows.TransactionInnerJoin(left, right, rows.OnEqual(a.ID, b.ID))
	fields := recordAt(rows.LeftScope(joined, rows.TransactionScope(left)))
	return rows.SelectTransaction(joined, Summaries, rows.MapSelected(Summaries.ID, expr.AsSelected(expr.Row(fields.ID))), rows.MapSelected(Summaries.Name, expr.AsSelected(expr.Row(fields.Name)))).All(ctx, tx)
}

func CountClaimed(ctx context.Context, tx *database.Tx) (Totals, error) {
	definition := rows.TransactionCTE("claimed", rows.LockedRecord(orm.Lock(Records.Query())))
	source := rows.AsTransaction[claimedAlias](definition, "candidate")
	fields := recordAt(rows.TransactionScope(source))
	return rows.SelectTransaction(source, TotalRows, rows.MapSelected(TotalRows.Count, expr.AsSelected(expr.CountFieldAt(fields.scope, orm.KeyExpr(Records.ID))))).RequireFirst(ctx, tx)
}
