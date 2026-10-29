package rows

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/value"
)

type recursionRecord struct {
	ID   int64
	Rank value.Nullable[int64]
}
type recursionFields struct {
	orm.Table[recursionRecord, int64]
	Rank orm.NullableNumber[recursionRecord, int64]
}
type recursionParent struct{}
type recursionChild struct{}
type recursionOutput struct{}

var recursionTable = orm.Define("records", func(b orm.Builder[recursionRecord], m *recursionRecord) recursionFields {
	return recursionFields{orm.TableOf(b, &m.ID), orm.NullableNumberOf(b, &m.Rank)}
})

func recursionSource() RecordRead[recursionRecord, recursionRecord] {
	q := recursionTable.Query()
	return SelectRecord(From(q), ModelScope(q))
}
func recursionID[S any](scope expr.Scope[S, recursionRecord]) expr.Expression[S, int64, int64] {
	return expr.At(scope, orm.KeyExpr(recursionTable.Key()))
}
func recursionWide[S any](scope expr.Scope[S, recursionRecord], n int) expr.Predicate[S] {
	// Floating computed values bind separately, exercising the statement-wide budget.
	return expr.OrderRow(expr.FloatOf(recursionID(scope))).In(make([]float64, n)...)
}

func recursiveRecordStep(self RecursiveSelf[recursionRecord]) RecordSource[recursionRecord] {
	parent := AsRecord[recursionParent](self, "parent")
	child := AsRecord[recursionChild](recursionSource(), "child")
	rank := expr.At(Scope(child), orm.NullableNumberExpr(recursionTable.Rank))
	joined := InnerJoin(child, parent, On(rank, recursionID(Scope(parent))))
	scope := LeftScope(joined, Scope(child))
	return SelectRecord(joined, scope).Where(recursionID(scope).Ne(8)).Limit(3)
}

func compileRecursiveRecords(table CommonTable[recursionRecord]) (orm.Statement, error) {
	a := AsRecord[recursionOutput](table, "result")
	return CompileRead(SelectRecord(a, Scope(a)).Where(recursionID(Scope(a)).Ne(9)).Limit(4))
}

func TestRecursiveCTEUsesDirectUnionAndOwnedReference(t *testing.T) {
	base := recursionSource()
	anchor := base.Where(recursionID(ModelScope(recursionTable.Query())).Eq(7)).Limit(2)
	for _, all := range []bool{false, true} {
		calls := 0
		step := func(self RecursiveSelf[recursionRecord]) RecordSource[recursionRecord] {
			calls++
			return recursiveRecordStep(self)
		}
		factory := RecursiveCTE[recursionRecord]
		operator := "UNION"
		if all {
			factory, operator = RecursiveAllCTE[recursionRecord], "UNION ALL"
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
	base := recursionSource()
	var escaped RecursiveSelf[recursionRecord]
	valid := RecursiveCTE("valid_tree", base, func(self RecursiveSelf[recursionRecord]) RecordSource[recursionRecord] {
		escaped = self
		return recursiveRecordStep(self)
	})
	var nilResult *Record[recursionRecord]
	for name, d := range map[string]CommonTable[recursionRecord]{
		"invalid name": RecursiveCTE("bad.name", base, recursiveRecordStep),
		"nil anchor":   RecursiveCTE[recursionRecord]("tree", nil, recursiveRecordStep),
		"nil callback": RecursiveCTE("tree", base, nil),
		"nil result":   RecursiveCTE("tree", base, func(RecursiveSelf[recursionRecord]) RecordSource[recursionRecord] { return nilResult }),
		"missing self": RecursiveCTE("tree", base, func(RecursiveSelf[recursionRecord]) RecordSource[recursionRecord] { return base }),
		"zero self": RecursiveCTE("tree", base, func(RecursiveSelf[recursionRecord]) RecordSource[recursionRecord] {
			return RecursiveSelf[recursionRecord]{}
		}),
		"foreign self":       RecursiveCTE("tree", base, func(RecursiveSelf[recursionRecord]) RecordSource[recursionRecord] { return escaped }),
		"self in anchor":     RecursiveCTE("tree", escaped, recursiveRecordStep),
		"escaped definition": CTE("escaped", escaped),
		"physical shadow":    RecursiveCTE("records", base, recursiveRecordStep),
		"not materialized":   valid.NotMaterialized(),
	} {
		if s, err := compileRecursiveRecords(d); !errors.Is(err, fault.Invalid) || s.SQL() != "" {
			t.Fatal(name, "invalid recursive definition compiled", err)
		}
	}
	a := AsRecord[recursionParent](escaped, "escaped")
	if _, err := CompileRead(SelectRecord(a, Scope(a))); !errors.Is(err, fault.Invalid) {
		t.Fatal("escaped recursive self executed outside owning CTE", err)
	}
	if _, err := compileRecursiveRecords(valid); err != nil {
		t.Fatal("rejected escapes invalidated original definition", err)
	}
}

func TestRecursiveCTERejectsSelfPlacement(t *testing.T) {
	base := recursionSource()
	for name, step := range map[string]func(RecursiveSelf[recursionRecord]) RecordSource[recursionRecord]{
		"duplicate": func(self RecursiveSelf[recursionRecord]) RecordSource[recursionRecord] {
			a, b := AsRecord[recursionParent](self, "a"), AsRecord[recursionChild](self, "b")
			j := InnerJoin(a, b, On(recursionID(Scope(a)), recursionID(Scope(b))))
			return SelectRecord(j, LeftScope(j, Scope(a)))
		},
		"expression subquery": func(self RecursiveSelf[recursionRecord]) RecordSource[recursionRecord] {
			a := AsRecord[recursionParent](self, "a")
			return base.Where(ExistsQuery(From(recursionTable.Query()), a))
		},
		"nullable join": func(self RecursiveSelf[recursionRecord]) RecordSource[recursionRecord] {
			a, b := AsRecord[recursionParent](base, "a"), AsRecord[recursionChild](self, "b")
			j := LeftJoin(a, b, On(recursionID(Scope(a)), recursionID(Scope(b))))
			return SelectRecord(j, LeftScope(j, Scope(a)))
		},
		"dependency captures self": func(self RecursiveSelf[recursionRecord]) RecordSource[recursionRecord] { return CTE("captured", self) },
		"intersect all": func(self RecursiveSelf[recursionRecord]) RecordSource[recursionRecord] {
			return IntersectAll(self, base)
		},
		"except all":   func(self RecursiveSelf[recursionRecord]) RecordSource[recursionRecord] { return ExceptAll(self, base) },
		"right except": func(self RecursiveSelf[recursionRecord]) RecordSource[recursionRecord] { return Except(base, self) },
		"direct aggregate": func(self RecursiveSelf[recursionRecord]) RecordSource[recursionRecord] {
			a := AsRecord[recursionParent](self, "a")
			id := recursionID(Scope(a))
			rank := expr.At(Scope(a), orm.NullableNumberExpr(recursionTable.Rank))
			q := SelectRecord(a, Scope(a)).OrderBy(expr.CountFieldAt(Scope(a), orm.KeyExpr(recursionTable.Key())).Asc())
			q.core.Source.Node.GroupBy = []querycore.ValueExpression{querycore.FieldRef{Table: "a", Column: "id"}, querycore.FieldRef{Table: "a", Column: "rank"}}
			_ = id
			_ = rank
			return q
		},
		"later nullable join": func(self RecursiveSelf[recursionRecord]) RecordSource[recursionRecord] {
			a, b, c := AsRecord[recursionParent](self, "a"), AsRecord[recursionChild](base, "b"), AsRecord[recursionOutput](base, "c")
			j := InnerJoin(a, b, On(recursionID(Scope(a)), recursionID(Scope(b))))
			chain := RightJoin(j, c, On(recursionID(LeftScope(j, Scope(a))), recursionID(Scope(c))))
			return SelectRecord(chain, RightScope(chain, Scope(c)))
		},
	} {
		if _, err := compileRecursiveRecords(RecursiveCTE("tree", base, step)); !errors.Is(err, fault.Invalid) {
			t.Fatal(name, "invalid recursive placement compiled", err)
		}
	}
}

func TestRecursiveCTESharedBoundsAndMalformedAST(t *testing.T) {
	base := recursionSource()
	wide := RecursiveCTE("wide", base.Where(recursionWide(ModelScope(recursionTable.Query()), querycore.MaxParameters/2+1)), func(self RecursiveSelf[recursionRecord]) RecordSource[recursionRecord] {
		a := AsRecord[recursionParent](self, "a")
		return SelectRecord(a, Scope(a)).Where(recursionWide(Scope(a), querycore.MaxParameters/2+1))
	})
	if s, err := compileRecursiveRecords(wide); !errors.Is(err, fault.Invalid) || s.SQL() != "" || len(s.Arguments()) != 0 {
		t.Fatal("recursive definitions bypassed shared parameter budget", err)
	}
	cyclic := RecursiveCTE("cyclic", base, recursiveRecordStep)
	node := &cyclic.core.Definition.Recursion.Step
	node.Source = querycore.TableSource{Alias: "loop", Columns: cyclic.core.Definition.Columns, Query: node}
	if _, err := compileRecursiveRecords(cyclic); !errors.Is(err, fault.Invalid) {
		t.Fatal("private recursive AST cycle was not bounded", err)
	}
	for _, mutate := range []func(*querycore.RecursiveCTE){
		func(r *querycore.RecursiveCTE) { r.Reference = nil },
		func(r *querycore.RecursiveCTE) { r.Operator = querycore.IntersectSet },
		func(r *querycore.RecursiveCTE) { r.Step.Selections = nil },
		func(r *querycore.RecursiveCTE) {
			r.Reference = &querycore.RecursiveReference{Name: "wrong", Columns: r.Reference.Columns}
		},
	} {
		d := RecursiveCTE("malformed", base, recursiveRecordStep)
		mutate(d.core.Definition.Recursion)
		if _, err := compileRecursiveRecords(d); !errors.Is(err, fault.Invalid) {
			t.Fatal("malformed recursive declaration accepted", err)
		}
	}
}

func TestRecursiveCTEDependenciesAndAliasAnalysis(t *testing.T) {
	base := CTE("anchor_rows", recursionSource())
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
	names, err := querycore.NamesInSelects(consumer.recordData(nil).Source.Node)
	if err != nil || !names.Used["parent"] || !names.Used["child"] {
		t.Fatal("alias analysis skipped recursive step", err)
	}
}
