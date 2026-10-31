package datatable

import (
	"reflect"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
)

func TestComputedComparisonFilterRequests(t *testing.T) {
	// Reuse the reporting scope so client filters compile alongside its mandatory tenant predicate.
	spec := reportSpec()
	_, name, note, _ := reportFields()
	length := expr.Length(name)
	nullableLength := expr.LengthNullable(note)
	spec.Filters = []FilterRegistration[reportRecord]{
		DefineFilter("length", "reports.length", Where(foundryhttp.IntegerQuery[int64](), expr.CompareFilter(length))),
		DefineFilter("noteLength", "reports.note_length", NullableWhere(foundryhttp.IntegerQuery[int64](), expr.NullableCompareFilter(nullableLength))),
	}
	table := Define(spec)
	if err := table.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name     string
		filter   Filter
		fragment string
		args     []any
	}{
		{"equal", Filter{Op: Equal, Column: "length", Values: []string{"0"}}, `CAST(CHAR_LENGTH(CAST("report_rows"."name" AS text)) AS bigint) = $2`, []any{int64(7), int64(0)}},
		{"membership", Filter{Op: In, Column: "length", Values: []string{"3", "5"}}, `CAST(CHAR_LENGTH(CAST("report_rows"."name" AS text)) AS bigint) IN ($2, $3)`, []any{int64(7), int64(3), int64(5)}},
		{"nullable equality", Filter{Op: Equal, Column: "noteLength", Values: []string{"0"}}, `CAST(CHAR_LENGTH(CAST("report_rows"."note" AS text)) AS bigint) = $2`, []any{int64(7), int64(0)}},
		{"nullable inequality", Filter{Op: NotEqual, Column: "noteLength", Values: []string{"3"}}, `((CAST(CHAR_LENGTH(CAST("report_rows"."note" AS text)) AS bigint) <> $2)) OR ((CAST(CHAR_LENGTH(CAST("report_rows"."note" AS text)) AS bigint) IS NULL))`, []any{int64(7), int64(3)}},
		{"null", Filter{Op: IsNull, Column: "noteLength"}, `CAST(CHAR_LENGTH(CAST("report_rows"."note" AS text)) AS bigint) IS NULL`, []any{int64(7)}},
		{"present", Filter{Op: IsNotNull, Column: "noteLength"}, `CAST(CHAR_LENGTH(CAST("report_rows"."note" AS text)) AS bigint) IS NOT NULL`, []any{int64(7)}},
	} {
		t.Run(test.name, func(t *testing.T) {
			prepared, err := table.definition.prepare(Request{Filters: []Filter{test.filter}}, DefaultConfig())
			if err != nil {
				t.Fatal(err)
			}
			source, err := table.definition.scoped(t.Context(), reportActor{Tenant: 7, Allowed: true}, QueryAction, prepared)
			if err != nil {
				t.Fatal(err)
			}
			statement, err := rows.CompileRead(rows.Read(source.rows))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(statement.SQL(), test.fragment) || !reflect.DeepEqual(statement.Arguments(), test.args) {
				t.Fatalf("SQL: %s\narguments: %#v\nwant fragment: %s\narguments: %#v", statement.SQL(), statement.Arguments(), test.fragment, test.args)
			}
		})
	}
	for _, op := range []Operator{Greater, Contains} {
		if _, err := table.definition.prepare(Request{Filters: []Filter{{Op: op, Column: "length", Values: []string{"3"}}}}, DefaultConfig()); err == nil {
			t.Fatal("equality adapter granted an extra capability", op)
		}
	}
}
