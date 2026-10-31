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
	"expr.LocalResolution <-> expr.TimestampUnit":                              "Scalar enum values grant no query capability; destination operations validate their domain.",
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
	"rows.Alias <-> rows.Inner":                                                "Inert scope tags cannot construct sources or change the named scope of an existing expression.",
	"rows.TransactionJoinSource <-> rows.TransactionSelectionSource":           "Equivalent sealed interfaces; validated source metadata still owns join eligibility and transaction requirements.",
}

func TestExportedTypesPreventPhaseConversions(t *testing.T) {
	imp, _ := surfaceImporter(t, token.NewFileSet(), ".", ".", "./expr", "./rows")
	var pkgs []*types.Package
	for _, path := range []string{"database/orm", "database/orm/expr", "database/orm/rows"} {
		pkg, err := imp.Import("github.com/weiloon1234/Foundry-Go/" + path)
		if err != nil {
			t.Fatal(err)
		}
		pkgs = append(pkgs, pkg)
	}
	seen := map[string]bool{}
	for _, pair := range exportedLayoutConversions(t, pkgs...) {
		seen[pair] = true
		if allowedPhaseConversions[pair] == "" && !explicitColumnAdapterConversion(pair) {
			t.Errorf("unapproved exported type conversion: %s", pair)
		}
	}
	for pair := range allowedPhaseConversions {
		if !seen[pair] {
			t.Errorf("stale conversion exception: %s", pair)
		}
	}
	// The family sweep above also needs same-family, different-scope witnesses.
	for _, pkg := range pkgs {
		if pkg.Name() != "expr" {
			continue
		}
		for _, name := range []string{"CompareFilterAdapter", "NullableCompareFilterAdapter", "OrderFilterAdapter", "NullableOrderFilterAdapter", "TextFilterAdapter", "NullableTextFilterAdapter"} {
			typ := pkg.Scope().Lookup(name).Type()
			first, err := types.Instantiate(nil, typ, []types.Type{types.Typ[types.Int], types.Typ[types.String]}, true)
			if err != nil {
				t.Fatal(err)
			}
			second, err := types.Instantiate(nil, typ, []types.Type{types.Typ[types.Bool], types.Typ[types.String]}, true)
			if err != nil {
				t.Fatal(err)
			}
			if types.ConvertibleTo(first, second) || types.ConvertibleTo(second, first) {
				t.Errorf("%s permits a filter scope conversion", name)
			}
		}
	}
}

// D2 adapters intentionally retain exactly their own handle's layout. A value
// conversion grants the same capabilities as its explicit Expr constructor;
// cross-family, model, value, role and query-phase conversions remain forbidden.
func explicitColumnAdapterConversion(pair string) bool {
	for _, name := range []string{
		"Column", "Enum", "NullableColumn", "NullableNumber", "NullableText", "NullableEnum",
		"InputColumn", "NullableInputColumn", "InputText", "NullableInputText",
		"InputNumber", "NullableInputNumber", "InputTime", "NullableInputTime",
		"InputEnum", "NullableInputEnum", "InputJSON", "NullableInputJSON",
		"InputInterval", "NullableInputInterval", "JSON", "NullableJSON",
		"Interval", "NullableInterval", "Encrypted", "NullableEncrypted", "InputKey",
	} {
		if pair == "orm."+name+" <-> orm."+name+"Adapter" {
			return true
		}
	}
	return false
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
