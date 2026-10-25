package scalarqueries

import (
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/decimal"
)

type SampleColumns struct {
	orm.Table[Sample, int]
	ID       orm.NumberPrimary[Sample, int]
	Quantity orm.Number[Sample, int16]
	Amount   orm.Number[Sample, decimal.Decimal]
	Score    orm.Number[Sample, float32]
	Name     orm.Text[Sample, Label]
	Note     orm.NullableText[Sample, Label]
	Tax      orm.NullableNumber[Sample, decimal.Decimal]
}

var Samples = orm.Define("calculation_samples", func(b orm.Builder[Sample], s *Sample) SampleColumns {
	table := orm.TableOf(b, &s.ID)
	return SampleColumns{Table: table, ID: orm.NumberKey(orm.PrimaryOf(b, table.Key())),
		Quantity: orm.NumberOf(b, &s.Quantity),
		Amount:   orm.NumberOf(b, &s.Amount),
		Score:    orm.NumberOf(b, &s.Score),
		Name:     orm.TextOf(b, &s.Name),
		Note:     orm.NullableTextOf(b, &s.Note),
		Tax:      orm.NullableNumberOf(b, &s.Tax),
	}
})
