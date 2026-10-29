package codemod

import (
	"bytes"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

func ormFixture(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestORMGolden(t *testing.T) {
	for _, test := range []struct {
		name, dependency string
		count            int
	}{
		{"orm_operators", "orm_operators_v01.go", 397},
		{"orm", "orm_v01.go", 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			source := ormFixture(t, test.name+".old.go")
			expected, err := format.Source([]byte(ormFixture(t, test.name+".new.go")))
			if err != nil {
				t.Fatal(err)
			}
			output, entries := rewriteRulesTest(t, source, "consumer.go", map[string]string{ormPath: ormFixture(t, test.dependency)}, ORM{})
			if !bytes.Equal(output, expected) {
				t.Fatalf("got:\n%s\nwant:\n%s", output, expected)
			}
			if test.count > 0 && len(entries) != test.count {
				t.Fatalf("got %d mappings, want %d", len(entries), test.count)
			}
			for _, entry := range entries {
				if entry.Status != Resolved {
					t.Fatal(entry)
				}
			}
		})
	}
}

const ormExprV01 = `package expr
import o "github.com/weiloon1234/Foundry-Go/database/orm"
func Value[M,V any](c o.ExpressionColumn[M,V]) int { return 0 }
func GreaterColumn[M,V any](a,b o.ExpressionColumn[M,V]) int { return 0 }
func At[M,V any](scope int,c o.ExpressionColumn[M,V]) int { return 0 }
`
const ormRowsV01 = `package rows
import o "github.com/weiloon1234/Foundry-Go/database/orm"
func SelectValue[M,V any](q o.Query[M],c o.ExpressionColumn[M,V]) int { return 0 }
func MapInsert[M,I,V any,C interface{ o.ExpressionColumn[M,V]; Set(I) int }](c C,expression I) int { return 0 }
func MapUpdate[M,I,V any,C interface{ o.ExpressionColumn[M,V]; Set(I) int }](c C,expression I) int { return 0 }
`

func TestORMAdaptersGolden(t *testing.T) {
	dependencies := map[string]string{ormPath: ormFixture(t, "orm_v01.go"), ormPath + "/expr": ormExprV01, ormPath + "/rows": ormRowsV01}
	source := ormFixture(t, "orm_adapters.old.go")
	expected, err := format.Source([]byte(ormFixture(t, "orm_adapters.new.go")))
	if err != nil {
		t.Fatal(err)
	}
	output, entries := rewriteRulesTest(t, source, "consumer.go", dependencies, ORM{})
	if !bytes.Equal(output, expected) {
		t.Fatalf("got:\n%s\nwant:\n%s", output, expected)
	}
	if len(entries) != 6 {
		t.Fatal(entries)
	}
	for _, entry := range entries {
		if entry.Status != Resolved {
			t.Fatal(entry)
		}
	}
}

func TestORMReceiverCapture(t *testing.T) {
	source := `package consumer
import o "` + ormPath + `"
func column() o.InputText[int,string,int] { return o.InputText[int,string,int]{} }
func query() o.Query[int] { return o.Query[int]{} }
func effect() int { return 0 }
var setter = column().Set
var terminal = query().All
var result = query().All(effect(),2)
var _ = o.WriteGet(1,column().Set)
`
	output, entries := rewriteRulesTest(t, source, "consumer.go", map[string]string{ormPath: ormFixture(t, "orm_v01.go")}, ORM{})
	for _, entry := range entries {
		if entry.Status != Resolved {
			t.Fatal(entry)
		}
	}
	if len(entries) != 4 {
		t.Fatal(entries)
	}
	file, err := parser.ParseFile(token.NewFileSet(), "out.go", output, 0)
	if err != nil {
		t.Fatal(err)
	}
	captures := 0
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		if _, ok := call.Fun.(*ast.FuncLit); ok {
			captures++
			if len(call.Args) != 1 {
				t.Fatal("capture must evaluate one receiver", string(output))
			}
		}
		return true
	})
	if captures != 4 || !strings.Contains(string(output), "o.All(foundryArg0, foundryArg1, foundryBound)") || !strings.Contains(string(output), "o.InputTextSet(foundryBound, foundryArg0)") {
		t.Fatal(string(output))
	}
	// Each effectful source expression survives once, plus its declaration.
	if strings.Count(string(output), "column()") != 3 || strings.Count(string(output), "query()") != 3 || strings.Count(string(output), "effect()") != 2 {
		t.Fatal(string(output))
	}
}

func TestORMUnresolved(t *testing.T) {
	for _, body := range []string{
		`func use(c *o.Text[int,string]) { _ = c.Eq("x") }`,
		`type Wrapped struct { o.Text[int,string] }; func use(c Wrapped) { _ = c.Eq("x") }`,
		`type Mid struct { o.Table[int,int] }; type Outer struct { Mid }; func use(t Outer) { _ = t.Find(1,2,3) }`,
		`func side() int { return 1 };func use(t o.Table[int,int]) { _ = t.Update(1,2,side()) }`,
		`var _ interface{Eq(string) int} = o.Text[int,string]{}`,
		`func helper[C interface{ Eq(string) int }](c C) {}; var _ = helper[o.Text[int,string]]`,
		`func helper[C o.ExpressionColumn[int,string]](c C) { _ = o.ConflictKey(c) }`,
		`var _ = o.ConflictSet[int,int,string,o.InputText[int,int,string]](o.InputText[int,int,string]{},1)`,
		`func use(c o.Text[int,string]) { o:=1; _=o; _=c.Eq("x") }`,
	} {
		t.Run(body, func(t *testing.T) {
			source := "package consumer\nimport o \"" + ormPath + "\"\n" + body
			output, entries := rewriteRulesTest(t, source, "consumer.go", map[string]string{ormPath: ormFixture(t, "orm_v01.go")}, ORM{})
			if len(entries) == 0 {
				t.Fatal("missing unresolved finding")
			}
			for _, entry := range entries {
				if entry.Status != Unresolved || entry.Message == "" {
					t.Fatal(entry)
				}
			}
			if string(output) != source {
				t.Fatalf("unresolved source changed:\n%s", output)
			}
		})
	}
}

func TestORMIdentityAndGenerated(t *testing.T) {
	source := `package consumer
type Text struct{}
func (Text) Eq(int) int { return 0 }
var _ = Text{}.Eq(1)
`
	output, entries := rewriteRulesTest(t, source, "consumer.go", nil, ORM{})
	if len(entries) != 0 || string(output) != source {
		t.Fatal(entries, string(output))
	}
	for _, name := range []string{"consumer.go", "schema_foundry.gen.go"} {
		source := "// Code generated by schema. DO NOT EDIT.\npackage consumer\nimport o \"" + ormPath + "\"\nvar _ = o.Text[int,string]{}.Eq(\"x\")\n"
		output, entries := rewriteRulesTest(t, source, name, map[string]string{ormPath: ormFixture(t, "orm_v01.go")}, ORM{})
		if len(entries) != 1 || entries[0].Status != Regenerate || string(output) != source {
			t.Fatal(entries)
		}
	}
}

func TestORMMethodExpressions(t *testing.T) {
	source := `package consumer
import o "` + ormPath + `"
var op = o.InputText[int,string,int].Set
var all = o.Query[int].All
var result = o.Query[int].All(o.Query[int]{},1,2)
`
	output, entries := rewriteRulesTest(t, source, "consumer.go", map[string]string{ormPath: ormFixture(t, "orm_v01.go")}, ORM{})
	if len(entries) != 3 {
		t.Fatal(entries)
	}
	for _, entry := range entries {
		if entry.Status != Resolved {
			t.Fatal(entry)
		}
	}
	if !strings.Contains(string(output), "o.InputTextSet[int, string, int]") || strings.Count(string(output), "o.All(foundryArg1, foundryArg2, foundryArg0)") != 2 {
		t.Fatal(string(output))
	}
}

func TestORMScopedWriteDoesNotUseConsumerOverrides(t *testing.T) {
	source := `package consumer
import o "` + ormPath + `"
type Tables struct { o.Table[int,int] }
func (Tables) Query() o.Query[int] { panic("consumer override") }
func (Tables) Key() o.Text[int,int] { panic("consumer override") }
var table Tables
var _ = table.Update(1,2,3,4)
`
	output, entries := rewriteRulesTest(t, source, "consumer.go", map[string]string{ormPath: ormFixture(t, "orm_v01.go")}, ORM{})
	if len(entries) != 1 || entries[0].Status != Resolved || !strings.Contains(string(output), "o.Update(1, 2, table.Table.Query(), table.Table.Key(), 3, 4)") {
		t.Fatal(entries, string(output))
	}
}

func TestORMTerminalCapturesBeforeIndexing(t *testing.T) {
	source := `package consumer
import o "` + ormPath + `"
func query() o.Query[int] { return o.Query[int]{} }
var values []int
var _ = query().All(values[0],2)
`
	output, entries := rewriteRulesTest(t, source, "consumer.go", map[string]string{ormPath: ormFixture(t, "orm_v01.go")}, ORM{})
	if len(entries) != 1 || entries[0].Status != Resolved || !strings.Contains(string(output), "}(query())(values[0], 2)") {
		t.Fatal(entries, string(output))
	}
}
