package framequeries

import (
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"time"
)

type SampleColumns struct {
	orm.Table[Sample, int]
	ID     orm.NumberPrimary[Sample, int]
	Band   orm.Number[Sample, int16]
	Amount orm.Number[Sample, decimal.Decimal]
	Score  orm.NullableNumber[Sample, float32]
	At     orm.Time[Sample, time.Time]
	Date   orm.Time[Sample, temporal.Date]
	Local  orm.Time[Sample, temporal.LocalDateTime]
	Clock  orm.Time[Sample, temporal.Time]
}

var Samples = orm.Define("frame_samples", func(b orm.Builder[Sample], s *Sample) SampleColumns {
	table := orm.TableOf(b, &s.ID)
	return SampleColumns{Table: table, ID: orm.NumberKey(orm.PrimaryOf(b, table.Key())),
		Band:   orm.NumberOf(b, &s.Band),
		Amount: orm.NumberOf(b, &s.Amount),
		Score:  orm.NullableNumberOf(b, &s.Score),
		At:     orm.TimeOf(b, &s.At),
		Date:   orm.TimeOf(b, &s.Date),
		Local:  orm.TimeOf(b, &s.Local),
		Clock:  orm.TimeOf(b, &s.Clock),
	}
})
