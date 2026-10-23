package orm

import (
	"go/types"
	"os"
	"sort"
	"strings"
	"testing"
)

// TestPublicMethodSurface pins exported fields and methods, and sealed interface
// methods, independently of receiver placement and promotion. Update only for an approved API change.
func TestPublicMethodSurface(t *testing.T) {
	var out strings.Builder
	for _, dir := range []string{".", "./rows", "./expr"} {
		pkg := surfacePackageIn(t, dir, "")
		for _, name := range pkg.Scope().Names() {
			obj, ok := pkg.Scope().Lookup(name).(*types.TypeName)
			if !ok || !obj.Exported() {
				continue
			}
			typ := types.Unalias(obj.Type())
			switch typ.Underlying().(type) {
			case *types.Struct, *types.Interface:
			default:
				continue
			}
			recordMethodSurface(&out, pkg.Name()+"."+name, typ)
		}
		for _, name := range map[string][]string{
			".":      {"FoundryModel", "FoundryOrderBy", "FoundryWhere", "FoundryRelated", "ValueOf", "Pivot"},
			"./rows": {"FromModel", "GroupBy", "Having", "OrderBy", "Distinct", "DistinctOn", "DistinctOnValues", "MapSelected", "As", "Scope", "On", "InnerJoin", "LeftJoin", "RightJoin", "FullJoin", "CrossJoin", "LeftScope", "RightScope", "NullableLeftScope", "NullableRightScope", "LeftNullableScope", "OnEqual", "OnNotEqual", "OnLess", "OnLessNullable", "OnLessOrEqual", "OnLessOrEqualNullable", "OnGreater", "OnGreaterNullable", "OnGreaterOrEqual", "OnGreaterOrEqualNullable", "OnLike", "OnLikeNullable", "OnDistinctFrom", "OnNotDistinctFrom", "OnAnd", "OnOr", "From", "FromRows", "SelectScalar", "WhereScalar", "ExistsQuery", "InQuery", "InNullableQuery", "ScalarQuery", "ScalarNullableQuery", "ScalarRowQuery", "ScalarNullableRowQuery", "Correlate", "ModelScope", "OuterScope", "InnerScope", "OuterNullableScope", "InnerNullableScope", "SelectCorrelatedScalar", "CorrelatedScalarQuery", "CorrelatedScalarNullableQuery", "CorrelatedScalarRowQuery", "CorrelatedScalarNullableRowQuery", "InCorrelatedQuery", "InNullableCorrelatedQuery", "SelectCorrelatedRecord", "ProjectCorrelated", "AsLateral", "LateralScope", "CrossJoinLateral", "InnerJoinLateral", "LeftJoinLateral", "At", "AtNullable", "AtNullableValue"},
			"./expr": {"Related", "Sum", "SumAt", "Avg", "AvgAt", "SumFloat", "SumFloatAt", "AvgFloat", "AvgFloatAt", "SumInterval", "SumIntervalAt", "AvgInterval", "AvgIntervalAt", "Min", "MinAt", "Max", "MaxAt", "CountField", "CountFieldAt", "CountDistinctField", "CountDistinctFieldAt", "FoundryAggregate", "Group", "HavingAnd", "HavingOr", "FoundryHaving", "FoundryGroup", "FoundryKey", "FoundryProjectionOrder", "AtNullable", "AtNullableValue", "FoundryNullableScope", "FoundryNullableScopeData", "Coalesce", "CoalesceValue", "CoalesceNullable", "CoalesceNullableValue", "NullIf", "NullIfValue", "NullIfNullable", "NullIfNullableValue", "When", "WhenValue", "Grouped", "Abs", "AbsNullable", "AbsNullableValue", "AbsValue", "Add", "AddNullable", "AddNullableValue", "AddValue", "CompareValue", "Count", "DecimalNullable", "DecimalNullableValue", "DecimalOf", "DecimalValue", "DistinctFrom", "DistinctFromValue", "Divide", "DivideNullable", "DivideNullableValue", "DivideValue", "Equal", "EqualValue", "Exists", "FloatNullable", "FloatNullableValue", "FloatOf", "FloatValue", "FoundrySelected", "Greater", "GreaterColumn", "GreaterNullable", "GreaterNullableValue", "GreaterOrEqual", "GreaterOrEqualNullable", "GreaterOrEqualNullableValue", "GreaterOrEqualValue", "GreaterValue", "Less", "LessNullable", "LessNullableValue", "LessOrEqual", "LessOrEqualNullable", "LessOrEqualNullableValue", "LessOrEqualValue", "LessValue", "Like", "LikeNullable", "LikeNullableValue", "LikeValue", "Multiply", "MultiplyNullable", "MultiplyNullableValue", "MultiplyValue", "Negate", "NegateNullable", "NegateNullableValue", "NegateValue", "NotDistinctFrom", "NotDistinctFromValue", "NotEqual", "NotEqualValue", "NullFor", "Nullable", "NullableRow", "OrderBy", "OrderNullableRow", "OrderNullableValue", "OrderRow", "OrderValue", "Param", "Remainder", "RemainderNullable", "RemainderNullableValue", "RemainderValue", "Select", "Subtract", "SubtractNullable", "SubtractNullableValue", "SubtractValue", "Where", "Not", "FoundryRowValue", "FoundrySelectedColumn", "FoundryRowExpression", "FoundrySelectedValue", "FoundryPredicateValue"},
		}[dir] {
			out.WriteString(pkg.Name() + "." + name + " " + types.TypeString(pkg.Scope().Lookup(name).Type(), nil) + "\n")
		}
		if dir == "./rows" {
			out.WriteString("rows.Raw " + types.TypeString(pkg.Scope().Lookup("Raw").Type(), nil) + "\n")
		}
		if dir == "." {
			for _, name := range []string{"WithRetrievalHooks", "NewRetrievalObserver", "RegisterRetrievalObserver", "Scope", "WithGlobalScopes", "WithoutGlobalScope", "WithoutGlobalScopes", "WithScopeContext", "WithoutRelationGlobalScope", "WithoutRelationGlobalScopes", "WithoutPivotGlobalScope", "WithoutPivotGlobalScopes", "CursorPaginate", "ChunkByKey", "EachByKey", "HasOneOf", "Has", "DoesntHave", "LoadMissing", "DefaultRelationLimits", "WithRelationLimits", "ThroughOf", "ManyToManyOf", "HasManyThroughOf", "HasOneThroughOf", "MorphManyOf", "MorphOneOf", "MorphToOf", "MorphToManyOf", "MorphedByManyOf", "Mutate", "InputKeyOf", "InputColumnOf", "NullableInputColumnOf", "InputTextOf", "NullableInputTextOf", "InputNumberOf", "NullableInputNumberOf", "InputTimeOf", "NullableInputTimeOf", "InputEnumOf", "NullableInputEnumOf", "InputJSONOf", "NullableInputJSONOf", "InputIntervalOf", "NullableInputIntervalOf", "InputBytesOf", "NullableInputBytesOf", "EncryptedChangeOf", "NullableEncryptedChangeOf", "WithHooks", "WriteGet", "WriteNullableGet", "WriteSet", "WriteIsSet", "ChangeOf", "NullableChangeOf", "Compare", "NewObserver", "NewDeletionObserver", "RegisterObserver", "WithoutModelHooks", "ForceDelete"} {
				out.WriteString("orm." + name + " " + types.TypeString(pkg.Scope().Lookup(name).Type(), nil) + "\n")
			}
			out.WriteString("orm.FoundryColumn " + types.TypeString(pkg.Scope().Lookup("FoundryColumn").Type(), nil) + "\n")
			for _, name := range []string{"NumberKey", "TextKey"} {
				sig := pkg.Scope().Lookup(name).Type().(*types.Signature)
				recordMethodSurface(&out, "orm."+name+" result", sig.Results().At(0).Type())
			}
		}
	}
	const path = "testdata/public-methods.txt"
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if out.String() != string(want) {
		t.Fatalf("public surface changed; preserve pinned fields and signatures (R9) or obtain API approval\nwant:\n%s\ngot:\n%s", want, out.String())
	}
}

func recordMethodSurface(out *strings.Builder, name string, typ types.Type) {
	iface, isInterface := typ.Underlying().(*types.Interface)
	for _, pointer := range []bool{false, true} {
		if pointer && isInterface {
			continue
		}
		receiver := typ
		label := name
		if pointer {
			receiver = types.NewPointer(typ)
			label = "*" + label
		}
		out.WriteString(label + "\n")
		if isInterface && !iface.IsMethodSet() {
			out.WriteString("  constraint " + types.TypeString(iface, nil) + "\n")
		}
		if !pointer {
			for _, field := range exportedSurfaceFields(typ) {
				out.WriteString("  field " + field.Name() + " " + types.TypeString(field.Type(), nil) + "\n")
			}
		}
		methods := types.NewMethodSet(receiver)
		for i := 0; i < methods.Len(); i++ {
			m := methods.At(i)
			if isInterface || m.Obj().Exported() {
				out.WriteString("  " + m.Obj().Name() + " " + types.TypeString(m.Type(), func(p *types.Package) string { return p.Path() }) + "\n")
			}
		}
	}
}

func promotedPublicMethods(pkg *types.Package) []string {
	var violations []string
	for _, name := range pkg.Scope().Names() {
		obj, ok := pkg.Scope().Lookup(name).(*types.TypeName)
		if !ok || !obj.Exported() {
			continue
		}
		named, ok := types.Unalias(obj.Type()).(*types.Named)
		if !ok || named.TypeParams().Len() == 0 {
			continue
		}
		for _, receiver := range []types.Type{named, types.NewPointer(named)} {
			methods := types.NewMethodSet(receiver)
			for i := 0; i < methods.Len(); i++ {
				method := methods.At(i)
				if len(method.Index()) > 1 {
					violations = append(violations, name+"."+method.Obj().Name()+" violates R9: declare a direct shell over shared state/helpers")
				}
			}
		}
	}
	return violations
}

func TestPublicGenericMethodsAreDirect(t *testing.T) {
	for _, dir := range []string{".", "./rows", "./expr"} {
		if violations := promotedPublicMethods(surfacePackageIn(t, dir, "")); len(violations) > 0 {
			t.Fatal(strings.Join(violations, "\n"))
		}
	}
	pkg := surfacePackage(t, `package orm; type Promoted[M any] struct { Column[M,int] }`)
	if len(promotedPublicMethods(pkg)) == 0 {
		t.Fatal("R9 guard accepted promoted generic methods")
	}
}

// Gather candidate names through embedding, then let Go resolve visibility,
// shadowing and ambiguity exactly as it does for consumer selectors.
func exportedSurfaceFields(typ types.Type) []*types.Var {
	names := map[string]bool{}
	seen := map[types.Type]bool{}
	var visit func(types.Type)
	visit = func(t types.Type) {
		t = types.Unalias(t)
		if p, ok := t.(*types.Pointer); ok {
			t = p.Elem()
		}
		if seen[t] {
			return
		}
		seen[t] = true
		s, ok := t.Underlying().(*types.Struct)
		if !ok {
			return
		}
		for i := 0; i < s.NumFields(); i++ {
			f := s.Field(i)
			if f.Exported() {
				names[f.Name()] = true
			}
			if f.Embedded() {
				visit(f.Type())
			}
		}
	}
	visit(typ)
	ordered := make([]string, 0, len(names))
	for name := range names {
		ordered = append(ordered, name)
	}
	sort.Strings(ordered)
	var fields []*types.Var
	for _, name := range ordered {
		obj, _, _ := types.LookupFieldOrMethod(typ, false, nil, name)
		if field, ok := obj.(*types.Var); ok && field.IsField() && field.Exported() {
			fields = append(fields, field)
		}
	}
	return fields
}

func TestSurfaceIncludesPromotedFields(t *testing.T) {
	pkg := surfacePackage(t, `package orm
 type surfaceBase struct { Visible int; Shadow int; Ambiguous int; hidden int }
 type surfaceOther struct { Ambiguous string }
 type SurfaceFields struct { surfaceBase; surfaceOther; Shadow string; Own bool }
 `)
	fields := exportedSurfaceFields(pkg.Scope().Lookup("SurfaceFields").Type())
	var got []string
	for _, field := range fields {
		got = append(got, field.Name()+" "+types.TypeString(field.Type(), nil))
	}
	if strings.Join(got, ", ") != "Own bool, Shadow string, Visible int" {
		t.Fatalf("incorrect visible fields: %v", got)
	}
}
