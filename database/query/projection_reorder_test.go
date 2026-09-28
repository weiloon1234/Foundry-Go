package query

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
)

func TestReportReorderingReplacesOnlyOuterOrders(t *testing.T) {
	base := cursorQuery()
	id := NewOrderedField[cursorRecord, int64]("records", "id", codec.Signed[int64]())
	report := SelectRecord(base.Where(id.Gt(4)), base.Scope()).OrderBy(id.Desc())
	before, err := report.Compile()
	if err != nil {
		t.Fatal(err)
	}
	after, err := report.ReorderUnwindowed(id.Asc()).Compile()
	if err != nil || !strings.HasSuffix(after.SQL(), `ORDER BY "records"."id" ASC`) || !reflect.DeepEqual(after.Arguments(), []any{int64(4)}) {
		t.Fatal("outer ordering was appended or scope changed", after.SQL(), err)
	}
	unchanged, _ := report.Compile()
	if before.SQL() != unchanged.SQL() || !reflect.DeepEqual(before.Arguments(), unchanged.Arguments()) {
		t.Fatal("reordering mutated source")
	}
	for name, bad := range map[string]ProjectionQuery[cursorRecord, cursorRecord]{
		"limit": report.Limit(1), "zero limit": report.Limit(0), "offset": report.Offset(1),
		"distinct on": report.DistinctOn(id.Group()), "zero": {},
	} {
		if compiled, err := bad.ReorderUnwindowed(id.Asc()).Compile(); !errors.Is(err, fault.Invalid) || compiled.SQL() != "" {
			t.Fatal("unsafe report source accepted", name, err)
		}
	}
	if _, err := report.ReorderUnwindowed().Compile(); !errors.Is(err, fault.Invalid) {
		t.Fatal("unstable empty order accepted", err)
	}
	if compiled, err := report.Distinct().ReorderUnwindowed(id.Asc()).Compile(); err != nil || !strings.HasPrefix(compiled.SQL(), "SELECT DISTINCT ") {
		t.Fatal("ordinary DISTINCT was lost", err)
	}
}

func TestReportReorderingPreservesMaterializedWinnerSelection(t *testing.T) {
	base := cursorQuery()
	id := NewOrderedField[cursorRecord, int64]("records", "id", codec.Signed[int64]())
	winners := base.Where(id.Gt(2)).DistinctOn(id.Group()).OrderBy(id.Desc()).Limit(5)
	source := As[firstAlias](winners, "winners")
	selected := scopedID(source.Scope())
	report := SelectRecord(source, source.Scope()).OrderBy(selected.Desc()).ReorderUnwindowed(selected.Asc())
	statement, err := report.Compile()
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{`DISTINCT ON ("records"."id")`, `ORDER BY "records"."id" DESC LIMIT $2) AS "winners"`, `ORDER BY "winners"."id" ASC`} {
		if !strings.Contains(statement.SQL(), fragment) {
			t.Fatal("materialized winner selection changed", statement.SQL())
		}
	}
	if !reflect.DeepEqual(statement.Arguments(), []any{int64(2), int64(5)}) {
		t.Fatal("inner window bindings changed")
	}
}
