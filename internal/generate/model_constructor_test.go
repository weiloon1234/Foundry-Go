package generate

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Enumerate the public declaration boundary, not a frozen list of constructor
// names: Builder consumers, option factories and table-capability constructors.
func declarationConstructor(fn *ast.FuncDecl) bool {
	if fn.Recv != nil || !fn.Name.IsExported() {
		return false
	}
	builder := false
	ast.Inspect(fn.Type, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok && id.Name == "Builder" {
			builder = true
		}
		return true
	})
	if builder {
		return true
	}
	if fn.Type.Results == nil {
		return false
	}
	for _, result := range fn.Type.Results.List {
		switch modelConstructorType(result.Type) {
		case "Option", "ColumnOption", "ColumnHandle", "SoftDeletable", "ModelIdentity":
			return true
		}
	}
	return false
}
func modelConstructorType(expr ast.Expr) string {
	switch x := expr.(type) {
	case *ast.Ident:
		return x.Name
	case *ast.IndexExpr:
		return modelConstructorType(x.X)
	case *ast.IndexListExpr:
		return modelConstructorType(x.X)
	}
	return ""
}

func modelGoldenConstructors(t *testing.T) map[string]bool {
	t.Helper()
	used := make(map[string]bool)
	files, err := filepath.Glob("testdata/models/*.golden")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range files {
		f, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			expr := call.Fun
			switch indexed := expr.(type) {
			case *ast.IndexExpr:
				expr = indexed.X
			case *ast.IndexListExpr:
				expr = indexed.X
			}
			if sel, ok := expr.(*ast.SelectorExpr); ok {
				if pkg, ok := sel.X.(*ast.Ident); ok && pkg.Name == "orm" {
					used[sel.Sel.Name] = true
				}
			}
			return true
		})
	}
	return used
}
func TestModelConstructorCompleteness(t *testing.T) {
	used := modelGoldenConstructors(t)
	files, err := os.ReadDir("../../database/orm")
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, file := range files {
		if file.IsDir() || !strings.HasSuffix(file.Name(), ".go") || strings.HasSuffix(file.Name(), "_test.go") {
			continue
		}
		path := filepath.Join("../../database/orm", file.Name())
		f, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || !declarationConstructor(fn) {
				continue
			}
			count++
			if !used[fn.Name.Name] {
				t.Errorf("exported ORM declaration constructor %s has no exercised generator spelling; add its declaration and golden", fn.Name.Name)
			}
		}
	}
	if count == 0 {
		t.Fatal("no ORM declaration constructors were enumerated")
	}
	t.Logf("checked %d exported declaration constructors against generated calls", count)
	// A newly exported constructor must be discovered even when its name and
	// return carrier do not yet appear in the generator.
	f, err := parser.ParseFile(token.NewFileSet(), "new.go", "package orm\nfunc FutureOf[M any](b Builder[M], p *string) Future[M] { panic(0) }", 0)
	if err != nil {
		t.Fatal(err)
	}
	fn := f.Decls[0].(*ast.FuncDecl)
	if !declarationConstructor(fn) || used[fn.Name.Name] {
		t.Fatal("new constructor was not detected as missing a spelling")
	}
}

func TestPlainConstructorFamilies(t *testing.T) {
	source, err := os.ReadFile("testdata/models/constructors.go.txt")
	if err != nil {
		t.Fatal(err)
	}
	dir := fixture(t, string(source))
	if _, err := generateFixture(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	generated := generatedSnapshot(t, dir)["models_foundry.gen.go"]
	for _, field := range []string{"Single", "WriterID", "IsPublic", "PresentationDeclared", "Execution", "Bytes", "Hash", "Number", "Amount", "State", "Instant", "DateTime", "Date", "Time", "Local", "Duration", "Document"} {
		if !strings.Contains(generated, "orm.ColumnOf(b, &m."+field) {
			t.Errorf("plain constructor unreachable for %s", field)
		}
	}
}
