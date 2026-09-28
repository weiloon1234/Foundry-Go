package query

import (
	"errors"
	"reflect"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
)

type reportRecord struct {
	ID    int64
	Count int64
}

func reportDefinition() ProjectionDefinition[reportRecord] {
	id := NewProjectionField[reportRecord, int64]("id")
	count := NewProjectionField[reportRecord, int64]("count")
	return DefineProjection([]ProjectionColumn[reportRecord]{id.Column(), count.Column()}, func(database.Row) (reportRecord, error) { return reportRecord{}, nil })
}
func TestProjectionCompleteTypedMappingAndSourceIsolation(t *testing.T) {
	field := cursorQuery().OrderBy(Order[cursorRecord]{field: fieldRef{"records", "id"}})
	id := NewProjectionField[reportRecord, int64]("id")
	count := NewProjectionField[reportRecord, int64]("count")
	input := Expression[cursorRecord, int64]{node: fieldRef{"records", "id"}}
	group := Group[cursorRecord]{node: fieldRef{"records", "id"}}
	mappings := []ProjectionMapping[cursorRecord, reportRecord]{Map(count, Count[cursorRecord]().Value()), Map(id, input)}
	q := Project(field, reportDefinition(), mappings...).GroupBy(group)
	mappings[0] = ProjectionMapping[cursorRecord, reportRecord]{}
	s, err := q.Compile()
	if err != nil {
		t.Fatal(err)
	}
	if s.SQL() != `SELECT "records"."id" AS "id", COUNT(*) AS "count" FROM "records" GROUP BY "records"."id" ORDER BY "records"."id" ASC` {
		t.Fatal("mapping order changed output decoding", s.SQL())
	}
	limited, err := q.Limit(2).Offset(1).Compile()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(limited.Arguments(), []any{int64(2), int64(1)}) {
		t.Fatal("projection window parameters changed")
	}
	unchanged, _ := q.Compile()
	if unchanged.SQL() != s.SQL() {
		t.Fatal("derived projection mutated source")
	}
	badType := NewProjectionField[reportRecord, string]("id")
	badValue := Expression[cursorRecord, string]{node: fieldRef{"records", "id"}}
	for _, q := range []ProjectionQuery[cursorRecord, reportRecord]{
		Project(field, reportDefinition(), Map(id, input)),
		Project(field, reportDefinition(), Map(id, input), Map(id, input)),
		Project(field, reportDefinition(), Map(id, input), Map(count, Expression[cursorRecord, int64]{})),
		Project(field, reportDefinition(), Map(badType, badValue), Map(count, Count[cursorRecord]().Value())),
		Project(field, reportDefinition(), Map(id, input), Map(count, Count[cursorRecord]().Value())),
		q.GroupBy(group), q.Limit(-1),
		Project(cursorQuery().With(testRelation(cursorQuery(), cursorQuery())), reportDefinition(), Map(id, input), Map(count, Count[cursorRecord]().Value())),
	} {
		if _, err := q.Compile(); !errors.Is(err, fault.Invalid) {
			t.Fatal("invalid projection mapping/grouping accepted", err)
		}
	}
}
