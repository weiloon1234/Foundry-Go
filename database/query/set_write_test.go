package query

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestSetBasedMutationsCompileWithoutPrimaryKeys(t *testing.T) {
	rank := NewNullableExactField[cursorRecord, int64]("records", "rank", codec.Signed[int64]())
	id := NewOrderedField[cursorRecord, int64]("records", "id", codec.Signed[int64]())
	q := scopedQuery(positiveScope).Where(id.Gt(10))
	assign := Change(Assign[cursorRecord]("records", "rank", codec.Nullable(codec.Signed[int64]()), value.Of(int64(3))))
	for _, test := range []struct {
		plan mutationPlan[cursorRecord]
		sql  string
		args []any
	}{
		{mutationPlan[cursorRecord]{query: q, kind: updateModel, mutation: assign, setBased: true, countOnly: true},
			`UPDATE "records" SET "rank" = $1 WHERE ("records"."id" > $2) AND ("records"."rank" > $3)`, []any{int64(3), int64(10), int64(0)}},
		{mutationPlan[cursorRecord]{query: q, kind: updateModel, setBased: true, countOnly: true, adjust: &adjustment{field: rank.ref, bind: rank.By(2).bind}},
			`UPDATE "records" SET "rank" = "rank" + $1 WHERE`, []any{int64(2), int64(10), int64(0)}},
		{mutationPlan[cursorRecord]{query: q, kind: updateModel, setBased: true, countOnly: true, adjust: &adjustment{field: rank.ref, bind: rank.By(2).bind, subtract: true}},
			`SET "rank" = "rank" - $1`, []any{int64(2), int64(10), int64(0)}},
		{mutationPlan[cursorRecord]{query: q, kind: deleteModel, setBased: true, countOnly: true},
			`DELETE FROM "records" WHERE ("records"."id" > $1) AND ("records"."rank" > $2)`, []any{int64(10), int64(0)}},
	} {
		statement, err := test.plan.compile()
		if err != nil || !strings.Contains(statement.SQL(), test.sql) || strings.Contains(statement.SQL(), "RETURNING") || !reflect.DeepEqual(statement.Arguments(), test.args) {
			t.Fatal("set-based statement", statement.SQL(), statement.Arguments(), err)
		}
	}
	// Adjusting an assigned column, the key or nothing at all is rejected.
	for _, bad := range []mutationPlan[cursorRecord]{
		{query: q, kind: updateModel, mutation: assign, setBased: true, adjust: &adjustment{field: rank.ref, bind: rank.By(1).bind}},
		{query: q, kind: updateModel, setBased: true, adjust: &adjustment{field: id.ref, bind: rank.By(1).bind}},
		{query: q, kind: updateModel, setBased: true},
		{query: q.OrderBy(id.Asc()), kind: deleteModel, setBased: true},
	} {
		if _, err := bad.compile(); !errors.Is(err, fault.Invalid) {
			t.Fatal("invalid set-based write compiled", err)
		}
	}
	// Per-model writes still require a primary-key predicate.
	if _, err := (mutationPlan[cursorRecord]{query: q, kind: updateModel, mutation: assign}).compile(); !errors.Is(err, fault.Invalid) {
		t.Fatal("per-model write lost its key requirement", err)
	}
}

// Set-based writes cannot silently skip hooks an executor might dispatch.
func TestSetBasedWritesRequireAcknowledgedHookSkipping(t *testing.T) {
	writer := &untouchedRelationWriter{}
	q := scopedQuery(positiveScope)
	if _, err := q.RemoveAll(t.Context(), writer); !errors.Is(err, fault.Invalid) || writer.calls != 0 {
		t.Fatal("unknown executor was assumed hook-free", err)
	}
	if _, err := q.WithoutModelHooks().RemoveAll(t.Context(), writer); err == nil || errors.Is(err, fault.Invalid) || writer.calls != 1 {
		t.Fatal("acknowledged set-based write did not reach its transaction", err)
	}
	// The acknowledgement never disables hooks of per-model writes.
	id := NewOrderedField[cursorRecord, int64]("records", "id", codec.Signed[int64]())
	if _, err := q.WithoutModelHooks().Where(id.Eq(1)).Remove(t.Context(), writer); !errors.Is(err, fault.Invalid) || writer.calls != 1 {
		t.Fatal("per-model write accepted WithoutModelHooks", err)
	}
	if _, err := q.AdjustAll(t.Context(), writer, Adjustment[cursorRecord]{}, false, Mutation[cursorRecord]{}); !errors.Is(err, fault.Invalid) {
		t.Fatal("zero adjustment accepted", err)
	}
}
