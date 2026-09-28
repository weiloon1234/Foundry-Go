package query

import (
	"database/sql/driver"
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/relation"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestTypedAggregateSQLAndResultRepresentations(t *testing.T) {
	integer := NewExactField[cursorRecord, int64]("records", "id", codec.Signed[int64]())
	input := testThrough().Where(integer.Gt(2)).WherePivot(integer.Lt(20)).aggregateInput()
	statement, err := compileRelationAggregate(input, integer.Avg().node, []driver.Value{int64(3), int64(4)}, 9)
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{
		`SELECT "foundry_pivot"."id", AVG(CAST("foundry_target"."id" AS numeric)), COUNT(*), COUNT(DISTINCT "foundry_pivot"."id")`,
		`WHERE ("foundry_target"."id" > $1) AND ("foundry_pivot"."id" < $2) AND ("foundry_pivot"."id" IN ($3, $4)) GROUP BY "foundry_pivot"."id" LIMIT $5`,
	} {
		if !strings.Contains(statement.SQL(), text) {
			t.Fatalf("missing %s in %s", text, statement.SQL())
		}
	}
	if !reflect.DeepEqual(statement.Arguments(), []any{int64(2), int64(20), int64(3), int64(4), int64(9)}) {
		t.Fatal("grouped parameters out of order")
	}
	if strings.Contains(statement.SQL(), "ORDER BY") {
		t.Fatal("aggregate retained unrelated row ordering")
	}
	result, err := integer.Avg().codec.Decode("1.5000000000000000")
	d, present := result.Get()
	if err != nil || !present || d.String() != "1.5" {
		t.Fatal("integer average lost decimal precision", err)
	}
	if v, err := integer.Sum().codec.Decode(nil); err != nil || !v.IsNull() {
		t.Fatal("aggregate NULL became zero")
	}
	floating := NewFloatField[cursorRecord, float32]("records", "id", codec.Float[float32]())
	floatValue, err := floating.Avg().codec.Decode(float64(1.5))
	f, ok := floatValue.Get()
	if err != nil || !ok || f != 1.5 {
		t.Fatal("float average result is not float64")
	}
	if _, err := integer.Sum().codec.Decode(float64(1.5)); err == nil {
		t.Fatal("approximate driver result accepted as exact decimal")
	}
}

func aggregateSlot(r AggregateRelation[cursorRecord, int64]) AggregateRelation[cursorRecord, int64] {
	return r.Bind("Count", cursorQuery(), func(cursorRecord) relation.Value[int64] { return relation.Value[int64]{} }, func(m cursorRecord, _ relation.Value[int64]) cursorRecord { return m })
}
func TestAggregateValidationAndCapturedScopes(t *testing.T) {
	r := testThrough()
	count := aggregateSlot(Related(r, Count[cursorRecord]()))
	if err := cursorQuery().With(count).Validate(); err != nil {
		t.Fatal(err)
	}
	var missing *ThroughRelation[cursorRecord, cursorRecord, cursorRecord]
	badField := NewExactField[cursorRecord, int64]("other", "id", codec.Signed[int64]())
	for _, q := range []Query[cursorRecord]{
		cursorQuery().With(count, count),
		cursorQuery().With(aggregateSlot(Related(missing, Count[cursorRecord]()))),
		cursorQuery().With(aggregateSlot(Related(r, Aggregate[cursorRecord, int64]{}))),
		cursorQuery().With(aggregateSlot(Related(r, badField.Count()))),
		cursorQuery().With(aggregateSlot(Related(r.WithPivot(testRelation(cursorQuery(), cursorQuery())), Count[cursorRecord]()))),
		cursorQuery().With(aggregateSlot(Related(r.With(testRelation(cursorQuery(), cursorQuery())), Count[cursorRecord]()))),
	} {
		if err := q.Validate(); !errors.Is(err, fault.Invalid) {
			t.Fatal("invalid aggregate declaration accepted", err)
		}
	}
	derived := count.Using(Related(r.WherePivot(NewScalarField[cursorRecord, int64]("records", "id", codec.Signed[int64]()).Eq(1)), Count[cursorRecord]()))
	if err := cursorQuery().With(derived).Validate(); err != nil {
		t.Fatal(err)
	}
	if count.name != derived.name || count.table != derived.table {
		t.Fatal("Using lost generated slot identity")
	}
	invalidBind := count.Bind("Count", cursorQuery().Limit(1), count.get, count.set)
	if err := cursorQuery().With(count.Using(invalidBind)).Validate(); !errors.Is(err, fault.Invalid) {
		t.Fatal("Using discarded invalid computation binding")
	}
}

func TestAggregateKeyEqualityNormalizesFloatingSignedZero(t *testing.T) {
	type parent struct{ key float64 }
	metadata := NewModelField("key", codec.Float[float64](), func(p parent) float64 { return p.key })
	positions, keys, _, err := aggregateKeys(t.Context(), metadata, []parent{{math.Copysign(0, -1)}, {0}, {1}}, 2)
	if err != nil || len(keys) != 2 {
		t.Fatal("equal signed-zero keys were not deduplicated", err)
	}
	first, _ := positions[0].Get()
	second, _ := positions[1].Get()
	returned, err := encodeModelKey(float64(0))
	if err != nil || first != second || first != returned {
		t.Fatal("SQL zero cannot match the source model's signed zero")
	}
}

func TestAggregateLoadedNullIsDistinctFromNotLoaded(t *testing.T) {
	var slot relation.Value[value.Nullable[decimal.Decimal]]
	if _, loaded := slot.Get(); loaded || slot.IsLoaded() {
		t.Fatal("zero aggregate is loaded")
	}
	slot = relation.Computed(value.Null[decimal.Decimal]())
	if v, loaded := slot.Get(); !loaded || !v.IsNull() {
		t.Fatal("computed SQL NULL lost loaded state")
	}
	count := relation.Computed(int64(0))
	if v, loaded := count.Get(); !loaded || v != 0 {
		t.Fatal("zero count is not loaded")
	}
}
