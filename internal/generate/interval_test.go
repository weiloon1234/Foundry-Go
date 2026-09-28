package generate

import (
	"reflect"
	"testing"
)

func TestFreshIntervalGeneration(t *testing.T) {
	dir := fixture(t, `package sample
import (
 "github.com/weiloon1234/Foundry-Go/database/query"
 "github.com/weiloon1234/Foundry-Go/temporal"
 "github.com/weiloon1234/Foundry-Go/value"
)
//foundry:model table=periods primary=ID
type Period struct { ID temporal.Interval; Length temporal.Interval; MaybeLength value.Nullable[temporal.Interval]; Date temporal.Date }
//foundry:projection
type Summary struct { Total value.Nullable[temporal.Interval]; Shifted temporal.LocalDateTime }
var initial = PeriodDraft{}.SetID(temporal.Months(1)).SetLength(temporal.Days(2)).ClearMaybeLength()
func selections() {
 q,f:=QueryPeriods(),PeriodFields()
 _ = query.SelectValue(q,f.Length.Sum().Value())
 _ = query.SelectValue(q,f.MaybeLength.Avg().Value())
 _ = ProjectSummary(q).SelectTotal(f.Length.Sum().Value()).SelectShifted(query.ShiftDate(f.Date,f.Length).Value())
}
`)
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	first := generatedSnapshot(t, dir)
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, generatedSnapshot(t, dir)) {
		t.Fatal("interval generation changed")
	}
	if _, err := Generate(t.Context(), Options{Dir: dir, Check: true}); err != nil {
		t.Fatal(err)
	}
}
