package transactionqueries

import (
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/value"
)

func nullableRecordAt[S any](scope expr.NullableScope[S, Record]) struct {
	ID expr.Expression[S, int64, value.Nullable[int64]]
} {
	return struct {
		ID expr.Expression[S, int64, value.Nullable[int64]]
	}{expr.AtNullable(scope, Records.ID)}
}

func nullableSummaryAt[S any](scope expr.NullableScope[S, Summary]) struct {
	ID expr.RowExpression[S, value.Nullable[int64]]
} {
	return struct {
		ID expr.RowExpression[S, value.Nullable[int64]]
	}{rows.AtNullable(scope, Summaries.ID)}
}
