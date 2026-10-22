package orm

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type surfaceWalker struct {
	seen       map[types.Type]bool
	rootORM    bool
	field      bool
	layerOnly  bool
	violations []string
}

func (w *surfaceWalker) walk(typ types.Type, path string) {
	if typ == nil {
		return
	}
	typ = types.Unalias(typ)
	switch t := typ.(type) {
	case *types.Named:
		obj := t.Obj()
		if pkg := obj.Pkg(); pkg != nil {
			pathName := pkg.Path()
			if w.rootORM && (pathName == "github.com/weiloon1234/Foundry-Go/database/orm/rows" || pathName == "github.com/weiloon1234/Foundry-Go/database/orm/expr") {
				w.violations = append(w.violations, path+" violates R4: reaches "+t.String())
				return
			}
			if !w.layerOnly && w.field && pathName == "github.com/weiloon1234/Foundry-Go/database/orm" && (obj.Name() == "Query" || obj.Name() == "LockedQuery" || obj.Name() == "Table") {
				w.violations = append(w.violations, path+" stores engine-sized orm type "+t.String())
				return
			}
		}
		if w.seen[typ] {
			return
		}
		w.seen[typ] = true

		generic := t.TypeParams().Len() > 0 || t.TypeArgs().Len() > 0
		if pkg := obj.Pkg(); pkg != nil && generic {
			heavy := pkg.Path() == "github.com/weiloon1234/Foundry-Go/database/codec" && obj.Name() == "Codec"
			if pkg.Path() == "github.com/weiloon1234/Foundry-Go/database/query" && obj.Name() != "Page" && obj.Name() != "SimplePage" {
				heavy = true
			}
			if heavy && !w.layerOnly {
				w.violations = append(w.violations, path+" reaches "+t.String())
				return
			}
		}
		for _, receiver := range []types.Type{t, types.NewPointer(t)} {
			methods := types.NewMethodSet(receiver)
			if generic && methods.Len() > 25 && !w.layerOnly {
				w.violations = append(w.violations, fmt.Sprintf("%s has %d methods", path, methods.Len()))
			}
			for i := 0; i < methods.Len(); i++ {
				m := methods.At(i).Obj()
				w.walk(m.Type(), path+"."+m.Name())
			}
		}
		w.walk(t.Underlying(), path)
		for i := 0; i < t.TypeArgs().Len(); i++ {
			w.walk(t.TypeArgs().At(i), path+" type argument")
		}
	case *types.Struct:
		for i := 0; i < t.NumFields(); i++ {
			f := t.Field(i)
			// Function-typed fields are intentionally exempt. Measured evidence:
			// query.GlobalScope[M]'s func field mentioning Query[M] adds only four
			// functions (+4), unlike a heavy struct field or method signature.
			// The exemption applies only here, never to method parameters/results.
			if _, ok := types.Unalias(f.Type()).Underlying().(*types.Signature); ok {
				if w.rootORM && !w.layerOnly {
					layer := surfaceWalker{seen: map[types.Type]bool{}, rootORM: true, layerOnly: true}
					layer.walk(f.Type(), path+"."+f.Name())
					w.violations = append(w.violations, layer.violations...)
				} else if w.layerOnly {
					w.walk(f.Type(), path+"."+f.Name())
				}
				continue
			}
			old := w.field
			w.field = true
			w.walk(f.Type(), path+"."+f.Name())
			w.field = old
		}
	case *types.Signature:
		for _, tuple := range []*types.Tuple{t.Params(), t.Results()} {
			for i := 0; i < tuple.Len(); i++ {
				w.walk(tuple.At(i).Type(), path+" signature")
			}
		}
	case *types.Pointer:
		w.walk(t.Elem(), path+" pointer")
	case *types.Array:
		w.walk(t.Elem(), path+" array")
	case *types.Slice:
		w.walk(t.Elem(), path+" slice")
	case *types.Map:
		w.walk(t.Key(), path+" key")
		w.walk(t.Elem(), path+" value")
	case *types.Chan:
		w.walk(t.Elem(), path+" channel")
	case *types.Interface:
		for i := 0; i < t.NumMethods(); i++ {
			w.walk(t.Method(i).Type(), path+"."+t.Method(i).Name())
		}
		for i := 0; i < t.NumEmbeddeds(); i++ {
			w.walk(t.EmbeddedType(i), path+" embedded")
		}
	}
}
func surfacePackage(t *testing.T, extra string) *types.Package {
	return surfacePackageIn(t, ".", extra)
}
func surfacePackageIn(t *testing.T, dir, extra string) *types.Package {
	pkg, _, _ := surfacePackageSources(t, dir, extra)
	return pkg
}
func surfacePackageSources(t *testing.T, dir, extra string) (*types.Package, []*ast.File, *types.Info) {
	t.Helper()
	command := exec.Command("go", "list", "-export", "-deps", "-json", ".", "github.com/weiloon1234/Foundry-Go/database/orm/rows")
	command.Dir = dir
	data, err := command.Output()
	if err != nil {
		t.Fatal(err)
	}
	exports := map[string]string{}
	absolute, err := filepath.Abs(dir)
	if err != nil {
		t.Fatal(err)
	}
	packagePath := ""
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	for {
		var pkg struct{ ImportPath, Export, Dir string }
		err := decoder.Decode(&pkg)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		exports[pkg.ImportPath] = pkg.Export
		if pkg.Dir == absolute {
			packagePath = pkg.ImportPath
		}
	}
	fset := token.NewFileSet()
	var files []*ast.File
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, file)
	}
	if extra != "" {
		file, err := parser.ParseFile(fset, "injected.go", extra, 0)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, file)
	}
	config := types.Config{Importer: importer.ForCompiler(fset, "gc", func(path string) (io.ReadCloser, error) { return os.Open(exports[path]) })}
	info := &types.Info{Defs: map[*ast.Ident]types.Object{}, Uses: map[*ast.Ident]types.Object{}, Instances: map[*ast.Ident]types.Instance{}}
	pkg, err := config.Check(packagePath, fset, files, info)
	if err != nil {
		t.Fatal(err)
	}
	return pkg, files, info
}
func checkSurface(pkg *types.Package) []string {
	w := surfaceWalker{seen: map[types.Type]bool{}}
	for _, name := range pkg.Scope().Names() {
		obj, ok := pkg.Scope().Lookup(name).(*types.TypeName)
		if !ok {
			continue
		}
		switch typ := obj.Type().(type) {
		case *types.Named:
			if typ.TypeParams().Len() == 0 {
				continue
			}
		case *types.Alias:
			if typ.TypeParams().Len() == 0 {
				continue
			}
		default:
			continue
		}
		w.seen = map[types.Type]bool{}
		w.rootORM = pkg.Path() == "github.com/weiloon1234/Foundry-Go/database/orm" && obj.Exported()
		w.walk(obj.Type(), name)
	}
	return w.violations
}
func TestGenericSurfaceHasNoHeavyEngineTypes(t *testing.T) {
	for _, dir := range []string{".", "./expr", "./rows"} {
		t.Run(dir, func(t *testing.T) {
			pkg := surfacePackageIn(t, dir, "")
			if violations := checkSurface(pkg); len(violations) > 0 {
				t.Fatal(strings.Join(violations, "\n"))
			}
			for _, name := range pkg.Scope().Names() {
				obj, ok := pkg.Scope().Lookup(name).(*types.TypeName)
				if !ok {
					continue
				}
				typ := types.Unalias(obj.Type())
				n, ok := typ.(*types.Named)
				if !ok || n.TypeParams().Len() == 0 {
					continue
				}
				t.Logf("%s: value=%d pointer=%d", name, types.NewMethodSet(typ).Len(), types.NewMethodSet(types.NewPointer(typ)).Len())
			}
		})
	}
}

func TestSurfaceWalkerRejectsTransitivePrivateViolations(t *testing.T) {
	pkg := surfacePackage(t, `package orm
 import "github.com/weiloon1234/Foundry-Go/database/codec"
 type codecAlias[V any] = codec.Codec[V]
 type hiddenHolder[V any] struct { bad codecAlias[V] }
 type leak[V any] struct { hidden *hiddenHolder[V] }
 type privateMethodLeak[V any] struct{}
 func (*privateMethodLeak[V]) hidden() codecAlias[V] { return codecAlias[V]{} }
 `)
	violations := strings.Join(checkSurface(pkg), "\n")
	if !strings.Contains(violations, "hiddenHolder") || !strings.Contains(violations, "privateMethodLeak") {
		t.Fatalf("walker missed private transitive violations: %s", violations)
	}
}

func TestSurfaceWalkerBudgetsAndFunctionFieldExemption(t *testing.T) {
	source := `package orm
 import "github.com/weiloon1234/Foundry-Go/database/query"
 type functionField[V any] struct { allowed func() query.Query[V] }
 type tooMany[V any] struct{}
 `
	for i := 0; i < 26; i++ {
		source += fmt.Sprintf("func (tooMany[V]) m%d() {}\n", i)
	}
	pkg := surfacePackage(t, source)
	violations := strings.Join(checkSurface(pkg), "\n")
	if !strings.Contains(violations, "tooMany has 26 methods") || strings.Contains(violations, "functionField") {
		t.Fatalf("budget/exemption: %s", violations)
	}
}

func TestSurfaceWalkerRejectsORMEngineFieldsAndReverseLayers(t *testing.T) {
	pkg := surfacePackage(t, `package orm
 import "github.com/weiloon1234/Foundry-Go/database/orm/expr"
 import "github.com/weiloon1234/Foundry-Go/database/orm/rows"
 type EngineField[M any] struct{ stored Query[M] }
 type LockedField[M any] struct{ stored *LockedQuery[M] }
 type TableField[M any] struct{ stored Table[M,int] }
 type ReverseField[M any] struct{ stored expr.Predicate[M] }
 type ReverseMethod[M any] struct{}
 func(ReverseMethod[M]) hidden()rows.Field[M,int]{return rows.Field[M,int]{}}
 type ReverseFunction[M any] struct{ callback func()expr.Predicate[M] }
 `)
	violations := strings.Join(checkSurface(pkg), "\n")
	for _, name := range []string{"EngineField", "LockedField", "TableField", "ReverseField", "ReverseMethod", "ReverseFunction"} {
		if !strings.Contains(violations, name) {
			t.Fatalf("missed %s: %s", name, violations)
		}
	}
}
