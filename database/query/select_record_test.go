package query

import (
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
)

func TestSelectRecordKeepsColumnsWindowsAndJoinMultiplicity(t *testing.T) {
	base := cursorQuery()
	id := scopedID(base.Scope())
	a := As[firstAlias](base.Where(id.Ne(2)).Limit(3), "a")
	b := As[secondAlias](base, "b")
	j := InnerJoin(a, b, On(scopedID(a.Scope()), scopedID(b.Scope())))
	scope := LeftScope(j, a.Scope())
	q := SelectRecord(j, scope).Where(scopedID(scope).Ne(4)).OrderBy(scopedID(scope).Desc()).Limit(5)
	s, err := q.Compile()
	if err != nil {
		t.Fatal(err)
	}
	want := `SELECT "a"."id", "a"."rank" FROM (SELECT "records"."id", "records"."rank" FROM "records" WHERE ("records"."id" <> $1) LIMIT $2) AS "a" INNER JOIN "records" AS "b" ON ("a"."id" = "b"."id") WHERE ("a"."id" <> $3) ORDER BY "a"."id" DESC LIMIT $4`
	if s.SQL() != want || !reflect.DeepEqual(s.Arguments(), []any{int64(2), int64(3), int64(4), int64(5)}) {
		t.Fatal("record selection changed join or window semantics", s.SQL(), s.Arguments())
	}
	if strings.Contains(s.SQL(), "DISTINCT") || len(q.recordQuery().columns) != 2 {
		t.Fatal("record selection changed multiplicity or lost columns")
	}
	_ = q.Where(scopedID(scope).Eq(9)).Limit(1)
	again, err := q.Compile()
	if err != nil || again.SQL() != s.SQL() || !reflect.DeepEqual(again.Arguments(), s.Arguments()) {
		t.Fatal("derived selection changed original builder", err)
	}

	// Complete selections retain their record type when used in CTEs and sets.
	combined := base.Union(q)
	definition := CTE("records_result", combined)
	alias := As[thirdAlias](definition, "result")
	if s, err := SelectRecord(alias, alias.Scope()).Compile(); err != nil || !strings.Contains(s.SQL(), "UNION") {
		t.Fatal("selected record lost source composition", err)
	}
	if _, err := SelectRecord(combined, combined.Scope()).Compile(); err != nil {
		t.Fatal("set scope lost complete record", err)
	}
}

func TestSelectRecordPreservedJoinSides(t *testing.T) {
	a, b, c := As[firstAlias](cursorQuery(), "a"), As[secondAlias](cursorQuery(), "b"), As[thirdAlias](cursorQuery(), "c")
	on := On(scopedID(a.Scope()), scopedID(b.Scope()))
	l, r := LeftJoin(a, b, on), RightJoin(a, b, on)
	if _, err := SelectRecord(l, LeftScope(l, a.Scope())).Compile(); err != nil {
		t.Fatal("preserved left selection failed", err)
	}
	if _, err := SelectRecord(r, RightScope(r, b.Scope())).Compile(); err != nil {
		t.Fatal("preserved right selection failed", err)
	}
	chain := InnerJoin(l, c, On(scopedID(LeftScope(l, a.Scope())), scopedID(c.Scope())))
	if _, err := SelectRecord(chain, LeftScope(chain, LeftScope(l, a.Scope()))).Compile(); err != nil {
		t.Fatal("preserved chained selection failed", err)
	}
}

func TestSelectRecordRejectsInvalidScopesAndLayouts(t *testing.T) {
	base := cursorQuery()
	scope := base.Scope()
	var nilSource *Query[cursorRecord]
	invalidColumn := scope
	invalidColumn.record = &recordMetadata[cursorRecord]{columns: slices.Clone(scope.record.columns), scan: scope.record.scan}
	invalidColumn.record.columns[0].Name = "wrong"
	noDecoder := scope
	noDecoder.record = &recordMetadata[cursorRecord]{columns: scope.record.columns}
	wrongTable := scope
	wrongTable.table = "missing"
	for name, q := range map[string]ProjectionQuery[cursorRecord, cursorRecord]{
		"nil source":           SelectRecord(nilSource, scope),
		"zero scope":           SelectRecord(base, RecordScope[cursorRecord, cursorRecord]{}),
		"declaration only":     SelectRecord(base, DeclareModelScope[cursorRecord]("records")),
		"missing table":        SelectRecord(base, wrongTable),
		"inconsistent columns": SelectRecord(base, invalidColumn),
		"missing decoder":      SelectRecord(base, noDecoder),
		"missing definition":   SelectRecord(For[cursorRecord]("records"), scope),
		"eager source":         SelectRecord(base.With(testRelation(base, base)), scope),
		"negative window":      SelectRecord(base, scope).Limit(-1),
	} {
		if s, err := q.Compile(); !errors.Is(err, fault.Invalid) || s.SQL() != "" {
			t.Fatal(name, "invalid record selection compiled", err)
		}
	}

	a := As[firstAlias](base, "a")
	other := As[firstAlias](base, "other")
	if _, err := SelectRecord(a, other.Scope()).Compile(); !errors.Is(err, fault.Invalid) {
		t.Fatal("same-tag wrong alias was accepted", err)
	}
	// Exercise runtime defense separately from public compile-time join scopes.
	b := As[secondAlias](base, "b")
	for _, j := range []joinNode{
		{source: b.input.node.source, kind: rightJoin},
		{source: b.input.node.source, kind: fullJoin},
	} {
		node := a.input.node
		node.joins = []joinNode{j}
		selection := recordSelection[cursorRecord]{table: "a", metadata: a.record}
		if _, err := selection.selectNode(node); !errors.Is(err, fault.Invalid) {
			t.Fatal("later join made a selected record nullable", err)
		}
	}
	for _, kind := range []joinKind{leftJoin, fullJoin} {
		node := a.input.node
		node.joins = []joinNode{{source: b.input.node.source, kind: kind}}
		selection := recordSelection[cursorRecord]{table: "b", metadata: b.record}
		if _, err := selection.selectNode(node); !errors.Is(err, fault.Invalid) {
			t.Fatal("nullable right source became a complete model", err)
		}
	}
}

func TestSelectRecordReusesOriginalDecoder(t *testing.T) {
	want := cursorRecord{ID: 42}
	base := cursorQuery()
	definition := *base.definition
	definition.scan = func(database.Row) (cursorRecord, error) { return want, nil }
	base.definition = &definition
	a := As[firstAlias](SelectRecord(base, base.Scope()), "selected")
	q := SelectRecord(a, a.Scope())
	for _, scan := range []func(database.Row) (cursorRecord, error){q.reader().scan, q.recordQuery().scan} {
		got, err := scan(nil)
		if err != nil || got != want {
			t.Fatal("record selection did not preserve original decoder", got, err)
		}
	}
}
