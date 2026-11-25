package modelcheck

import (
	"context"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/internal/frameworkinfo"
)

type testImporter map[string]*types.Package

func (i testImporter) Import(path string) (*types.Package, error) {
	if p := i[path]; p != nil {
		return p, nil
	}
	return nil, fmt.Errorf("unexpected import %s", path)
}

func testAnalysis(t *testing.T, body string) (*Analysis, *ast.File) {
	t.Helper()
	return testAnalysisNamed(t, "sample.go", body)
}

func testAnalysisNamed(t *testing.T, fileName, body string) (*Analysis, *ast.File) {
	t.Helper()
	return testAnalysisInPackage(t, "example/sample", fileName, body)
}

func testAnalysisInPackage(t *testing.T, pkgPath, fileName, body string) (*Analysis, *ast.File) {
	t.Helper()
	fset := token.NewFileSet()
	imports := testImporter{}
	check := func(path, name, source string, info *types.Info) (*types.Package, *ast.File) {
		file, err := parser.ParseFile(fset, name, source, parser.ParseComments)
		if err != nil {
			t.Fatal(err)
		}
		config := types.Config{Importer: imports}
		pkg, err := config.Check(path, fset, []*ast.File{file}, info)
		if err != nil {
			t.Fatal(err)
		}
		imports[path] = pkg
		return pkg, file
	}

	ormPath := frameworkinfo.ModulePath + "/database/orm"
	check(ormPath, "orm.go", `package orm
type CheckedTable interface{ ormError() error }
type Builder[M any] struct{}
type Option struct{}
type Table[M any, K comparable] struct{}
func TableOf[M any,K comparable](b Builder[M],p *K) Table[M,K] {return Table[M,K]{}}
func (t Table[M, K]) ormError() error { return nil }
func Define[M any, T CheckedTable](name string, declare func(Builder[M], *M) T, opts ...Option) T {
	var zero T
	return zero
}
`, nil)

	info := &types.Info{
		Types:      make(map[ast.Expr]types.TypeAndValue),
		Defs:       make(map[*ast.Ident]types.Object),
		Uses:       make(map[*ast.Ident]types.Object),
		Instances:  make(map[*ast.Ident]types.Instance),
		Selections: make(map[*ast.SelectorExpr]*types.Selection),
	}
	pkg, file := check(pkgPath, fileName, body, info)
	a := NewAnalysis(info, fset)
	a.Package = pkg
	a.Index(file)
	return a, file
}

func TestDirectCall(t *testing.T) {
	body := `package sample
import "github.com/weiloon1234/Foundry-Go/database/orm"
type User struct{ ID int64 }
type UserTable struct{ orm.Table[User, int64] }
var Users = orm.Define("users", func(b orm.Builder[User], u *User) UserTable { return UserTable{} })
`
	a, file := testAnalysis(t, body)
	diagnostics := a.CheckPackage([]*ast.File{file})
	if len(diagnostics) != 1 {
		t.Fatalf("expected 1 finding, got %d: %v", len(diagnostics), diagnostics)
	}
	d := diagnostics[0]
	if d.Subject != "model User" {
		t.Errorf("expected target 'model User', got %q", d.Subject)
	}
	if d.Fix != directiveReplacement {
		t.Errorf("expected message %q, got %q", directiveReplacement, d.Fix)
	}
	if d.Line != 5 {
		t.Errorf("expected position line 5, got %d", d.Line)
	}
	if text := d.String(); d.Column < 1 || !strings.Contains(text, "[model-table] model User: table is declared by hand; put the //foundry:model directive") || !strings.HasSuffix(text, ". Foundry guide: model-declarations#recipe-declare-table") {
		t.Errorf("finding lost its rule, fix or teaching: %q", text)
	}
}

func TestImportAlias(t *testing.T) {
	body := `package sample
import o "github.com/weiloon1234/Foundry-Go/database/orm"
type User struct{ ID int64 }
type UserTable struct{ o.Table[User, int64] }
var Users = o.Define("users", func(b o.Builder[User], u *User) UserTable { return UserTable{} })
`
	a, file := testAnalysis(t, body)
	diagnostics := a.CheckPackage([]*ast.File{file})
	if len(diagnostics) != 1 {
		t.Fatalf("expected 1 finding, got %d: %v", len(diagnostics), diagnostics)
	}
	if diagnostics[0].Subject != "model User" {
		t.Errorf("expected target 'model User', got %q", diagnostics[0].Subject)
	}
}

func TestDotImport(t *testing.T) {
	body := `package sample
import . "github.com/weiloon1234/Foundry-Go/database/orm"
type User struct{ ID int64 }
type UserTable struct{ Table[User, int64] }
var Users = Define("users", func(b Builder[User], u *User) UserTable { return UserTable{} })
`
	a, file := testAnalysis(t, body)
	diagnostics := a.CheckPackage([]*ast.File{file})
	if len(diagnostics) != 1 {
		t.Fatalf("expected 1 finding, got %d: %v", len(diagnostics), diagnostics)
	}
	if diagnostics[0].Subject != "model User" {
		t.Errorf("expected target 'model User', got %q", diagnostics[0].Subject)
	}
}

func TestFunctionValue(t *testing.T) {
	body := `package sample
import "github.com/weiloon1234/Foundry-Go/database/orm"
type User struct{ ID int64 }
type UserTable struct{ orm.Table[User, int64] }
var def = orm.Define[User, UserTable]
var Users = def("users", func(b orm.Builder[User], u *User) UserTable { return UserTable{} })
`
	a, file := testAnalysis(t, body)
	diagnostics := a.CheckPackage([]*ast.File{file})
	if len(diagnostics) != 1 {
		t.Fatalf("expected 1 finding, got %d: %v", len(diagnostics), diagnostics)
	}
	if diagnostics[0].Subject != "model User" {
		t.Errorf("expected target 'model User', got %q", diagnostics[0].Subject)
	}
}

func TestTableBaseEmbeddedThroughTypeAlias(t *testing.T) {
	body := `package sample
import "github.com/weiloon1234/Foundry-Go/database/orm"
type User struct{ ID int64 }
type MyBase = orm.Table[User, int64]
type UserTable struct{ MyBase }
`
	a, file := testAnalysis(t, body)
	diagnostics := a.CheckPackage([]*ast.File{file})
	if len(diagnostics) != 1 {
		t.Fatalf("expected 1 finding, got %d: %v", len(diagnostics), diagnostics)
	}
	if diagnostics[0].Subject != "model User" {
		t.Errorf("expected target 'model User', got %q", diagnostics[0].Subject)
	}
	if diagnostics[0].Line != 5 {
		t.Errorf("expected line 5, got %d", diagnostics[0].Line)
	}
}

func TestDirectTableBaseEmbeddedWithoutDefine(t *testing.T) {
	body := `package sample
import "github.com/weiloon1234/Foundry-Go/database/orm"
type User struct{ ID int64 }
type UserTable struct{ orm.Table[User, int64] }
`
	a, file := testAnalysis(t, body)
	diagnostics := a.CheckPackage([]*ast.File{file})
	if len(diagnostics) != 1 {
		t.Fatalf("expected 1 finding, got %d: %v", len(diagnostics), diagnostics)
	}
	if diagnostics[0].Subject != "model User" {
		t.Errorf("expected target 'model User', got %q", diagnostics[0].Subject)
	}
}

func TestGenericTableBaseEmbedded(t *testing.T) {
	// Decision 21: type-parameterised table struct produces 0 findings (reported before, not reported now).
	body := `package sample
import "github.com/weiloon1234/Foundry-Go/database/orm"
type GenericTable[M any, K comparable] struct{ orm.Table[M, K] }
`
	a, file := testAnalysis(t, body)
	diagnostics := a.CheckPackage([]*ast.File{file})
	if len(diagnostics) != 0 {
		t.Fatalf("expected 0 findings for generic table struct, got %d: %v", len(diagnostics), diagnostics)
	}
}

func TestGenericModelDeclarationPasses(t *testing.T) {
	// Decision 21: generic model declaration through orm.Define produces 0 findings (reported before, not reported now).
	body := `package sample
import "github.com/weiloon1234/Foundry-Go/database/orm"
type Run[R any] struct{ ID int64 }
type RunTable[R any] struct{ orm.Table[Run[R], int64] }
func Runs[R any]() RunTable[R] {
	return orm.Define("runs", func(b orm.Builder[Run[R]], m *Run[R]) RunTable[R] {
		return RunTable[R]{}
	})
}
`
	a, file := testAnalysis(t, body)
	diagnostics := a.CheckPackage([]*ast.File{file})
	if len(diagnostics) != 0 {
		t.Fatalf("expected 0 findings for generic model declaration, got %d: %v", len(diagnostics), diagnostics)
	}
}

func TestORMTreeDeclarationPasses(t *testing.T) {
	// Decision 22: declaration inside the ORM package tree produces 0 findings (reported before, not reported now).
	ormTreePkg := frameworkinfo.ModulePath + "/database/orm/internal/enuminit"
	body := `package enuminit
import "github.com/weiloon1234/Foundry-Go/database/orm"
type Model struct{ ID int64 }
type ModelTable struct{ orm.Table[Model, int64] }
var Models = orm.Define("models", func(b orm.Builder[Model], m *Model) ModelTable {
	return ModelTable{}
})
`
	a, file := testAnalysisInPackage(t, ormTreePkg, "a_models.go", body)
	diagnostics := a.CheckPackage([]*ast.File{file})
	if len(diagnostics) != 0 {
		t.Fatalf("expected 0 findings for declaration inside ORM package tree, got %d: %v", len(diagnostics), diagnostics)
	}
}

func TestORMTreeStructEmbeddingPasses(t *testing.T) {
	// Decision 22: struct embedding orm.Table inside the ORM package tree produces 0 findings (reported before, not reported now).
	ormTreePkg := frameworkinfo.ModulePath + "/database/orm/internal/testhelper"
	body := `package testhelper
import "github.com/weiloon1234/Foundry-Go/database/orm"
type Model struct{ ID int64 }
type ModelTable struct{ orm.Table[Model, int64] }
`
	a, file := testAnalysisInPackage(t, ormTreePkg, "testhelper.go", body)
	diagnostics := a.CheckPackage([]*ast.File{file})
	if len(diagnostics) != 0 {
		t.Fatalf("expected 0 findings for struct embedding inside ORM package tree, got %d: %v", len(diagnostics), diagnostics)
	}
}

func TestOutsideORMTreeStillReported(t *testing.T) {
	// Concrete model in an ordinary package outside ORM tree is still reported.
	outsidePkg := "example/myapp/models"
	body := `package models
import "github.com/weiloon1234/Foundry-Go/database/orm"
type Item struct{ ID int64 }
type ItemTable struct{ orm.Table[Item, int64] }
var Items = orm.Define("items", func(b orm.Builder[Item], m *Item) ItemTable {
	return ItemTable{}
})
`
	a, file := testAnalysisInPackage(t, outsidePkg, "item.go", body)
	diagnostics := a.CheckPackage([]*ast.File{file})
	if len(diagnostics) != 1 {
		t.Fatalf("expected 1 finding for concrete model outside ORM tree, got %d: %v", len(diagnostics), diagnostics)
	}
	if diagnostics[0].Subject != "model Item" {
		t.Errorf("expected target 'model Item', got %q", diagnostics[0].Subject)
	}
}

func TestTablePassedAsParameterPasses(t *testing.T) {
	body := `package sample
import "github.com/weiloon1234/Foundry-Go/database/orm"
type User struct{ ID int64 }
func Process(t orm.Table[User, int64]) {}
func ReturnTable(t orm.Table[User, int64]) orm.Table[User, int64] { return t }
`
	a, file := testAnalysis(t, body)
	diagnostics := a.CheckPackage([]*ast.File{file})
	if len(diagnostics) != 0 {
		t.Fatalf("expected 0 findings, got %d: %v", len(diagnostics), diagnostics)
	}
}

func TestTestFileDeclarationsPass(t *testing.T) {
	body := `package sample
import "github.com/weiloon1234/Foundry-Go/database/orm"
type User struct{ ID int64 }
type UserTable struct{ orm.Table[User, int64] }
var Users = orm.Define("users", func(b orm.Builder[User], u *User) UserTable { return UserTable{} })
`
	// In a _test.go file, table declarations pass (0 findings)
	a, testFile := testAnalysisNamed(t, "sample_test.go", body)
	testDiagnostics := a.CheckPackage([]*ast.File{testFile})
	if len(testDiagnostics) != 0 {
		t.Fatalf("expected 0 findings in test file, got %d: %v", len(testDiagnostics), testDiagnostics)
	}

	// The exact same declaration in a non-test file is a finding
	a2, nonTestFile := testAnalysisNamed(t, "sample.go", body)
	nonTestDiagnostics := a2.CheckPackage([]*ast.File{nonTestFile})
	if len(nonTestDiagnostics) != 1 {
		t.Fatalf("expected 1 finding in non-test file, got %d: %v", len(nonTestDiagnostics), nonTestDiagnostics)
	}
	if nonTestDiagnostics[0].Subject != "model User" {
		t.Errorf("expected target 'model User', got %q", nonTestDiagnostics[0].Subject)
	}
}

func TestTestFileMultipleDefinesPass(t *testing.T) {
	body := `package sample
import "github.com/weiloon1234/Foundry-Go/database/orm"
type User struct{ ID int64 }
type UserTable struct{ orm.Table[User, int64] }
var T1 = orm.Define("users1", func(b orm.Builder[User], u *User) UserTable { return UserTable{} })
var T2 = orm.Define("users2", func(b orm.Builder[User], u *User) UserTable { return UserTable{} })
`
	// In a _test.go file, multiple defines pass
	a, testFile := testAnalysisNamed(t, "sample_test.go", body)
	diagnostics := a.CheckPackage([]*ast.File{testFile})
	if len(diagnostics) != 0 {
		t.Fatalf("expected 0 findings for defines in test file, got %d: %v", len(diagnostics), diagnostics)
	}

	// In a non-test file, both defines are findings
	a2, nonTestFile := testAnalysisNamed(t, "sample.go", body)
	nonTestDiagnostics := a2.CheckPackage([]*ast.File{nonTestFile})
	if len(nonTestDiagnostics) != 2 {
		t.Fatalf("expected 2 findings in non-test file, got %d: %v", len(nonTestDiagnostics), nonTestDiagnostics)
	}
}

func TestGeneratedOwnership(t *testing.T) {
	dir := t.TempDir()
	genFile := filepath.Join(dir, "user_foundry.gen.go")
	content := `// Code generated by foundry generate; DO NOT EDIT.
package sample
import "github.com/weiloon1234/Foundry-Go/database/orm"
type User struct{ ID int64 }
type UserTable struct{ orm.Table[User, int64] }
var Users = orm.Define("users", func(b orm.Builder[User], u *User) UserTable { return UserTable{} })
`
	if err := os.WriteFile(genFile, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

	// Case 1: Header present, but not recorded in manifest -> FAILS (1 finding)
	a, file := testAnalysisNamed(t, genFile, content)
	diagnostics := a.CheckPackage([]*ast.File{file})
	if len(diagnostics) != 1 {
		t.Fatalf("expected 1 finding when missing manifest, got %d: %v", len(diagnostics), diagnostics)
	}

	// Case 2: Recorded in manifest AND header present -> PASSES (0 findings)
	manifestData := map[string]any{
		"version": 1,
		"files": map[string]string{
			"user_foundry.gen.go": "some-hash",
		},
	}
	manifestBytes, err := json.Marshal(manifestData)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, frameworkinfo.GenerationManifest), manifestBytes, 0600); err != nil {
		t.Fatal(err)
	}

	a2, file2 := testAnalysisNamed(t, genFile, content)
	diagnostics2 := a2.CheckPackage([]*ast.File{file2})
	if len(diagnostics2) != 0 {
		t.Fatalf("expected 0 findings for valid generated-owned file, got %d: %v", len(diagnostics2), diagnostics2)
	}

	// Case 3: In manifest but without generated header -> FAILS (1 finding)
	noHeaderContent := `package sample
import "github.com/weiloon1234/Foundry-Go/database/orm"
type User struct{ ID int64 }
type UserTable struct{ orm.Table[User, int64] }
var Users = orm.Define("users", func(b orm.Builder[User], u *User) UserTable { return UserTable{} })
`
	noHeaderFile := filepath.Join(dir, "noheader_foundry.gen.go")
	if err := os.WriteFile(noHeaderFile, []byte(noHeaderContent), 0600); err != nil {
		t.Fatal(err)
	}
	manifestData["files"] = map[string]string{
		"noheader_foundry.gen.go": "some-hash",
	}
	manifestBytes, _ = json.Marshal(manifestData)
	_ = os.WriteFile(filepath.Join(dir, frameworkinfo.GenerationManifest), manifestBytes, 0600)

	a3, file3 := testAnalysisNamed(t, noHeaderFile, noHeaderContent)
	diagnostics3 := a3.CheckPackage([]*ast.File{file3})
	if len(diagnostics3) != 1 {
		t.Fatalf("expected 1 finding when header missing, got %d: %v", len(diagnostics3), diagnostics3)
	}
}

func TestCheckCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := Check(ctx, t.TempDir(), nil)
	if err == nil {
		t.Fatal("expected error on canceled context")
	}

	expired, cancelExpired := context.WithDeadline(t.Context(), time.Now().Add(-time.Second))
	defer cancelExpired()
	_, err = Check(expired, t.TempDir(), nil)
	if err == nil {
		t.Fatal("expected error on expired deadline")
	}
}

func TestGenericFactoriesAndComputedNames(t *testing.T) {
	const pre = `package sample
import "github.com/weiloon1234/Foundry-Go/database/orm"
type User struct{ ID int64 }
type Run[R any] struct{ ID int64; Value R }
func define[M any](name string) orm.Table[M,int64] { return orm.Define(name, func(b orm.Builder[M], m *M) orm.Table[M,int64] { return orm.Table[M,int64]{} }) }
func pass[M any](table orm.Table[M,int64]) orm.Table[M,int64] { return table }
`
	for _, tc := range []struct {
		name, source string
		want         int
	}{
		{"generic receiver", `type Factory[M any] struct{};func (f Factory[M]) Build(name string) orm.Table[M,int64] {return orm.Define(name,func(b orm.Builder[M],m *M) orm.Table[M,int64]{return orm.Table[M,int64]{}})};var Users=Factory[User]{}.Build("users")`, 1},
		{"generic receiver expression", `type Factory[M any] struct{};func (f Factory[M]) Build(name string) orm.Table[M,int64] {return orm.Define(name,func(b orm.Builder[M],m *M) orm.Table[M,int64]{return orm.Table[M,int64]{}})};var Users=Factory[User].Build(Factory[User]{},"users")`, 1},
		{"nongeneric method", `type Factory struct{};func (Factory) Build(name string) orm.Table[User,int64] {return orm.Define(name,func(b orm.Builder[User],m *User) orm.Table[User,int64]{return orm.Table[User,int64]{}})};var Users=Factory{}.Build("users")`, 1},
		{"auxiliary string", `func env()string{return "prod"};func custom[M any](name,env string) orm.Table[M,int64]{return orm.Define(name,func(b orm.Builder[M],m *M) orm.Table[M,int64]{return orm.Table[M,int64]{}})};var Users=custom[User]("users",env())`, 1},
		{"nongeneric wrapper", `func custom(name string) orm.Table[User,int64]{return orm.Define(name,func(b orm.Builder[User],m *User) orm.Table[User,int64]{return orm.Table[User,int64]{}})};var Users=custom("users")`, 1},
		{"second parameter", `func env()string{return "prod"};func custom[M any](env,name string) orm.Table[M,int64]{return orm.Define(name,func(b orm.Builder[M],m *M) orm.Table[M,int64]{return orm.Table[M,int64]{}})};var Users=custom[User](env(),"users")`, 1},
		{"computed argument", `func env()string{return "prod"};var Users=define[User](env())`, 0},
		{"concrete factory", `var Users=define[User]("users")`, 1},
		{"generic model", `var Runs=define[Run[string]]("runs")`, 0},
		{"passthrough", `func identity(t orm.Table[User,int64]) orm.Table[User,int64] {return pass(t)}`, 0},
		{"existing nongeneric table", `func existing() orm.Table[User,int64] {return orm.Table[User,int64]{}};var Users=existing()`, 0},
		{"computed", `type UserTable struct{orm.Table[User,int64]};func table(name string) UserTable{return orm.Define(name,func(b orm.Builder[User],m *User) UserTable{return UserTable{orm.TableOf(b,&m.ID)}})}`, 0},
		{"folded", `var Users=orm.Define("us"+"ers",func(b orm.Builder[User],m *User) orm.Table[User,int64]{return orm.Table[User,int64]{}})`, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, f := testAnalysis(t, pre+tc.source)
			got := a.CheckPackage([]*ast.File{f})
			if len(got) != tc.want {
				t.Fatalf("findings: %v; want %d", got, tc.want)
			}
		})
	}
}
