package transactionqueries

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/query"
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
	definition := query.TransactionCTE("claimed", QueryRecords().OrderBy(RecordFields().ID.Asc()).Limit(1).ForUpdate().SkipLocked())
	source := query.AsTransaction[claimedAlias](definition, "candidate")
	fields := RecordFieldsAt(source.Scope())
	return ProjectTransactionSummary(source).
		SelectID(fields.ID.Value()).SelectName(fields.Name.Value()).Query().All(ctx, tx)
}

func Joined(ctx context.Context, tx *database.Tx) ([]Summary, error) {
	definition := query.TransactionCTE("claimed", QueryRecords().ForShare().Limit(1))
	left := query.AsTransaction[claimedAlias](definition, "candidate")
	right := query.AsTransaction[otherAlias](query.TransactionOf(QueryRecords()), "other")
	a, b := RecordFieldsAt(left.Scope()), RecordFieldsAt(right.Scope())
	joined := query.TransactionInnerJoin(left, right, query.On(a.ID, b.ID))
	fields := RecordFieldsAt(query.LeftScope(joined, left.Scope()))
	return ProjectTransactionSummary(joined).
		SelectID(fields.ID.Value()).SelectName(fields.Name.Value()).Query().All(ctx, tx)
}

func CountClaimed(ctx context.Context, tx *database.Tx) (Totals, error) {
	definition := query.TransactionCTE("claimed", QueryRecords().ForUpdate())
	source := query.AsTransaction[claimedAlias](definition, "candidate")
	fields := RecordFieldsAt(source.Scope())
	return ProjectTransactionTotals(source).SelectCount(fields.ID.Count().Value()).Query().RequireFirst(ctx, tx)
}
