package query

import (
	"errors"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
)

func TestOneOfManyLoadsFirstRowPerKey(t *testing.T) {
	rank := NewNullableOrderedField[cursorRecord, int64]("records", "rank", codec.Signed[int64]())
	r := testRelation(cursorQuery(), cursorQuery()).OfMany(rank.Desc())
	statement, err := r.oneOfManyTarget().Compile()
	if err != nil || !strings.Contains(statement.SQL(), `SELECT DISTINCT ON ("records"."id")`) || !strings.Contains(statement.SQL(), `ORDER BY "records"."id" ASC, "records"."rank" DESC`) {
		t.Fatal("one-of-many did not choose per key", statement.SQL(), err)
	}
	latest := testRelation(cursorQuery(), cursorQuery()).LatestOfMany()
	if statement, err := latest.oneOfManyTarget().Compile(); err != nil || !strings.Contains(statement.SQL(), `"records"."id" DESC`) {
		t.Fatal("latest of many", statement.SQL(), err)
	}
	if err := cursorQuery().With(testRelation(cursorQuery(), cursorQuery()).OfMany()).Validate(); !errors.Is(err, fault.Invalid) {
		t.Fatal("one-of-many without an order accepted", err)
	}
	input := r.aggregateInput()
	if err := input.validate(0, DefaultRelationLimits()); !errors.Is(err, fault.Invalid) {
		t.Fatal("aggregate over one-of-many accepted", err)
	}
}
