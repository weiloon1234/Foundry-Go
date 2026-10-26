package querycore

import (
	"math"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/internal/sqlvalue"
	"github.com/weiloon1234/Foundry-Go/temporal"
)

func TestIntervalRelationKeysFollowSQLEqualityWithoutChangingValues(t *testing.T) {
	type record struct{ Period temporal.Interval }
	elapsed, _ := temporal.Elapsed(30 * 24 * time.Hour)
	parents := []record{{temporal.Months(1)}, {temporal.Days(30)}, {elapsed}}
	positions, keys, seen, err := RelationKeys(t.Context(), len(parents), 2, func(i int) (temporal.Interval, sqlvalue.Value, bool, error) {
		v := parents[i].Period
		raw, err := codec.Interval().Bind(v)
		if err != nil {
			return v, sqlvalue.Value{}, false, err
		}
		key, err := RecordEqualityKey(codec.TypeInterval, raw)
		return v, key, true, err
	})
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
	if IntervalComparisonKey(big) == IntervalComparisonKey(near) {
		t.Fatal("large interval comparison key rounded away microseconds")
	}
}

func TestAggregateKeyEqualityNormalizesFloatingSignedZero(t *testing.T) {
	values := []float64{math.Copysign(0, -1), 0, 1}
	positions, keys, _, err := RelationKeys(t.Context(), len(values), 2, func(i int) (float64, sqlvalue.Value, bool, error) {
		key, err := RecordEqualityKey(codec.TypeFloat, values[i])
		return values[i], key, true, err
	})
	if err != nil || len(keys) != 2 {
		t.Fatal("equal zero keys not deduplicated", err)
	}
	returned, err := RecordEqualityKey(codec.TypeFloat, float64(0))
	first, _ := positions[0].Get()
	second, _ := positions[1].Get()
	if err != nil || first != second || first != returned {
		t.Fatal("SQL zero cannot match signed zero")
	}
}
