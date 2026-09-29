package query

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
)

func modelQuery() Query[user] {
	return ForModel(Define("public.users", "id", []Column{{Name: "id"}, {Name: "name"}, {Name: "age"}}, func(database.Row) (user, error) { return user{}, nil }))
}

func TestCompilerBindsValuesQuotesNamesAndPreservesPrecedence(t *testing.T) {
	name := NewTextField[user, string]("public.users", "name", codec.String[string]())
	age := NewOrderedField[user, int]("public.users", "age", codec.Signed[int]())
	q := modelQuery().Where(Or(name.Contains("a_%!'"), age.In(18, 21)).Not(), age.Gte(1)).OrderBy(name.Desc()).Limit(10).Offset(2)
	compiled, err := q.Compile()
	if err != nil {
		t.Fatal(err)
	}
	expected := `SELECT "public"."users"."id", "public"."users"."name", "public"."users"."age" FROM "public"."users" WHERE (NOT (("public"."users"."name" LIKE $1 ESCAPE '!') OR ("public"."users"."age" = ANY($2)))) AND ("public"."users"."age" >= $3) ORDER BY "public"."users"."name" DESC LIMIT $4 OFFSET $5`
	if compiled.SQL() != expected {
		t.Fatalf("unexpected SQL:\n%s", compiled.SQL())
	}
	if !reflect.DeepEqual(compiled.Arguments(), []any{"%a!_!%!!'%", "{18,21}", int64(1), int64(10), int64(2)}) {
		t.Fatal("binding order/escaping changed")
	}
	for _, format := range []string{"%v", "%+v", "%#v"} {
		if strings.Contains(fmt.Sprintf(format, compiled), "a!_") {
			t.Fatal("statement formatting exposed bindings")
		}
	}
	args := compiled.Arguments()
	args[0] = "changed"
	if compiled.Arguments()[0] == "changed" {
		t.Fatal("argument snapshot aliases statement")
	}
	empty, err := modelQuery().Where(age.In().Not()).Compile()
	if err != nil || !strings.HasSuffix(empty.SQL(), " WHERE (NOT FALSE)") || len(empty.Arguments()) != 0 {
		t.Fatal("empty membership semantics changed")
	}
}

func TestCompilerRejectsInvalidBoundariesAndResources(t *testing.T) {
	age := NewOrderedField[user, int]("public.users", "age", codec.Signed[int]())
	deep := age.Eq(1)
	for range MaxExpressionDepth + 1 {
		deep = deep.Not()
	}
	badCodec := codec.String[string]().Validated(func(string) error { return fault.New(fault.Invalid, "invalid enum") })
	for name, q := range map[string]Query[user]{
		"missing metadata":    For[user]("public.users"),
		"negative limit":      modelQuery().Limit(-1),
		"negative offset":     modelQuery().Offset(-1),
		"undeclared column":   modelQuery().Where(NewScalarField[user, int]("public.users", "typo", codec.Signed[int]()).Eq(1)),
		"undeclared order":    modelQuery().OrderBy(NewScalarField[user, int]("public.users", "typo", codec.Signed[int]()).Asc()),
		"invalid binding":     modelQuery().Where(NewTextField[user, string]("public.users", "name", badCodec).Eq("invalid")),
		"deep predicate":      modelQuery().Where(deep),
		"too many parameters": modelQuery().Where(NewScalarField[user, float64]("public.users", "age", codec.Float[float64]()).In(make([]float64, MaxParameters)...)).Limit(1),
		"undeclared codec":    modelQuery().Where(NewScalarField[user, int]("public.users", "age", codec.Codec[int]{}).Eq(1)),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := q.Compile(); !errors.Is(err, fault.Invalid) {
				t.Fatalf("invalid query compiled: %v", err)
			}
		})
	}
	for _, definition := range []Definition[user]{
		Define("users", "missing", []Column{{Name: "id"}}, func(database.Row) (user, error) { return user{}, nil }),
		Define("users", "id", []Column{{Name: "id"}, {Name: "id"}}, func(database.Row) (user, error) { return user{}, nil }),
		Define("users", "id", []Column{{Name: "id"}}, (func(database.Row) (user, error))(nil)),
		Define(strings.Repeat("a", 64), "id", []Column{{Name: "id"}}, func(database.Row) (user, error) { return user{}, nil }),
	} {
		if err := definition.Validate(); !errors.Is(err, fault.Invalid) {
			t.Fatal("invalid model definition accepted")
		}
	}
}

func TestConcurrentQueryDerivationAndWindowAggregates(t *testing.T) {
	name := NewTextField[user, string]("public.users", "name", codec.String[string]())
	base := modelQuery().Where(name.Eq("unchanged"))
	var wg sync.WaitGroup
	for i := range 16 {
		wg.Go(func() {
			q := base.Limit(i).Offset(i).OrderBy(name.Asc())
			for _, kind := range []readKind{readModels, readCount, readExists} {
				compiled, err := q.compile(kind)
				if err != nil {
					t.Error(err)
					return
				}
				arguments := compiled.Arguments()
				limit := i
				if kind == readExists && limit > 1 {
					limit = 1
				}
				if arguments[0] != "unchanged" || arguments[1] != int64(limit) {
					t.Error("query window changed")
				}
				if kind == readCount && !strings.HasPrefix(compiled.SQL(), "SELECT COUNT(*) FROM (") {
					t.Error("count ignored selected window")
				}
			}
		})
	}
	wg.Wait()
	compiled, err := base.Compile()
	if err != nil || len(compiled.Arguments()) != 1 || strings.Contains(compiled.SQL(), "LIMIT") {
		t.Fatal("base query changed")
	}
}
