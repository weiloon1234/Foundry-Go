package query

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
)

func recursiveRecordStep(self RecursiveSelf[cursorRecord]) RecordQuerySource[cursorRecord] {
	parent := As[firstAlias](self, "parent")
	child := As[secondAlias](cursorQuery(), "child")
	rank := NewNullableField[Alias[secondAlias, cursorRecord], int64](child.Scope().Table(), "rank", codec.Signed[int64]())
	joined := InnerJoin(child, parent, On(rank, scopedID(parent.Scope())))
	scope := LeftScope(joined, child.Scope())
	return SelectRecord(joined, scope).Where(scopedID(scope).Ne(8)).Limit(3)
}

func compileRecursiveRecords(table CommonTable[cursorRecord]) (Statement, error) {
	a := As[thirdAlias](table, "result")
	return SelectRecord(a, a.Scope()).Where(scopedID(a.Scope()).Ne(9)).Limit(4).Compile()
}

func TestRecursiveCTEUsesDirectUnionAndOwnedReference(t *testing.T) {
	base := cursorQuery()
	anchor := base.Where(scopedID(base.Scope()).Eq(7)).Limit(2)
	for _, all := range []bool{false, true} {
		calls := 0
		step := func(self RecursiveSelf[cursorRecord]) RecordQuerySource[cursorRecord] {
			calls++
			return recursiveRecordStep(self)
		}
		factory := RecursiveCTE[cursorRecord]
		operator := "UNION"
		if all {
			factory, operator = RecursiveAllCTE[cursorRecord], "UNION ALL"
		}
		definition := factory("descendants", anchor, step)
		s, err := compileRecursiveRecords(definition)
		if err != nil {
			t.Fatal(err)
		}
		if calls != 1 || !strings.HasPrefix(s.SQL(), `WITH RECURSIVE "descendants" ("id", "rank") AS ((SELECT`) || !strings.Contains(s.SQL(), ") "+operator+" (SELECT") || !strings.Contains(s.SQL(), `INNER JOIN "descendants" AS "parent"`) {
			t.Fatal("invalid recursive SQL shape or repeated construction callback", s.SQL())
		}
		if !reflect.DeepEqual(s.Arguments(), []any{int64(7), int64(2), int64(8), int64(3), int64(9), int64(4)}) {
			t.Fatal("recursive parameters or windows changed", s.Arguments())
		}
		_ = definition.Materialized()
		again, err := compileRecursiveRecords(definition)
		if err != nil || again.SQL() != s.SQL() || calls != 1 {
			t.Fatal("recursive declaration changed after reuse", err)
		}
		if _, err := compileRecursiveRecords(definition.Materialized()); err != nil {
			t.Fatal("materialized clone lost working-table identity", err)
		}
	}
}

func TestRecursiveCTEInvalidBoundaries(t *testing.T) {
	base := cursorQuery()
	var escaped RecursiveSelf[cursorRecord]
	valid := RecursiveCTE("valid_tree", base, func(self RecursiveSelf[cursorRecord]) RecordQuerySource[cursorRecord] {
		escaped = self
		return recursiveRecordStep(self)
	})
	var nilResult *Query[cursorRecord]
	for name, d := range map[string]CommonTable[cursorRecord]{
		"invalid name": RecursiveCTE("bad.name", base, recursiveRecordStep),
		"nil anchor":   RecursiveCTE[cursorRecord]("tree", nil, recursiveRecordStep),
		"nil callback": RecursiveCTE("tree", base, nil),
		"nil result":   RecursiveCTE("tree", base, func(RecursiveSelf[cursorRecord]) RecordQuerySource[cursorRecord] { return nilResult }),
		"missing self": RecursiveCTE("tree", base, func(RecursiveSelf[cursorRecord]) RecordQuerySource[cursorRecord] { return base }),
		"zero self": RecursiveCTE("tree", base, func(RecursiveSelf[cursorRecord]) RecordQuerySource[cursorRecord] {
			return RecursiveSelf[cursorRecord]{}
		}),
		"foreign self":       RecursiveCTE("tree", base, func(RecursiveSelf[cursorRecord]) RecordQuerySource[cursorRecord] { return escaped }),
		"self in anchor":     RecursiveCTE("tree", escaped, recursiveRecordStep),
		"escaped definition": CTE("escaped", escaped),
		"physical shadow":    RecursiveCTE("records", base, recursiveRecordStep),
		"not materialized":   valid.NotMaterialized(),
	} {
		if s, err := compileRecursiveRecords(d); !errors.Is(err, fault.Invalid) || s.SQL() != "" {
			t.Fatal(name, "invalid recursive definition compiled", err)
		}
	}
	a := As[firstAlias](escaped, "escaped")
	if _, err := SelectRecord(a, a.Scope()).Compile(); !errors.Is(err, fault.Invalid) {
		t.Fatal("escaped recursive self executed outside owning CTE", err)
	}
	if _, err := compileRecursiveRecords(valid); err != nil {
		t.Fatal("rejected escapes invalidated original definition", err)
	}
}

func TestRecursiveCTERejectsSelfPlacement(t *testing.T) {
	base := cursorQuery()
	for name, step := range map[string]func(RecursiveSelf[cursorRecord]) RecordQuerySource[cursorRecord]{
		"duplicate": func(self RecursiveSelf[cursorRecord]) RecordQuerySource[cursorRecord] {
			a, b := As[firstAlias](self, "a"), As[secondAlias](self, "b")
			j := InnerJoin(a, b, On(scopedID(a.Scope()), scopedID(b.Scope())))
			return SelectRecord(j, LeftScope(j, a.Scope()))
		},
		"expression subquery": func(self RecursiveSelf[cursorRecord]) RecordQuerySource[cursorRecord] {
			a := As[firstAlias](self, "a")
			return base.Where(ExistsQuery(base, a))
		},
		"nullable join": func(self RecursiveSelf[cursorRecord]) RecordQuerySource[cursorRecord] {
			a, b := As[firstAlias](base, "a"), As[secondAlias](self, "b")
			j := LeftJoin(a, b, On(scopedID(a.Scope()), scopedID(b.Scope())))
			return SelectRecord(j, LeftScope(j, a.Scope()))
		},
		"dependency captures self": func(self RecursiveSelf[cursorRecord]) RecordQuerySource[cursorRecord] { return CTE("captured", self) },
		"intersect all": func(self RecursiveSelf[cursorRecord]) RecordQuerySource[cursorRecord] {
			return IntersectAll(self, base)
		},
		"except all":   func(self RecursiveSelf[cursorRecord]) RecordQuerySource[cursorRecord] { return ExceptAll(self, base) },
		"right except": func(self RecursiveSelf[cursorRecord]) RecordQuerySource[cursorRecord] { return Except(base, self) },
		"direct aggregate": func(self RecursiveSelf[cursorRecord]) RecordQuerySource[cursorRecord] {
			a := As[firstAlias](self, "a")
			id := scopedID(a.Scope())
			rank := NewNullableField[Alias[firstAlias, cursorRecord], int64]("a", "rank", codec.Signed[int64]())
			return SelectRecord(a, a.Scope()).GroupBy(id.Group(), rank.Group()).OrderBy(id.Count().Asc())
		},
		"later nullable join": func(self RecursiveSelf[cursorRecord]) RecordQuerySource[cursorRecord] {
			a, b, c := As[firstAlias](self, "a"), As[secondAlias](base, "b"), As[thirdAlias](base, "c")
			j := InnerJoin(a, b, On(scopedID(a.Scope()), scopedID(b.Scope())))
			chain := RightJoin(j, c, On(scopedID(LeftScope(j, a.Scope())), scopedID(c.Scope())))
			return SelectRecord(chain, RightScope(chain, c.Scope()))
		},
	} {
		if _, err := compileRecursiveRecords(RecursiveCTE("tree", base, step)); !errors.Is(err, fault.Invalid) {
			t.Fatal(name, "invalid recursive placement compiled", err)
		}
	}
}

func TestRecursiveCTESharedBoundsAndMalformedAST(t *testing.T) {
	base := cursorQuery()
	values := make([]int64, MaxParameters/2+1)
	wide := RecursiveCTE("wide", base.Where(scopedID(base.Scope()).In(values...)), func(self RecursiveSelf[cursorRecord]) RecordQuerySource[cursorRecord] {
		a := As[firstAlias](self, "a")
		return SelectRecord(a, a.Scope()).Where(scopedID(a.Scope()).In(values...))
	})
	if s, err := compileRecursiveRecords(wide); !errors.Is(err, fault.Invalid) || s.SQL() != "" || len(s.Arguments()) != 0 {
		t.Fatal("recursive definitions bypassed shared parameter budget", err)
	}
	cyclic := RecursiveCTE("cyclic", base, recursiveRecordStep)
	node := &cyclic.definition.recursion.step
	node.source = tableSource{alias: "loop", columns: cyclic.definition.columns, query: node}
	if _, err := compileRecursiveRecords(cyclic); !errors.Is(err, fault.Invalid) {
		t.Fatal("private recursive AST cycle was not bounded", err)
	}
	for _, mutate := range []func(*recursiveCTE){
		func(r *recursiveCTE) { r.reference = nil },
		func(r *recursiveCTE) { r.operator = intersectSet },
		func(r *recursiveCTE) { r.step.selections = nil },
		func(r *recursiveCTE) { r.reference = &recursiveReference{name: "wrong", columns: r.reference.columns} },
	} {
		d := RecursiveCTE("malformed", base, recursiveRecordStep)
		mutate(d.definition.recursion)
		if _, err := compileRecursiveRecords(d); !errors.Is(err, fault.Invalid) {
			t.Fatal("malformed recursive declaration accepted", err)
		}
	}
}

func TestRecursiveCTEDependenciesAndAliasAnalysis(t *testing.T) {
	base := CTE("anchor_rows", cursorQuery())
	tree := RecursiveCTE("tree", base, recursiveRecordStep)
	consumer := CTE("consumed", Union(tree, tree))
	s, err := compileRecursiveRecords(consumer)
	if err != nil {
		t.Fatal(err)
	}
	a, b, c := strings.Index(s.SQL(), `"anchor_rows" (`), strings.Index(s.SQL(), `"tree" (`), strings.Index(s.SQL(), `"consumed" (`)
	if a < 0 || b <= a || c <= b || strings.Count(s.SQL(), `"tree" ("id", "rank") AS`) != 1 {
		t.Fatal("recursive CTE dependencies not emitted once in order", s.SQL())
	}
	names, err := namesInSelects(consumer.recordQuery().node)
	if err != nil || !names.used["parent"] || !names.used["child"] {
		t.Fatal("alias analysis skipped recursive step", err)
	}
}
