package query

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/fault"
)

func TestNamedWindowsShareDefinitionsAndBindings(t *testing.T) {
	q := cursorQuery()
	f := scopedID(q.Scope())
	base := WindowFor(q).PartitionBy(f.Group()).Named("partition")
	ordered := base.OrderBy(f.Asc()).Named("ordered")
	framed := ordered.RowsBetween(Preceding(2), CurrentRow()).Named("framed")
	n := q.modelSelect()
	n.selections = []selectItem{{expression: RowNumber(framed).node}, {expression: Count[cursorRecord]().Over(framed).node}}
	var c compiler
	sql, err := c.compileSelect(n)
	if err != nil || strings.Count(sql, `"framed" AS (`) != 1 || strings.Count(sql, `OVER "framed"`) != 2 || !strings.Contains(sql, `WINDOW "partition" AS (PARTITION BY "records"."id"), "ordered" AS ("partition" ORDER BY "records"."id" ASC), "framed" AS ("ordered" ROWS BETWEEN $1 PRECEDING AND CURRENT ROW)`) || !reflect.DeepEqual(c.arguments, []any{int64(2)}) {
		t.Fatal(sql, c.arguments, err)
	}
	// Derivation cannot change an existing named descriptor or its definition.
	_ = ordered.RowsBetween(CurrentRow(), Following(9)).Named("other")
	var again compiler
	text, err := again.compileSelect(n)
	if err != nil || text != sql || !reflect.DeepEqual(again.arguments, c.arguments) {
		t.Fatal("named descriptor changed through derivation", err)
	}
	row := RowNumber(framed)
	if _, err := SelectValue(q, row).DistinctOnValues(row.Key()).OrderBy(row.Asc()).Compile(); err != nil {
		t.Fatal("named window DISTINCT identity", err)
	}
}

func TestNamedWindowInheritanceAndCycles(t *testing.T) {
	q := cursorQuery()
	f := scopedID(q.Scope())
	base := WindowFor(q).OrderBy(f.Asc()).Named("ordered")
	framed := base.RowsBetween(Preceding(1), CurrentRow()).Named("framed")
	for name, w := range map[string]Window[cursorRecord]{
		"invalid name":       base.Named("bad name"),
		"partition override": base.PartitionBy(f.Group()),
		"order override":     base.OrderBy(f.Desc()),
		"framed extension":   framed.RowsBetween(CurrentRow(), UnboundedFollowing()),
		"framed alias":       framed.Named("alias"),
		"nested window":      WindowFor(q).OrderBy(RowNumber(base).Asc()).Named("nested"),
	} {
		if _, err := SelectValue(q, RowNumber(w)).Compile(); !errors.Is(err, fault.Invalid) {
			t.Fatal(name, err)
		}
	}
	other := WindowFor(q).Named("ordered")
	if _, err := SelectValue(q, RowNumber(base)).OrderBy(RowNumber(other).Asc()).Compile(); !errors.Is(err, fault.Invalid) {
		t.Fatal("duplicate named definition accepted", err)
	}
	cycle := &namedWindow{name: "cycle"}
	cycle.definition.reference = cycle
	if _, err := SelectValue(q, RowNumber(Window[cursorRecord]{node: windowSpec{reference: cycle}})).Compile(); !errors.Is(err, fault.Invalid) {
		t.Fatal("cyclic window accepted", err)
	}
	// Aggregate partitioning in a definition must establish SELECT grouping.
	grouped := WindowFor(q).PartitionByValues(Count[cursorRecord]().Value().Key()).Named("counts")
	if _, err := SelectValue(q, RowNumber(grouped)).Compile(); err != nil {
		t.Fatal(err)
	}
	if _, err := SelectValue(q, f.Value()).OrderBy(RowNumber(grouped).Asc()).Compile(); !errors.Is(err, fault.Invalid) {
		t.Fatal("named aggregate partition did not group input", err)
	}
}

func TestNamedWindowQueryIsolationAndQualification(t *testing.T) {
	q := cursorQuery()
	f := scopedID(q.Scope())
	inner := SelectValue(q, RowNumber(WindowFor(q).OrderBy(f.Asc()).Named("same"))).Limit(1)
	outer := SelectValue(q, ScalarQuery(q, inner)).OrderBy(RowNumber(WindowFor(q).OrderBy(f.Desc()).Named("same")).Asc())
	s, err := outer.Compile()
	if err != nil || strings.Count(s.SQL(), `WINDOW "same" AS (`) != 2 {
		t.Fatal("named definitions escaped their SELECT", s.SQL(), err)
	}
	if _, err := SelectValue(q, RowNumber(WindowFor(q).OrderBy(ScalarQuery(q, inner).Asc()).Named("nested_scope"))).Compile(); err != nil {
		t.Fatal("independent scalar window rejected", err)
	}
	definition := WindowFor(q).PartitionBy(f.Group()).Named("shared").node.reference
	renamer := correlationRenamer{from: "records", to: "qualified"}
	one := renamer.windowSpec(windowSpec{reference: definition}, 0)
	two := renamer.windowSpec(windowSpec{reference: definition}, 0)
	if renamer.err != nil || one.reference != two.reference || one.reference == definition || one.reference.definition.partitions[0].(fieldRef).table != "qualified" || definition.definition.partitions[0].(fieldRef).table != "records" {
		t.Fatal("qualification lost named identity or changed source", renamer.err)
	}
}
