package query

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
)

func TestProjectionDerivedSourcesPreserveOutputAndWindows(t *testing.T) {
	id := NewOrderedField[cursorRecord, int64]("records", "id", codec.Signed[int64]())
	outputID := NewProjectionField[reportRecord, int64]("id")
	outputCount := NewProjectionField[reportRecord, int64]("count")
	report := Project(cursorQuery().Where(id.Gt(2)), reportDefinition(), Map(outputID, id.Value()), Map(outputCount, Count[cursorRecord]().Value())).GroupBy(id.Group()).Having(Count[cursorRecord]().Gt(3)).OrderBy(Count[cursorRecord]().Desc()).Limit(4)
	source := As[firstAlias](report, "totals")
	count := NewOrderedField[Alias[firstAlias, reportRecord], int64](source.Scope().Table(), "count", codec.Signed[int64]())
	s, err := SelectValue(source, count.Value()).Where(count.Gt(5)).Limit(6).Compile()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(s.SQL(), `FROM (SELECT "records"."id" AS "id", COUNT(*) AS "count"`) || !strings.Contains(s.SQL(), `HAVING (COUNT(*) > $2) ORDER BY COUNT(*) DESC LIMIT $3) AS "totals" WHERE ("totals"."count" > $4) LIMIT $5`) || !reflect.DeepEqual(s.Arguments(), []any{int64(2), int64(3), int64(4), int64(5), int64(6)}) {
		t.Fatal("derived output/filter/window mismatch", s.SQL(), s.Arguments())
	}
	bad := As[firstAlias](Project(cursorQuery(), reportDefinition()), "bad")
	if _, err := SelectValue(bad, count.Value()).Compile(); !errors.Is(err, fault.Invalid) {
		t.Fatal("incomplete derived projection accepted", err)
	}
	// Original model fields are not visible outside the declared result boundary.
	forged := NewScalarField[Alias[firstAlias, reportRecord], int64]("records", "id", codec.Signed[int64]())
	if _, err := SelectValue(source, forged.Value()).Compile(); !errors.Is(err, fault.Invalid) {
		t.Fatal("inner model field leaked outside projection", err)
	}
	a := As[firstAlias](cursorQuery(), "a")
	selected := Nullable(scopedID(a.Scope()).Value())
	columns := pairProjection(a, selected, selected).definition.sourceColumns()
	if len(columns) != 2 || !columns[0].Nullable || !columns[1].Nullable || report.definition.sourceColumns()[0].Nullable {
		t.Fatal("derived columns lost declared nullability")
	}
}
