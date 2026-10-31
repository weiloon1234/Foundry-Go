package expr_test

import (
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/enum"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
	"github.com/weiloon1234/Foundry-Go/value"
)

type recordState string

const (
	stateActive  recordState = "active"
	statePending recordState = "pending"
)

func (recordState) EnumDescriptor() enum.Descriptor[recordState] {
	return enum.Describe("database/orm/expr_test", "recordState",
		enum.Case[recordState]{Name: "stateActive", Value: stateActive},
		enum.Case[recordState]{Name: "statePending", Value: statePending},
	)
}

type filterTestRecord struct {
	ID       int64
	State    recordState
	Nullable value.Nullable[recordState]
}

type filterTestFields struct {
	orm.Table[filterTestRecord, int64]
	State    orm.Enum[filterTestRecord, recordState]
	Nullable orm.NullableEnum[filterTestRecord, recordState]
}

var filterTestTable = orm.Define("filter_test_records", func(b orm.Builder[filterTestRecord], r *filterTestRecord) filterTestFields {
	table := orm.TableOf(b, &r.ID)
	return filterTestFields{
		Table:    table,
		State:    orm.EnumOf(b, &r.State),
		Nullable: orm.NullableEnumOf(b, &r.Nullable),
	}
})

func TestRowComparisonValue(t *testing.T) {
	cmp := expr.CompareRow(expr.Value(orm.EnumExpr(filterTestTable.State)))
	val := expr.AsSelected(cmp)

	var _ expr.Selected[filterTestRecord, recordState] = val

	asc := expr.AscValue(val)
	desc := expr.DescValue(val)
	var _ expr.ProjectionOrder[filterTestRecord] = asc
	var _ expr.ProjectionOrder[filterTestRecord] = desc

	key := expr.Key(val)
	var _ expr.ProjectionKey[filterTestRecord] = key

	core := expr.FoundrySelected(val, sqlowner.Expressions())
	if core.Err != nil {
		t.Fatalf("unexpected error on selected: %v", core.Err)
	}
	if len(core.Refs) != 1 {
		t.Fatalf("expected 1 ref, got %d", len(core.Refs))
	}

	var zero expr.RowComparison[filterTestRecord, recordState]
	zeroVal := expr.AsSelected(zero)
	zeroCore := expr.FoundrySelected(zeroVal, sqlowner.Expressions())
	if zeroCore.Node != nil {
		t.Fatal("expected nil node on zero-value RowComparison.Value()")
	}
}

func TestNullableRowComparisonValue(t *testing.T) {
	cmp := expr.CompareNullableRow(expr.Value(orm.NullableEnumExpr(filterTestTable.Nullable)))
	val := expr.AsSelected(cmp)

	var _ expr.Selected[filterTestRecord, value.Nullable[recordState]] = val

	asc := expr.AscValue(val)
	desc := expr.DescValue(val)
	var _ expr.ProjectionOrder[filterTestRecord] = asc
	var _ expr.ProjectionOrder[filterTestRecord] = desc

	key := expr.Key(val)
	var _ expr.ProjectionKey[filterTestRecord] = key

	core := expr.FoundrySelected(val, sqlowner.Expressions())
	if core.Err != nil {
		t.Fatalf("unexpected error on selected: %v", core.Err)
	}
	if len(core.Refs) != 1 {
		t.Fatalf("expected 1 ref, got %d", len(core.Refs))
	}

	var zero expr.NullableRowComparison[filterTestRecord, recordState]
	zeroVal := expr.AsSelected(zero)
	zeroCore := expr.FoundrySelected(zeroVal, sqlowner.Expressions())
	if zeroCore.Node != nil {
		t.Fatal("expected nil node on zero-value NullableRowComparison.Value()")
	}
}
