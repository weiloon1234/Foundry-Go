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
		if dir == "." {
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
	_, isInterface := typ.Underlying().(*types.Interface)
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
