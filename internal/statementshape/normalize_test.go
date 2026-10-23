package statementshape

import "testing"

func TestGroupingOnly(t *testing.T) {
	for _, pair := range [][2]string{
		{"select id from t where id in (1,2)", "select id from t where id in (3)"},
		{"values (1,2),(3,4)", "values (5,6)"},
		{"select 1 -- x\r from t", "select 2 from t"},
	} {
		a, ok := Normalize(pair[0])
		b, other := Normalize(pair[1])
		if !ok || !other || a != b {
			t.Fatal(a, b, ok, other)
		}
	}
	for _, pair := range [][2]string{{"values (array[1,2])", "values (1)"}, {"values (1,2)", "values (1)"}} {
		a, _ := Normalize(pair[0])
		b, _ := Normalize(pair[1])
		if a == b {
			t.Fatal(a)
		}
	}
}
