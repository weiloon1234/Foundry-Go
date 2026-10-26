package rows

import (
	"database/sql/driver"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/ormcore"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
)

func compileComposition(r ormcore.RecordSource) (string, error) {
	if r.Source.Err != nil {
		return "", r.Source.Err
	}
	c := querycore.Compiler{}
	return c.CompileSelect(r.Source.Node)
}

func TestCommonTableDependenciesAndImmutability(t *testing.T) {
	m := declareMembers()
	first := CTE("first", ModelRecord(m.Query().Where(m.ID.Ne(0)).Limit(3)))
	a := AsRecord[left](first, "a")
	second := CTE("second", SelectRecord(a, Scope(a)))
	b := AsRecord[right](second, "b")
	joined := CrossJoin(a, b)
	q := SelectRecord(joined, LeftScope(joined, Scope(a)))
	sql, err := compileComposition(q.core)
	if err != nil || strings.Count(sql, `"first" (`) != 1 || strings.Index(sql, `"first" (`) > strings.Index(sql, `"second" (`) {
		t.Fatal(sql, err)
	}
	if !strings.Contains(sql, `LIMIT $2`) || !strings.Contains(sql, `FROM "first" AS "a"`) {
		t.Fatal(sql)
	}
	materialized := first.Materialized()
	if first.core.Definition == materialized.core.Definition || first.core.Definition.Materialization != querycore.CteDefault {
		t.Fatal("mutated definition")
	}
	for _, c := range []CommonTable[member]{materialized, first.NotMaterialized()} {
		alias := AsRecord[left](c, "a")
		text, err := compileComposition(SelectRecord(alias, Scope(alias)).core)
		if err != nil || !strings.Contains(text, "MATERIALIZED") {
			t.Fatal(text, err)
		}
	}
	duplicate := AsRecord[right](CTE("first", ModelRecord(m.Query().Limit(2))), "d")
	bad := CrossJoin(a, duplicate)
	if _, err := compileComposition(SelectRecord(bad, LeftScope(bad, Scope(a))).core); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	for _, c := range []CommonTable[member]{CTE[member]("valid", nil), CTE("invalid name", ModelRecord(m.Query())), {}} {
		alias := AsRecord[left](c, "a")
		if _, err := compileComposition(SelectRecord(alias, Scope(alias)).core); !errors.Is(err, fault.Invalid) {
			t.Fatal(err)
		}
	}
}

func TestRecursiveScopeRestrictions(t *testing.T) {
	m := declareMembers()
	var captured RecursiveSelf[member]
	step := func(self RecursiveSelf[member]) RecordSource[member] {
		captured = self
		a := AsRecord[left](self, "parent")
		return SelectRecord(a, Scope(a)).Where(expr.At(Scope(a), m.ID).Ne(0))
	}
	c := RecursiveCTE("tree", ModelRecord(m.Query().Where(m.ID.Eq(1))), step)
	a := AsRecord[left](c, "tree_rows")
	text, err := compileComposition(SelectRecord(a, Scope(a)).core)
	if err != nil || !strings.HasPrefix(text, "WITH RECURSIVE ") || !strings.Contains(text, " UNION ") {
		t.Fatal(text, err)
	}
	escaped := AsRecord[right](captured, "escaped")
	if _, err := compileComposition(SelectRecord(escaped, Scope(escaped)).core); !errors.Is(err, fault.Invalid) {
		t.Fatal("escaped self", err)
	}
	missing := RecursiveCTE("missing", ModelRecord(m.Query()), func(RecursiveSelf[member]) RecordSource[member] { return ModelRecord(m.Query()) })
	twice := RecursiveCTE("twice", ModelRecord(m.Query()), func(self RecursiveSelf[member]) RecordSource[member] {
		a, b := AsRecord[left](self, "a"), AsRecord[right](self, "b")
		j := CrossJoin(a, b)
		return SelectRecord(j, LeftScope(j, Scope(a)))
	})
	nested := RecursiveCTE("nested", ModelRecord(m.Query()), func(self RecursiveSelf[member]) RecordSource[member] { return CTE("escape", self) })
	for _, bad := range []CommonTable[member]{missing, twice, nested, c.NotMaterialized(), RecursiveCTE("nil", ModelRecord(m.Query()), nil), RecursiveCTE("nil_result", ModelRecord(m.Query()), func(RecursiveSelf[member]) RecordSource[member] { return nil })} {
		a := AsRecord[left](bad, "bad")
		if _, err := compileComposition(SelectRecord(a, Scope(a)).core); !errors.Is(err, fault.Invalid) {
			t.Fatal("invalid recursion accepted", err)
		}
	}
	all := AsRecord[left](RecursiveAllCTE("tree_all", ModelRecord(m.Query()), step), "a")
	if text, err := compileComposition(SelectRecord(all, Scope(all)).core); err != nil || !strings.Contains(text, " UNION ALL ") {
		t.Fatal(text, err)
	}
}

func TestRecordSetsPreserveWindowsAndDecoder(t *testing.T) {
	m := declareMembers()
	a, b := ModelRecord(m.Query().Where(m.ID.Eq(1)).Limit(2)), ModelRecord(m.Query().Where(m.ID.Eq(2)).Offset(1))
	for i, q := range []SetQuery[member]{Union(a, b), UnionAll(a, b), Intersect(a, b), IntersectAll(a, b), Except(a, b), ExceptAll(a, b)} {
		q = q.Where(expr.At(q.Scope(), m.ID).Ne(0)).OrderBy(expr.At(q.Scope(), m.ID).Asc()).Limit(3).Offset(2)
		state := &driverState{columns: []string{"id", "name", "nickname"}, rows: [][]driver.Value{{int64(1), "Ada", nil}}}
		got, err := ReadSet(q).All(t.Context(), openTest(t, state))
		op := []string{" UNION ", " UNION ALL ", " INTERSECT ", " INTERSECT ALL ", " EXCEPT ", " EXCEPT ALL "}[i]
		if err != nil || len(got) != 1 || got[0].Name != "Ada" || !strings.Contains(state.sql, op) || !strings.Contains(state.sql, `LIMIT $2`) || !strings.Contains(state.sql, `OFFSET $4`) {
			t.Fatal(got, err, state.sql)
		}
		alias := AsRecord[left](q, "result")
		if _, err := compileComposition(SelectRecord(alias, Scope(alias)).core); err != nil {
			t.Fatal(err)
		}
	}
	empty := SetQuery[member]{}
	if _, err := compileComposition(empty.recordData(nil)); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	bad := ModelRecord(m.Query())
	bad.core.Record.Columns = bad.core.Record.Columns[:1]
	if _, err := compileComposition(Union(a, bad).core); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	tx := a
	tx.core.Transaction = true
	if _, err := compileComposition(Union(a, tx).core); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
}

func TestScalarSetsKeepCodecAndNullability(t *testing.T) {
	m := declareMembers()
	a := Values(SelectScalar(From(m.Query()), expr.Row(expr.Value(m.ID)).Value()))
	b := Values(SelectScalar(From(m.Query().Limit(1)), expr.Row(expr.Value(m.ID)).Value()))
	for _, q := range []ValueSetQuery[int64]{UnionValues(a, b), UnionAllValues(a, b), IntersectValues(a, b), IntersectAllValues(a, b), ExceptValues(a, b), ExceptAllValues(a, b)} {
		q = q.Where(q.Value().Ne(0)).OrderBy(q.Value().Asc()).Limit(2).Offset(1)
		state := &driverState{columns: []string{"value"}, rows: [][]driver.Value{{int64(4)}}}
		got, err := ReadValues(q).All(t.Context(), openTest(t, state))
		if err != nil || !reflect.DeepEqual(got, []int64{4}) {
			t.Fatal(got, err)
		}
		inner := SelectScalar(q, expr.Row(q.Value()).Value())
		alias := AsRecord[left](ModelRecord(m.Query()), "m")
		selected := SelectRecord(alias, Scope(alias)).Where(InQuery(expr.At(Scope(alias), m.ID), inner))
		if _, err := compileComposition(selected.core); err != nil {
			t.Fatal(err)
		}
	}
	n := Values(SelectScalar(From(m.Query()), expr.Row(expr.Value(m.Nickname)).Value()))
	nullable := UnionAllValues(n, n)
	state := &driverState{columns: []string{"value"}, rows: [][]driver.Value{{nil}, {"Ada"}}}
	got, err := ReadValues(nullable).All(t.Context(), openTest(t, state))
	if err != nil || len(got) != 2 || !got[0].IsNull() {
		t.Fatal(got, err)
	}
	// A scalar set also supplies a complete one-column record for a CTE.
	c := AsRecord[left](CTE("values_cte", UnionValues(a, b)), "v")
	if _, err := ReadRecord(SelectRecord(c, Scope(c))).All(t.Context(), openTest(t, &driverState{columns: []string{"value"}, rows: [][]driver.Value{{int64(3)}}})); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []ValueSetQuery[int64]{UnionValues[int64](nil, b), UnionValues(ValueRecord[int64]{}, b), {}} {
		if _, err := ReadValues(bad).All(t.Context(), openTest(t, &driverState{})); !errors.Is(err, fault.Invalid) {
			t.Fatal(err)
		}
	}
}

func TestCompositionRetainsBothDeferredChecks(t *testing.T) {
	m := declareMembers()
	sentinel := errors.New("right declaration failed")
	a, b := ModelRecord(m.Query()), ModelRecord(m.Query())
	b.core.Source.Checks = append(b.core.Source.Checks, func() error { return sentinel })
	combined := Union(a, b)
	state := &driverState{}
	if _, err := ReadSet(combined).All(t.Context(), openTest(t, state)); !errors.Is(err, sentinel) || state.sql != "" {
		t.Fatal(err, state.sql)
	}
	c := RecursiveCTE("tree", a, func(self RecursiveSelf[member]) RecordSource[member] {
		r := self.core
		r.Source.Checks = append(r.Source.Checks, func() error { return sentinel })
		return Record[member]{core: r}
	})
	alias := AsRecord[left](c, "tree_rows")
	state = &driverState{}
	if _, err := ReadRecord(SelectRecord(alias, Scope(alias))).All(t.Context(), openTest(t, state)); !errors.Is(err, sentinel) || state.sql != "" {
		t.Fatal(err, state.sql)
	}
}
