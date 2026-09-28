package query

import (
	"math"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/temporal"
)

func TestIntervalRelationKeysFollowSQLEqualityWithoutChangingValues(t *testing.T) {
	type record struct{ Period temporal.Interval }
	metadata := NewModelField("period", codec.Interval(), func(r record) temporal.Interval { return r.Period })
	field := NewOrderedField[record, temporal.Interval]("intervals", "period", codec.Interval())
	elapsed, _ := temporal.Elapsed(30 * 24 * time.Hour)
	parents := []record{{temporal.Months(1)}, {temporal.Days(30)}, {elapsed}}
	positions, keys, seen, err := relationKeys(t.Context(), metadata, field.ScalarField, parents, 2)
	if err != nil || len(keys) != 1 || len(seen) != 1 || keys[0] != temporal.Months(1) {
		t.Fatal(keys, seen, err)
	}
	for _, p := range positions {
		if p != positions[0] {
			t.Fatal("SQL-equal intervals produced different groups")
		}
	}
	if parents[1].Period != temporal.Days(30) || parents[2].Period != elapsed {
		t.Fatal("interval grouping changed stored components")
	}
	big, _ := temporal.NewInterval(math.MaxInt32, math.MaxInt32, time.Hour)
	near, _ := temporal.NewInterval(math.MaxInt32, math.MaxInt32, time.Hour-time.Microsecond)
	if intervalComparisonKey(big) == intervalComparisonKey(near) {
		t.Fatal("large interval comparison key rounded away microseconds")
	}
}
