package querycore

import (
	"database/sql/driver"
	"strings"
	"testing"
)

func TestKeysetPredicateUsesRowComparisonOnlyForNotNullKeys(t *testing.T) {
	columns := []Column{{Name: "id"}, {Name: "rank", Nullable: true}}
	id, rank := FieldRef{Table: "records", Column: "id"}, FieldRef{Table: "records", Column: "rank"}
	keys := []driver.Value{int64(7), int64(42)}
	for _, test := range []struct {
		name     string
		orders   []QueryOrder
		nullable bool
		want     string
		forbid   []string
	}{
		{"ascending", []QueryOrder{{Field: rank}, {Field: id}}, false, `(("records"."rank", "records"."id") > ($1, $2))`, nil},
		{"descending", []QueryOrder{{Field: rank, Descending: true}, {Field: id, Descending: true}}, false, `(("records"."rank", "records"."id") < ($1, $2))`, nil},
		{"mixed", []QueryOrder{{Field: rank}, {Field: id, Descending: true}}, false, `"records"."rank" >`, []string{") > (", "IS NULL"}},
		{"nullable", []QueryOrder{{Field: rank}, {Field: id}}, true, `"records"."rank" IS NULL`, []string{") > ("}},
	} {
		t.Run(test.name, func(t *testing.T) {
			c := NewCompiler(columns)
			c.Sources = map[string]map[string]Column{"records": c.Columns}
			s, e := c.Expression(CursorPredicate(test.orders, keys, func(o QueryOrder) bool { return test.nullable && o.Field.Column == "rank" }))
			if e != nil || !strings.Contains(s, test.want) {
				t.Fatal(s, e)
			}
			for _, f := range test.forbid {
				if strings.Contains(s, f) {
					t.Fatal(s)
				}
			}
		})
	}
}
