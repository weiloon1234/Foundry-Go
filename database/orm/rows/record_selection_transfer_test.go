package rows

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"errors"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/ormcore"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
	"reflect"
	"strings"
	"testing"
	"unsafe"
)

func TestSelectRecordKeepsColumnsWindowsAndJoinMultiplicity(t *testing.T) {
	m := declareMembers()
	a, b := As[left](m.Query().Where(orm.KeyNe(m.ID, 2)).Limit(3), "a"), As[right](m.Query(), "b")
	j := InnerJoin(a, b, On(expr.At(Scope(a), orm.KeyExpr(m.ID)), expr.At(Scope(b), orm.KeyExpr(m.ID))))
	scope := LeftScope(j, Scope(a))
	id := expr.At(scope, orm.KeyExpr(m.ID))
	q := SelectRecord(j, scope).Where(expr.Ne(id, 4)).OrderBy(expr.Desc(id)).Limit(5)
	s, e := CompileRead(ReadRecord(q))
	want := `SELECT "a"."id", "a"."name", "a"."nickname" FROM (SELECT "members"."id", "members"."name", "members"."nickname" FROM "members" WHERE ("members"."id" <> $1) LIMIT $2) AS "a" INNER JOIN "members" AS "b" ON ("a"."id" = "b"."id") WHERE ("a"."id" <> $3) ORDER BY "a"."id" DESC LIMIT $4`
	if e != nil || s.SQL() != want || !reflect.DeepEqual(s.Arguments(), []any{int64(2), int64(3), int64(4), int64(5)}) || len(q.core.Record.Columns) != 3 {
		t.Fatal(s.SQL(), s.Arguments(), e)
	}
	_ = q.Where(expr.Eq(id, 9)).Limit(1)
	again, _ := CompileRead(ReadRecord(q))
	if again.SQL() != s.SQL() || !reflect.DeepEqual(again.Arguments(), s.Arguments()) {
		t.Fatal("record mutated")
	}
	combined := Union(ModelRecord(m.Query()), q)
	alias := AsRecord[latestTag](CTE("records_result", combined), "result")
	s, e = CompileRead(ReadRecord(SelectRecord(alias, Scope(alias))))
	if e != nil || !strings.Contains(s.SQL(), "UNION") {
		t.Fatal("record CTE/set composition", e)
	}
	if _, e := CompileRead(ReadRecord(SelectRecord(combined, combined.Scope()))); e != nil {
		t.Fatal("set lost complete record", e)
	}
}
func TestSelectRecordPreservedJoinSides(t *testing.T) {
	m := declareMembers()
	a, b, c := As[left](m.Query(), "a"), As[right](m.Query(), "b"), As[latestTag](m.Query(), "c")
	on := On(expr.At(Scope(a), orm.KeyExpr(m.ID)), expr.At(Scope(b), orm.KeyExpr(m.ID)))
	l, r := LeftJoin(a, b, on), RightJoin(a, b, on)
	if _, e := CompileRead(ReadRecord(SelectRecord(l, LeftScope(l, Scope(a))))); e != nil {
		t.Fatal(e)
	}
	if _, e := CompileRead(ReadRecord(SelectRecord(r, RightScope(r, Scope(b))))); e != nil {
		t.Fatal(e)
	}
	chain := InnerJoin(l, c, On(expr.At(LeftScope(l, Scope(a)), orm.KeyExpr(m.ID)), expr.At(Scope(c), orm.KeyExpr(m.ID))))
	if _, e := CompileRead(ReadRecord(SelectRecord(chain, LeftScope(chain, LeftScope(l, Scope(a)))))); e != nil {
		t.Fatal(e)
	}
}
func TestSelectRecordRejectsInvalidScopesAndLayouts(t *testing.T) {
	m := declareMembers()
	base := From(m.Query())
	scope := ModelScope(m.Query())
	var missing *Input[member]
	for _, q := range []RecordRead[member, member]{SelectRecord(missing, scope), SelectRecord(base, expr.Scope[member, member]{}), SelectRecord(Input[member]{}, scope), SelectRecord(base, scope).Limit(-1)} {
		if s, e := CompileRead(ReadRecord(q)); !errors.Is(e, fault.Invalid) || s.SQL() != "" {
			t.Fatal("invalid record selection", e)
		}
	}
	original := ModelRecord(m.Query())
	data := expr.FoundryScopeData(scope, sqlowner.Expressions())
	for _, mode := range []string{"missing table", "columns", "decoder"} {
		metadata := original.core.Record
		sc := data
		switch mode {
		case "missing table":
			sc.Table = "missing"
		case "columns":
			metadata.Columns = append([]querycore.Column(nil), metadata.Columns...)
			metadata.Columns[0].Name = "wrong"
		case "decoder":
			metadata.Scan = nil
		}
		r := ormcore.SelectRecord(base.source, sc, metadata, reflect.TypeFor[member]())
		if _, e := ormcore.CompileRecord(t.Context(), r); !errors.Is(e, fault.Invalid) {
			t.Fatal(mode, e)
		}
	}
	a, b := As[left](m.Query(), "a"), As[right](m.Query(), "b")
	other := As[left](m.Query(), "other")
	if _, e := CompileRead(ReadRecord(SelectRecord(a, Scope(other)))); !errors.Is(e, fault.Invalid) {
		t.Fatal("wrong same-tag alias", e)
	}
	for _, test := range []struct {
		kind     querycore.JoinKind
		selected string
	}{{querycore.RightJoin, "a"}, {querycore.FullJoin, "a"}, {querycore.LeftJoin, "b"}, {querycore.FullJoin, "b"}} {
		node := a.source.Node
		node.Joins = []querycore.JoinNode{{Source: b.source.Node.Source, Kind: test.kind, On: querycore.BinaryComparison{Left: querycore.FieldRef{Table: "a", Column: "id"}, Right: querycore.FieldRef{Table: "b", Column: "id"}, Operator: querycore.Equal}}}
		if _, e := querycore.SelectRecordNode(node, test.selected, original.core.Record.Columns, true); !errors.Is(e, fault.Invalid) {
			t.Fatal("nullable side became complete record", test, e)
		}

	}
}
func TestSelectRecordReusesOriginalDecoder(t *testing.T) {
	m := declareMembers()
	r := ModelRecord(m.Query())
	want := member{ID: 42}
	r.core.Record.Scan = func(_ database.Row, p unsafe.Pointer) error { *(*member)(p) = want; return nil }
	// Exercise the common record owner through aliases; public model codecs are not replaceable.
	source := ormcore.AliasRecord(r.core, "selected", reflect.TypeFor[member](), reflect.TypeFor[Alias[left, member]](), false)
	scope := ormcore.Scope{Reference: ormcore.Reference{Table: "selected", Owner: r.core.Record.Owner}}
	selected := ormcore.SelectRecord(source, scope, r.core.Record, reflect.TypeFor[member]())
	for _, scan := range []func(database.Row, unsafe.Pointer) error{r.core.Record.Scan, selected.Record.Scan} {
		var got member
		if e := scan(nil, unsafe.Pointer(&got)); e != nil || got != want {
			t.Fatal("decoder not retained", got, e)
		}
	}
}
