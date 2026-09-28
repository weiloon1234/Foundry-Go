package query

import (
	"errors"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/relation"
	"github.com/weiloon1234/Foundry-Go/fault"
)

func testRelation(source, target Query[cursorRecord]) OneRelation[cursorRecord, cursorRecord] {
	field := NewScalarField[cursorRecord, int64]("records", "id", codec.Signed[int64]())
	return BelongsTo(field, field).Bind("Parent", source, target, func(cursorRecord) relation.One[cursorRecord] { return relation.One[cursorRecord]{} }, func(m cursorRecord, _ relation.One[cursorRecord]) cursorRecord { return m })
}

func TestRelationBindingsAndCapturedValues(t *testing.T) {
	q := cursorQuery()
	field := NewScalarField[cursorRecord, int64]("records", "id", codec.Signed[int64]())
	r := testRelation(q, q)
	captured := q.With(&r)
	r = r.Where(field.Eq(1))
	if len(captured.relations[0].(OneRelation[cursorRecord, cursorRecord]).spec.target.predicates) != 0 {
		t.Fatal("With retained caller's mutable descriptor pointer")
	}
	if err := q.With(r).Validate(); err != nil {
		t.Fatal(err)
	}
	var missing *OneRelation[cursorRecord, cursorRecord]
	for _, bad := range []Query[cursorRecord]{
		q.With(missing), q.With(r, r),
		q.With(testRelation(q.Where(field.Eq(1)), q)),
		q.With(testRelation(q, q.Where(field.Eq(1)))),
		q.With(testRelation(q, q.Limit(1))),
	} {
		if err := bad.Validate(); !errors.Is(err, fault.Invalid) {
			t.Fatal("invalid/scoped metadata binding accepted")
		}
	}
	var missingField *ScalarField[cursorRecord, int64]
	if err := q.With(BelongsTo(missingField, field)).Validate(); !errors.Is(err, fault.Invalid) {
		t.Fatal("nil typed key descriptor accepted")
	}
	nested := testRelation(q, q)
	for range 9 {
		nested = testRelation(q, q).With(nested)
	}
	if err := q.With(nested).Validate(); !errors.Is(err, fault.Invalid) {
		t.Fatal("default depth bound ignored")
	}
	limits := DefaultRelationLimits()
	limits.MaxDepth = 12
	if err := q.WithRelationLimits(limits).With(nested).Validate(); err != nil {
		t.Fatal(err)
	}
}
