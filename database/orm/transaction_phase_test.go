package orm

import (
	"go/token"
	"go/types"
	"slices"
	"sort"
	"testing"
)

// Compare ordinary conversions and solve matching generic layouts, retaining
// nested/concrete substitutions and independently sized argument lists. The
// go/types conversion check, not the structural matcher, decides each witness.
func exportedLayoutConversions(t *testing.T, pkgs ...*types.Package) []string {
	t.Helper()
	exported := map[string]types.Type{}
	for _, pkg := range pkgs {
		for _, name := range pkg.Scope().Names() {
			obj, ok := pkg.Scope().Lookup(name).(*types.TypeName)
			if !ok || !obj.Exported() {
				continue
			}
			typ := obj.Type()
			if iface, ok := typ.Underlying().(*types.Interface); ok && !iface.IsMethodSet() {
				continue // Constraints are not value types and cannot be converted.
			}
			exported[pkg.Name()+"."+name] = typ
		}
	}
	defaults := map[string]types.Type{}
	for name, typ := range exported {
		defaults[name] = (phaseSubstitution{}).instantiate(t, typ)
	}
	var violations []string
	for name, source := range exported {
		for other, target := range exported {
			if name >= other || types.Identical(source, target) {
				continue
			}
			left, right := defaults[name], defaults[other]
			ordinary := !types.Identical(left, right) && types.ConvertibleTo(left, right) && types.ConvertibleTo(right, left)
			if !ordinary && (phaseTypeParams(source).Len() > 0 || phaseTypeParams(target).Len() > 0) {
				_, _, ordinary = phaseConversionWitness(t, source, target)
			}
			if ordinary {
				violations = append(violations, name+" <-> "+other)
			}
		}
	}
	sort.Strings(violations)
	return violations
}

// Each entry names a specific pair and explains why it grants no capability.
// New pairs, empty reasons and stale entries fail. Input-role conversions have
// no exceptions; their declaration roles must remain distinct.
var allowedPhaseConversions = map[string]string{
	"expr.Aggregate <-> expr.OrderedAggregate":                                 "Both already carry AggregateExpression; ordered operations require the same ordered value type.",
	"expr.DateUnit <-> expr.LocalResolution":                                   "Scalar enum values grant no query capability; destination operations validate their domain.",
	"expr.DateUnit <-> expr.TimestampUnit":                                     "Scalar enum values grant no query capability; destination operations validate their domain.",
	"expr.DateUnit <-> query.DateUnit":                                         "Scalar enum values grant no query capability; destination operations validate their domain.",
	"expr.DateUnit <-> query.LocalResolution":                                  "Scalar enum values grant no query capability; destination operations validate their domain.",
	"expr.DateUnit <-> query.TimestampUnit":                                    "Scalar enum values grant no query capability; destination operations validate their domain.",
	"expr.JSONKind <-> orm.MorphName":                                          "Scalar names grant no query capability and retain destination name/codec validation.",
	"expr.JSONKind <-> query.GlobalScopeName":                                  "Scalar names grant no query capability and retain destination name/codec validation.",
	"expr.JSONKind <-> query.MorphName":                                        "Scalar names grant no query capability and retain destination name/codec validation.",
	"expr.LocalResolution <-> expr.TimestampUnit":                              "Scalar enum values grant no query capability; destination operations validate their domain.",
	"expr.LocalResolution <-> query.DateUnit":                                  "Scalar enum values grant no query capability; destination operations validate their domain.",
	"expr.LocalResolution <-> query.LocalResolution":                           "Scalar enum values grant no query capability; destination operations validate their domain.",
	"expr.LocalResolution <-> query.TimestampUnit":                             "Scalar enum values grant no query capability; destination operations validate their domain.",
	"expr.NullableOrderedRowExpression <-> expr.NullableTextRowExpression":     "These are row values; equivalent ordered/text predicates already exist as row package functions, retaining nullability.",
	"expr.NullableOrderedRowExpression <-> expr.OrderedRowExpression":          "A nullable wrapper cannot satisfy the other ordered/string constraint; no valid conversion exists.",
	"expr.NullableOrderedRowExpression <-> expr.RowExpression":                 "These are row values; equivalent ordered/text predicates already exist as row package functions, retaining nullability.",
	"expr.NullableOrderedRowExpression <-> expr.TextRowExpression":             "A nullable wrapper cannot satisfy the other ordered/string constraint; no valid conversion exists.",
	"expr.NullableOrderedValueComparison <-> expr.NullableTextValueComparison": "Selected comparisons already exist via CompareValue, OrderValue or TextValue and nullable variants; no aggregate/row capability.",
	"expr.NullableOrderedValueComparison <-> expr.OrderedValueComparison":      "A nullable wrapper cannot satisfy the other ordered/string constraint; no valid conversion exists.",
	"expr.NullableOrderedValueComparison <-> expr.Selected":                    "Selected comparisons already exist via CompareValue, OrderValue or TextValue and nullable variants; no aggregate/row capability.",
	"expr.NullableOrderedValueComparison <-> expr.TextValueComparison":         "A nullable wrapper cannot satisfy the other ordered/string constraint; no valid conversion exists.",
	"expr.NullableOrderedValueComparison <-> expr.ValueComparison":             "Selected comparisons already exist via CompareValue, OrderValue or TextValue and nullable variants; no aggregate/row capability.",
	"expr.NullableTextRowExpression <-> expr.OrderedRowExpression":             "A nullable wrapper cannot satisfy the other ordered/string constraint; no valid conversion exists.",
	"expr.NullableTextRowExpression <-> expr.RowExpression":                    "These are row values; equivalent ordered/text predicates already exist as row package functions, retaining nullability.",
	"expr.NullableTextRowExpression <-> expr.TextRowExpression":                "A nullable wrapper cannot satisfy the other ordered/string constraint; no valid conversion exists.",
	"expr.NullableTextValueComparison <-> expr.OrderedValueComparison":         "A nullable wrapper cannot satisfy the other ordered/string constraint; no valid conversion exists.",
	"expr.NullableTextValueComparison <-> expr.Selected":                       "Selected comparisons already exist via CompareValue, OrderValue or TextValue and nullable variants; no aggregate/row capability.",
	"expr.NullableTextValueComparison <-> expr.TextValueComparison":            "A nullable wrapper cannot satisfy the other ordered/string constraint; no valid conversion exists.",
	"expr.NullableTextValueComparison <-> expr.ValueComparison":                "Selected comparisons already exist via CompareValue, OrderValue or TextValue and nullable variants; no aggregate/row capability.",
	"expr.OrderedRowExpression <-> expr.RowExpression":                         "These are row values; equivalent ordered/text predicates already exist as row package functions, retaining nullability.",
	"expr.OrderedRowExpression <-> expr.TextRowExpression":                     "These are row values; equivalent ordered/text predicates already exist as row package functions, retaining nullability.",
	"expr.OrderedValueComparison <-> expr.Selected":                            "Selected comparisons already exist via CompareValue, OrderValue or TextValue and nullable variants; no aggregate/row capability.",
	"expr.OrderedValueComparison <-> expr.TextValueComparison":                 "Selected comparisons already exist via CompareValue, OrderValue or TextValue and nullable variants; no aggregate/row capability.",
	"expr.OrderedValueComparison <-> expr.ValueComparison":                     "Selected comparisons already exist via CompareValue, OrderValue or TextValue and nullable variants; no aggregate/row capability.",
	"expr.RowExpression <-> expr.TextRowExpression":                            "These are row values; equivalent ordered/text predicates already exist as row package functions, retaining nullability.",
	"expr.Selected <-> expr.TextValueComparison":                               "Selected comparisons already exist via CompareValue, OrderValue or TextValue and nullable variants; no aggregate/row capability.",
	"expr.Selected <-> expr.ValueComparison":                                   "Selected comparisons already exist via CompareValue, OrderValue or TextValue and nullable variants; no aggregate/row capability.",
	"expr.TextValueComparison <-> expr.ValueComparison":                        "Selected comparisons already exist via CompareValue, OrderValue or TextValue and nullable variants; no aggregate/row capability.",
	"expr.TimestampUnit <-> query.DateUnit":                                    "Scalar enum values grant no query capability; destination operations validate their domain.",
	"expr.TimestampUnit <-> query.LocalResolution":                             "Scalar enum values grant no query capability; destination operations validate their domain.",
	"expr.TimestampUnit <-> query.TimestampUnit":                               "Scalar enum values grant no query capability; destination operations validate their domain.",
	"orm.MorphName <-> query.GlobalScopeName":                                  "Scalar names grant no query capability and retain destination name/codec validation.",
	"orm.MorphName <-> query.JSONKind":                                         "Scalar names grant no query capability and retain destination name/codec validation.",
	"orm.PivotChanges <-> query.PivotChanges":                                  "Plain attached/detached/updated result slices grant no read or write operation.",
	"orm.RetrievalHooks <-> query.RetrievalHooks":                              "The same publicly constructible retrieved callback configuration grants no executor or phase.",
	"query.BinaryField <-> query.ScalarField":                                  "Disjoint value constraints prohibit a valid conversion; the sweep conservatively erases constraints.",
	"query.CursorScope <-> query.Set":                                          "Inert scope tags cannot construct sources or change the named scope of an existing expression.",
	"query.DateUnit <-> query.LocalResolution":                                 "Scalar enum values grant no query capability; destination operations validate their domain.",
	"query.DateUnit <-> query.TimestampUnit":                                   "Scalar enum values grant no query capability; destination operations validate their domain.",
	"query.ExactField <-> query.FloatField":                                    "Disjoint value constraints prohibit a valid conversion; the sweep conservatively erases constraints.",
	"query.ExactField <-> query.IntervalField":                                 "Disjoint value constraints prohibit a valid conversion; the sweep conservatively erases constraints.",
	"query.ExactField <-> query.TextField":                                     "Disjoint value constraints prohibit a valid conversion; the sweep conservatively erases constraints.",
	"query.FloatField <-> query.IntervalField":                                 "Disjoint value constraints prohibit a valid conversion; the sweep conservatively erases constraints.",
	"query.FloatField <-> query.TextField":                                     "Disjoint value constraints prohibit a valid conversion; the sweep conservatively erases constraints.",
	"query.GlobalScopeName <-> query.JSONKind":                                 "Scalar names grant no query capability and retain destination name/codec validation.",
	"query.GlobalScopeName <-> query.MorphName":                                "Scalar names grant no query capability and retain destination name/codec validation.",
	"query.IntervalField <-> query.TextField":                                  "Disjoint value constraints prohibit a valid conversion; the sweep conservatively erases constraints.",
	"query.JSONKind <-> query.MorphName":                                       "Scalar names grant no query capability and retain destination name/codec validation.",
	"query.JSONPath <-> query.RowExpression":                                   "JSONNullableRoot and JSON already adapt the same nullable JSON row value in both directions.",
	"query.LocalResolution <-> query.TimestampUnit":                            "Scalar enum values grant no query capability; destination operations validate their domain.",
	"query.NullableExactField <-> query.NullableFloatField":                    "Disjoint value constraints prohibit a valid conversion; the sweep conservatively erases constraints.",
	"query.NullableExactField <-> query.NullableIntervalField":                 "Disjoint value constraints prohibit a valid conversion; the sweep conservatively erases constraints.",
	"query.NullableExactField <-> query.NullableTextField":                     "Disjoint value constraints prohibit a valid conversion; the sweep conservatively erases constraints.",
	"query.NullableFloatField <-> query.NullableIntervalField":                 "Disjoint value constraints prohibit a valid conversion; the sweep conservatively erases constraints.",
	"query.NullableFloatField <-> query.NullableTextField":                     "Disjoint value constraints prohibit a valid conversion; the sweep conservatively erases constraints.",
	"query.NullableIntervalField <-> query.NullableTextField":                  "Disjoint value constraints prohibit a valid conversion; the sweep conservatively erases constraints.",
	"query.NullableOrderedRowExpression <-> query.OrderedRowExpression":        "A nullable wrapper cannot satisfy the other ordered/string constraint; no valid conversion exists.",
	"query.NullableOrderedValueComparison <-> query.OrderedValueComparison":    "A nullable wrapper cannot satisfy the other ordered/string constraint; no valid conversion exists.",
	"rows.Alias <-> rows.Inner":                                                "Inert scope tags cannot construct sources or change the named scope of an existing expression.",
	"rows.TransactionJoinSource <-> rows.TransactionSelectionSource":           "Equivalent sealed interfaces; validated source metadata still owns join eligibility and transaction requirements.",
}

func TestExportedTypesPreventPhaseConversions(t *testing.T) {
	imp, _ := surfaceImporter(t, token.NewFileSet(), ".", ".", "./expr", "./rows", "../query")
	var pkgs []*types.Package
	for _, path := range []string{"database/orm", "database/orm/expr", "database/orm/rows", "database/query"} {
		pkg, err := imp.Import("github.com/weiloon1234/Foundry-Go/" + path)
		if err != nil {
			t.Fatal(err)
		}
		pkgs = append(pkgs, pkg)
	}
	seen := map[string]bool{}
	for _, pair := range exportedLayoutConversions(t, pkgs...) {
		seen[pair] = true
		if allowedPhaseConversions[pair] == "" {
			t.Errorf("unapproved exported type conversion: %s", pair)
		}
	}
	for pair := range allowedPhaseConversions {
		if !seen[pair] {
			t.Errorf("stale conversion exception: %s", pair)
		}
	}
}

func TestTransactionLayoutSweepDetectsOriginalRecordConversion(t *testing.T) {
	pkg := surfacePackageIn(t, "./rows", `package rows
 import "github.com/weiloon1234/Foundry-Go/internal/ormcore"
 type TransactionLayoutProbe[R any] struct { _ [0]*R; ordinaryRecord [0]struct{}; core ormcore.RecordSource }
 `)
	if !slices.Contains(exportedLayoutConversions(t, pkg), "rows.Record <-> rows.TransactionLayoutProbe") {
		t.Fatal("layout sweep missed ordinary/transaction record escape")
	}
}

// Inject the ordinary native layouts under names belonging to later phases.
// These probes deliberately reproduce the conversions that phase fields prevent.
func TestPhaseSweepDetectsNativeExpressionConversions(t *testing.T) {
	pkg := surfacePackageIn(t, "./expr", `package expr
 import "github.com/weiloon1234/Foundry-Go/internal/ormcore"
 type HavingLayoutProbe[S any] struct { predicatePhase [0]struct{}; _ [0]*S; core ormcore.Predicate }
 type ValueComparisonLayoutProbe[S,V any] struct { rowPhase [0]struct{}; _ [0]*S; data rowData[V] }
 type ValueCaseLayoutProbe[S,V any] struct { rowCasePhase [0]struct{}; _ [0]*S; builder ormcore.CaseBuilder; column ormcore.Column[V] }
 type ProjectionKeyLayoutProbe[S any] struct { rowKeyPhase [0]struct{}; _ [0]*S; core ormcore.Expression }
 `)
	pairs := exportedLayoutConversions(t, pkg)
	for _, pair := range []string{"expr.HavingLayoutProbe <-> expr.Predicate", "expr.RowExpression <-> expr.ValueComparisonLayoutProbe", "expr.Case <-> expr.ValueCaseLayoutProbe", "expr.GroupKey <-> expr.ProjectionKeyLayoutProbe"} {
		if !slices.Contains(pairs, pair) {
			t.Errorf("phase sweep missed %s", pair)
		}
	}
}

func TestPhaseSweepIncludesNonStructsAndPackagePairs(t *testing.T) {
	left, right := types.NewPackage("example/left", "left"), types.NewPackage("example/right", "right")
	for _, definition := range []struct {
		pkg  *types.Package
		name string
	}{{left, "First"}, {right, "Second"}, {left, "private"}} {
		obj := types.NewTypeName(token.NoPos, definition.pkg, definition.name, nil)
		types.NewNamed(obj, types.Typ[types.Uint8], nil)
		definition.pkg.Scope().Insert(obj)
	}
	if got := exportedLayoutConversions(t, left, right); !slices.Equal(got, []string{"left.First <-> right.Second"}) {
		t.Fatalf("non-struct/package conversion sweep: %v", got)
	}
}

func TestPhaseSweepIncludesUnnamedAliases(t *testing.T) {
	pkg := types.NewPackage("example/probe", "probe")
	layout := types.NewStruct([]*types.Var{types.NewField(token.NoPos, pkg, "value", types.Typ[types.Int], false)}, nil)
	named := types.NewTypeName(token.NoPos, pkg, "Named", nil)
	types.NewNamed(named, layout, nil)
	pkg.Scope().Insert(named)
	alias := types.NewTypeName(token.NoPos, pkg, "Alias", nil)
	types.NewAlias(alias, layout)
	pkg.Scope().Insert(alias)
	same := types.NewTypeName(token.NoPos, pkg, "Same", nil)
	types.NewAlias(same, named.Type())
	pkg.Scope().Insert(same)
	want := []string{"probe.Alias <-> probe.Named", "probe.Alias <-> probe.Same"}
	if got := exportedLayoutConversions(t, pkg); !slices.Equal(got, want) {
		t.Fatalf("unnamed alias conversion sweep: %v", got)
	}
}
