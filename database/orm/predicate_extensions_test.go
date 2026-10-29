package orm

import (
	"errors"
	"github.com/weiloon1234/Foundry-Go/fault"
	"reflect"
	"strings"
	"testing"
)

func TestPredicateExtensionsCompile(t *testing.T) {
	f := compilerTable("public.users")
	name, age := f.Name, f.Age
	for _, test := range []struct {
		predicate Predicate[compilerRecord]
		sql       string
		arguments []any
	}{
		{NumberNotIn(age), "WHERE TRUE ", nil},
		{NumberIn(age), "WHERE FALSE ", nil},
		{And(NumberNotIn(age), NumberGt(age, 1)), `WHERE (TRUE AND ("public"."users"."age" > $1))`, []any{int64(1)}},
		{NumberNotIn(age, 1, 2), `WHERE ("public"."users"."age" <> ALL($1))`, []any{"{1,2}"}},
		{NumberNotIn(age), "WHERE TRUE ", nil},
		{NumberBetween(age, 1, 9), `WHERE (("public"."users"."age" >= $1) AND ("public"."users"."age" <= $2))`, []any{int64(1), int64(9)}},
		{TextStartsWith(name, "a_%!"), `WHERE ("public"."users"."name" LIKE $1 ESCAPE '!')`, []any{"a!_!%!!%"}},
		{TextEndsWith(name, "z"), `WHERE ("public"."users"."name" LIKE $1 ESCAPE '!')`, []any{"%z"}},
		{TextIStartsWith(name, "Ab"), `WHERE ("public"."users"."name" ILIKE $1 ESCAPE '!')`, []any{"Ab%"}},
		{TextIEndsWith(name, "Ab"), `WHERE ("public"."users"."name" ILIKE $1 ESCAPE '!')`, []any{"%Ab"}},
		{TextILike(name, "a%"), `WHERE ("public"."users"."name" ILIKE $1)`, []any{"a%"}},
	} {
		statement, err := Compile(f.Query().Where(test.predicate).Limit(1))
		if err != nil {
			t.Fatal(err)
		}
		sql := statement.SQL()
		if !strings.Contains(sql+" ", test.sql) {
			t.Fatalf("missing %q in %s", test.sql, sql)
		}
		arguments := statement.Arguments()
		if !reflect.DeepEqual(arguments[:len(arguments)-1], append([]any{}, test.arguments...)) {
			t.Fatalf("%s bound %v", sql, arguments)
		}
	}
	statement, err := Compile(f.Query().OrderBy(NumberAsc(age).NullsFirst(), TextDesc(name).NullsLast()))
	if err != nil || !strings.HasSuffix(statement.SQL(), `ORDER BY "public"."users"."age" ASC NULLS FIRST, "public"."users"."name" DESC NULLS LAST`) {
		t.Fatal("explicit NULL placement lost", statement.SQL(), err)
	}
	// Empty native junctions cannot invent a declaration owner. Owned empty
	// membership retains the legacy TRUE/FALSE SQL identities above.
	for _, p := range []Predicate[compilerRecord]{And[compilerRecord](), Or[compilerRecord]()} {
		if _, err := Compile(f.Query().Where(p)); !errors.Is(err, fault.Invalid) {
			t.Fatal("ownerless junction accepted", err)
		}
	}
	r := records()
	if _, err := newCursorPlan(r.Query().OrderBy(NullableTextAsc(r.Note).NullsFirst()).state, 1, "", false, false); err == nil || !strings.Contains(err.Error(), "NULL") {
		t.Fatal("cursor accepted explicit NULL placement", err)
	}
}
