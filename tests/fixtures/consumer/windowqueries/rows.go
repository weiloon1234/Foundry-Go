package windowqueries

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

type RankingRowColumns struct {
	rows.Row[RankingRow]
	ID         rows.Field[RankingRow, model.ID[models.Order]]
	BuyerID    rows.Field[RankingRow, model.ID[models.User]]
	Number     rows.Field[RankingRow, int64]
	Rank       rows.Field[RankingRow, int64]
	Dense      rows.Field[RankingRow, int64]
	Percent    rows.Field[RankingRow, float64]
	Cumulative rows.Field[RankingRow, float64]
	Bucket     rows.Field[RankingRow, int32]
}

var Rankings = rows.Define(func(b rows.Builder[RankingRow], r *RankingRow) RankingRowColumns {
	return RankingRowColumns{Row: b.Row(),
		ID:         rows.IDOf(b, &r.ID),
		BuyerID:    rows.IDOf(b, &r.BuyerID),
		Number:     rows.NumberOf(b, &r.Number),
		Rank:       rows.NumberOf(b, &r.Rank),
		Dense:      rows.NumberOf(b, &r.Dense),
		Percent:    rows.NumberOf(b, &r.Percent),
		Cumulative: rows.NumberOf(b, &r.Cumulative),
		Bucket:     rows.NumberOf(b, &r.Bucket),
	}
})

type NavigationRowColumns struct {
	rows.Row[NavigationRow]
	Amount   rows.Field[NavigationRow, int64]
	Previous rows.Field[NavigationRow, value.Nullable[int64]]
	Next     rows.Field[NavigationRow, value.Nullable[int64]]
	Fallback rows.Field[NavigationRow, int64]
	First    rows.Field[NavigationRow, value.Nullable[int64]]
	Last     rows.Field[NavigationRow, value.Nullable[int64]]
	Nth      rows.Field[NavigationRow, value.Nullable[int64]]
}

var Navigation = rows.Define(func(b rows.Builder[NavigationRow], r *NavigationRow) NavigationRowColumns {
	return NavigationRowColumns{Row: b.Row(),
		Amount:   rows.NumberOf(b, &r.Amount),
		Previous: rows.NullableNumberOf(b, &r.Previous),
		Next:     rows.NullableNumberOf(b, &r.Next),
		Fallback: rows.NumberOf(b, &r.Fallback),
		First:    rows.NullableNumberOf(b, &r.First),
		Last:     rows.NullableNumberOf(b, &r.Last),
		Nth:      rows.NullableNumberOf(b, &r.Nth),
	}
})

type WindowTotalColumns struct {
	rows.Row[WindowTotal]
	Amount  rows.Field[WindowTotal, int64]
	Running rows.Field[WindowTotal, value.Nullable[decimal.Decimal]]
	Count   rows.Field[WindowTotal, int64]
	Any     rows.Field[WindowTotal, bool]
}

var WindowTotals = rows.Define(func(b rows.Builder[WindowTotal], r *WindowTotal) WindowTotalColumns {
	return WindowTotalColumns{Row: b.Row(),
		Amount:  rows.NumberOf(b, &r.Amount),
		Running: rows.NullableNumberOf(b, &r.Running),
		Count:   rows.NumberOf(b, &r.Count),
		Any:     rows.FieldOf(b, &r.Any, orm.Codec(codec.Bool[bool]())),
	}
})
