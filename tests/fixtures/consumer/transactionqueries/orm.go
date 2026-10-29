package transactionqueries

import (
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/value"
)

type RecordColumns struct {
	orm.Table[Record, int64]
	ID   orm.Key[Record, int64]
	Name orm.Text[Record, string]
}

var Records = orm.Define("records", func(b orm.Builder[Record], r *Record) RecordColumns {
	table := orm.TableOf(b, &r.ID)
	return RecordColumns{table, table.Key(), orm.TextOf(b, &r.Name)}
})

type SummaryColumns struct {
	rows.Row[Summary]
	ID   rows.Field[Summary, int64]
	Name rows.Field[Summary, string]
}

var Summaries = rows.Define(func(b rows.Builder[Summary], s *Summary) SummaryColumns {
	return SummaryColumns{b.Row(), rows.NumberOf(b, &s.ID), rows.TextOf(b, &s.Name)}
})

type TotalColumns struct {
	rows.Row[Totals]
	Count rows.Field[Totals, int64]
}

var TotalRows = rows.Define(func(b rows.Builder[Totals], t *Totals) TotalColumns {
	return TotalColumns{b.Row(), rows.NumberOf(b, &t.Count)}
})

type PairColumns struct {
	rows.Row[Pair]
	Left  rows.Field[Pair, value.Nullable[int64]]
	Right rows.Field[Pair, value.Nullable[int64]]
}

var Pairs = rows.Define(func(b rows.Builder[Pair], p *Pair) PairColumns {
	return PairColumns{b.Row(), rows.NullableNumberOf(b, &p.Left), rows.NullableNumberOf(b, &p.Right)}
})

// Each binding retains its original scope; joined/correlated scopes must be lifted
// explicitly before these fields can be used in that query.
type recordExpressions[S any] struct {
	scope expr.Scope[S, Record]
	ID    expr.Expression[S, int64, int64]
	Name  expr.Expression[S, string, string]
}

func recordAt[S any](scope expr.Scope[S, Record]) recordExpressions[S] {
	return recordExpressions[S]{scope, expr.At(scope, orm.KeyExpr(Records.ID)), expr.At(scope, orm.TextExpr(Records.Name))}
}
