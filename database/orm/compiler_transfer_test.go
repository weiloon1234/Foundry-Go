package orm

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
)

type compilerRecord struct {
	ID   int64
	Name string
	Age  int
}
type compilerFields struct {
	Table[compilerRecord, int64]
	Name Text[compilerRecord, string]
	Age  Number[compilerRecord, int]
}

func compilerTable(name string) compilerFields {
	return Define(name, func(b Builder[compilerRecord], m *compilerRecord) compilerFields {
		return compilerFields{TableOf(b, &m.ID), TextOf(b, &m.Name), NumberOf(b, &m.Age)}
	})
}

func TestQueryAndBindingsAreIndependent(t *testing.T) {
	f := compilerTable("users")
	values := []int{1, 2}
	p := f.Age.In(values...)
	values[0] = 99
	s, err := Compile(f.Query().Where(p))
	if err != nil || !reflect.DeepEqual(s.Arguments(), []any{"{1,2}"}) {
		t.Fatal("membership borrowed inputs", s.Arguments(), err)
	}
	base := f.Query().Where(f.Name.Eq("base")).OrderBy(f.Name.Asc())
	var group sync.WaitGroup
	for i := range 32 {
		group.Go(func() {
			q := base.Where(f.Age.Gte(i)).OrderBy(f.Age.Desc())
			if _, err := Compile(q); err != nil {
				t.Error(err)
			}
			if len(q.state.ast.Predicates) != 2 || len(q.state.ast.Orders) != 2 {
				t.Error("derived query lost expressions")
			}
		})
	}
	group.Wait()
	if len(base.state.ast.Predicates) != 1 || len(base.state.ast.Orders) != 1 {
		t.Fatal("base query mutated")
	}
	if _, err := Compile(f.Query().Where(Or(f.Name.Contains("literal_%"), Not(And(f.Age.Gt(1), f.Age.Lt(10)))))); err != nil {
		t.Fatal(err)
	}
}
func TestCompilerBindsValuesQuotesNamesAndPreservesPrecedence(t *testing.T) {
	f := compilerTable("public.users")
	q := f.Query().Where(Not(Or(f.Name.Contains("a_%!'"), f.Age.In(18, 21))), f.Age.Gte(1)).OrderBy(f.Name.Desc()).Limit(10).Offset(2)
	compiled, err := Compile(q)
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
			t.Fatal("statement exposed bindings")
		}
	}
	args := compiled.Arguments()
	args[0] = "changed"
	if compiled.Arguments()[0] == "changed" {
		t.Fatal("argument snapshot aliases statement")
	}
	empty, err := Compile(f.Query().Where(Not(f.Age.In())))
	if err != nil || !strings.HasSuffix(empty.SQL(), " WHERE (NOT FALSE)") || len(empty.Arguments()) != 0 {
		t.Fatal("empty membership semantics changed", empty.SQL(), err)
	}
}
func TestInvalidDeclarationBoundaries(t *testing.T) {
	f := compilerTable("users")
	other := compilerTable("orders")
	for name, q := range map[string]Query[compilerRecord]{"zero": {}, "unsafe table": compilerTable("users;drop table users").Query(), "zero predicate": f.Query().Where(Predicate[compilerRecord]{}), "zero column": f.Query().Where(Number[compilerRecord, int]{}.Eq(1)), "foreign column": f.Query().Where(other.Age.Eq(1)), "zero order": f.Query().OrderBy(Order[compilerRecord]{})} {
		t.Run(name, func(t *testing.T) {
			if _, err := Compile(q); !errors.Is(err, fault.Invalid) {
				t.Fatal("invalid declaration accepted", err)
			}
		})
	}
	type unsafeRecord struct {
		ID  int64
		Bad int `foundry:"column=id;--"`
	}
	invalid := Define("users", func(b Builder[unsafeRecord], m *unsafeRecord) Table[unsafeRecord, int64] {
		NumberOf(b, &m.Bad)
		return TableOf(b, &m.ID)
	})
	if _, err := Compile(invalid.Query()); !errors.Is(err, fault.Invalid) {
		t.Fatal("unsafe column accepted", err)
	}
	nullable := Define("public.records", func(b Builder[record], r *record) recordTable {
		table := TableOf(b, &r.ID)
		return recordTable{Table: table, ID: table.Key(), Name: TextOf(b, &r.Name), Note: NullableTextOf(b, &r.Note), Amount: NumberOf(b, &r.Amount)}
	})
	if statement, err := Compile(nullable.Query().Where(nullable.Note.IsNull())); err != nil || !strings.Contains(statement.SQL(), `"public"."records"."note" IS NULL`) {
		t.Fatal("qualified nullable field rejected", statement.SQL(), err)
	}

}
func TestCompilerRejectsInvalidBoundariesAndResources(t *testing.T) {
	f := compilerTable("users")
	deep := f.Age.Eq(1)
	for range querycore.MaxExpressionDepth + 1 {
		deep = Not(deep)
	}
	bad := Define("bad", func(b Builder[compilerRecord], m *compilerRecord) compilerFields {
		return compilerFields{TableOf(b, &m.ID), TextOf(b, &m.Name, Codec(codec.String[string]().Validated(func(string) error { return fault.New(fault.Invalid, "invalid enum") }))), NumberOf(b, &m.Age)}
	})
	zeroCodec := Define("zero_codec", func(b Builder[compilerRecord], m *compilerRecord) compilerFields {
		return compilerFields{TableOf(b, &m.ID), TextOf(b, &m.Name, Codec(codec.Codec[string]{})), NumberOf(b, &m.Age)}
	})
	for name, q := range map[string]Query[compilerRecord]{"zero codec": zeroCodec.Query(), "negative limit": f.Query().Limit(-1), "negative offset": f.Query().Offset(-1), "deep": f.Query().Where(deep), "bad codec": bad.Query().Where(bad.Name.Eq("invalid"))} {
		t.Run(name, func(t *testing.T) {
			if _, err := Compile(q); !errors.Is(err, fault.Invalid) {
				t.Fatal("invalid boundary accepted", err)
			}
		})
	}
}
func TestConcurrentQueryDerivationAndWindowAggregates(t *testing.T) {
	f := compilerTable("public.users")
	base := f.Query().Where(f.Name.Eq("unchanged"))
	var group sync.WaitGroup
	for i := range 16 {
		group.Go(func() {
			q := base.Limit(i).Offset(i).OrderBy(f.Name.Asc())
			for _, kind := range []querycore.ReadKind{querycore.ReadModels, querycore.ReadCount, querycore.ReadExists} {
				statement, err := compileRead(q.state, kind)
				if err != nil {
					t.Error(err)
					return
				}
				limit := i
				if kind == querycore.ReadExists && limit > 1 {
					limit = 1
				}
				if statement.Bindings[0] != "unchanged" || statement.Bindings[1] != int64(limit) {
					t.Error("query window changed")
				}
				if kind == querycore.ReadCount && !strings.HasPrefix(statement.SQLText, "SELECT COUNT(*) FROM (") {
					t.Error("count ignored window")
				}
			}
		})
	}
	group.Wait()
	s, err := Compile(base)
	if err != nil || len(s.Arguments()) != 1 || strings.Contains(s.SQL(), "LIMIT") {
		t.Fatal("base changed", err)
	}
}
