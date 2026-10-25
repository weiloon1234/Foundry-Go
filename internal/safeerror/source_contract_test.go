package safeerror_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
)

func TestRuntimeErrorConstructionUsesDiagnosticOwners(t *testing.T) {
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate framework source for diagnostic construction contract")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(source), "../.."))
	var violations []string
	var sources []diagnosticSource
	files := token.NewFileSet()
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if entry.IsDir() {
			if relative != "." && excludedDiagnosticSourceDirectory(relative, entry.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(relative, ".go") || strings.HasSuffix(relative, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(files, path, nil, parser.ParseComments)
		if err != nil {
			return err
		}
		sources = append(sources, diagnosticSource{relative: relative, file: file})
		imports := make(map[string]string)
		for _, imported := range file.Imports {
			importPath, err := strconv.Unquote(imported.Path.Value)
			if err != nil {
				return err
			}
			if importPath != "fmt" && importPath != "errors" && importPath != "github.com/weiloon1234/Foundry-Go/cli" && importPath != "github.com/weiloon1234/Foundry-Go/fault" && importPath != "github.com/weiloon1234/Foundry-Go/internal/safeerror" {
				continue
			}
			name := filepath.Base(importPath)
			if imported.Name != nil {
				name = imported.Name.Name
			}
			if name == "." {
				violations = append(violations, relative+": use named errors/fault/cli/safeerror imports so diagnostic ownership remains explicit")
			}
			imports[name] = importPath
		}
		check := func(node ast.Node, owner string, initializer ast.Expr, inFunction bool) {
			var stack []ast.Node
			depth := 0
			ast.Inspect(node, func(current ast.Node) bool {
				if current == nil {
					if _, ok := stack[len(stack)-1].(*ast.FuncLit); ok {
						depth--
					}
					stack = stack[:len(stack)-1]
					return true
				}
				stack = append(stack, current)
				if _, ok := current.(*ast.FuncLit); ok {
					depth++
				}
				if expression, ok := current.(*ast.SelectorExpr); ok {
					pkg, name := diagnosticConstructor(expression, imports)
					if pkg == "github.com/weiloon1234/Foundry-Go/internal/safeerror" && name == "Sentinel" {
						var parent *ast.CallExpr
						if len(stack) > 1 {
							parent, _ = stack[len(stack)-2].(*ast.CallExpr)
						}
						if parent == nil || parent.Fun != expression {
							position := files.Position(expression.Pos())
							violations = append(violations, relative+":"+strconv.Itoa(position.Line)+": call safeerror.Sentinel directly in a package variable initializer; do not retain the registration function")
						}
					}
				}
				call, ok := current.(*ast.CallExpr)
				if !ok {
					return true
				}
				if unownedCLIUsage(call.Fun, imports, relative, owner) {
					position := files.Position(call.Pos())
					violations = append(violations, relative+":"+strconv.Itoa(position.Line)+" ("+owner+"): capture framework CLI usage diagnostics with internal/clidiag.Usage, or ownedUsage inside cli; public cli.Usage remains untrusted")
				}
				pkg, name := diagnosticConstructor(call.Fun, imports)
				if pkg == "" {
					return true
				}
				position := files.Position(call.Pos())
				site := relative + ":" + strconv.Itoa(position.Line) + " (" + owner + ")"
				if pkg == "github.com/weiloon1234/Foundry-Go/internal/safeerror" && name == "Sentinel" {
					if inFunction || depth != 0 || initializer != call {
						violations = append(violations, site+": register safeerror.Sentinel only as a direct package variable initializer; request-time registration retains unbounded state")
					}
					return true
				}
				if pkg == "errors" && name == "New" || pkg == "fmt" && name == "Errorf" {
					if !allowedPlainDiagnostic(relative, owner, pkg, name) {
						violations = append(violations, site+": use a safeerror construction owner or declare an intentional presentation boundary")
					}
					return true
				}
				if pkg == "errors" && name == "Join" {
					if relative != "internal/safeerror/diagnostic.go" || owner != "Join" {
						violations = append(violations, site+": use safeerror.Join to retain captured diagnostics and singleton identity")
					}
					return true
				}
				if pkg != "github.com/weiloon1234/Foundry-Go/fault" || name != "New" && name != "Wrap" && name != "Panic" {
					return true
				}
				if relative == "internal/safeerror/diagnostic.go" && (owner == "WrapFault" && name == "Wrap" || owner == "Panic" && name == "Panic" || owner == "Sentinel" && name == "New") {
					return true
				}
				violations = append(violations, site+": construct trusted diagnostics with safeerror.NewFault/WrapFault, generic retained failures with WrapFailure, contained panics with safeerror.Panic, and public fault sentinels with package-level safeerror.Sentinel")
				return true
			})
		}
		for _, declaration := range file.Decls {
			switch declaration := declaration.(type) {
			case *ast.FuncDecl:
				if declaration.Body != nil {
					check(declaration.Body, declaration.Name.Name, nil, true)
				}
			case *ast.GenDecl:
				for _, specification := range declaration.Specs {
					values, ok := specification.(*ast.ValueSpec)
					if !ok {
						continue
					}
					for index, expression := range values.Values {
						owner := values.Names[0].Name
						if len(values.Values) == len(values.Names) {
							owner = values.Names[index].Name
						}
						check(expression, owner, expression, false)
					}
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	violations = append(violations, unownedEnumReturns(files, sources)...)
	violations = append(violations, checkTypedDiagnosticOwners(sources)...)
	sort.Strings(violations)
	for _, violation := range violations {
		t.Error(violation)
	}
}

func excludedDiagnosticSourceDirectory(relative, name string) bool {
	if strings.HasPrefix(name, ".") || name == "node_modules" || name == "vendor" {
		return true
	}
	switch relative {
	case "tests", "tools", "testkit", "internal/generate", "internal/cmd", "internal/testinputs", "internal/errorgraphtest", "internal/scheduletest", "internal/websockettest":
		return true
	}
	return false
}

func diagnosticConstructor(expression ast.Expr, imports map[string]string) (string, string) {
	switch expression := expression.(type) {
	case *ast.ParenExpr:
		return diagnosticConstructor(expression.X, imports)
	case *ast.SelectorExpr:
		if name, ok := expression.X.(*ast.Ident); ok && name.Obj == nil {
			return imports[name.Name], expression.Sel.Name
		}
	case *ast.Ident:
		if expression.Obj == nil {
			return imports["."], expression.Name
		}
	}
	return "", ""
}

func unownedCLIUsage(expression ast.Expr, imports map[string]string, relative, owner string) bool {
	if parenthesized, ok := expression.(*ast.ParenExpr); ok {
		return unownedCLIUsage(parenthesized.X, imports, relative, owner)
	}
	pkg, name := diagnosticConstructor(expression, imports)
	if filepath.Dir(relative) == "cli" {
		if identifier, ok := expression.(*ast.Ident); ok && identifier.Name == "Usage" && (identifier.Obj == nil || identifier.Obj.Kind == ast.Fun) {
			pkg, name = "github.com/weiloon1234/Foundry-Go/cli", "Usage"
		}
	}
	if pkg != "github.com/weiloon1234/Foundry-Go/cli" || name != "Usage" {
		return false
	}
	return !(relative == "cli/diagnostics.go" && owner == "ownedUsage" || relative == "internal/clidiag/usage.go" && owner == "Usage")
}

func TestCLIUsageConstructionGuardRejectsPublicFactoryCalls(t *testing.T) {
	for _, test := range []struct {
		name, relative, owner, source string
		want                          bool
	}{
		{"framework command", "audit/command/command.go", "Run", `package command; import cli "github.com/weiloon1234/Foundry-Go/cli"; func Run() { cli.Usage("framework message") }`, true},
		{"aliased framework command", "audit/command/command.go", "Run", `package command; import commands "github.com/weiloon1234/Foundry-Go/cli"; func Run() { commands.Usage("framework message") }`, true},
		{"same package", "cli/arguments.go", "Run", `package cli; func Usage(string) {}; func Run() { Usage("framework message") }`, true},
		{"other file declaration", "cli/command.go", "Run", `package cli; func Run() { Usage("framework message") }`, true},
		{"parenthesized local call", "cli/command.go", "Run", `package cli; func Run() { (Usage)("framework message") }`, true},
		{"shared source owner", "internal/clidiag/usage.go", "Usage", `package clidiag; import cli "github.com/weiloon1234/Foundry-Go/cli"; func Run() { cli.Usage("framework message") }`, false},
		{"private source owner", "cli/diagnostics.go", "ownedUsage", `package cli; func Run() { Usage("framework message") }`, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			file, err := parser.ParseFile(token.NewFileSet(), "fixture.go", test.source, 0)
			if err != nil {
				t.Fatal(err)
			}
			imports := map[string]string{}
			for _, entry := range file.Imports {
				path, err := strconv.Unquote(entry.Path.Value)
				if err != nil {
					t.Fatal(err)
				}
				name := filepath.Base(path)
				if entry.Name != nil {
					name = entry.Name.Name
				}
				imports[name] = path
			}
			rejected := false
			ast.Inspect(file, func(node ast.Node) bool {
				if call, ok := node.(*ast.CallExpr); ok {
					rejected = rejected || unownedCLIUsage(call.Fun, imports, test.relative, test.owner)
				}
				return true
			})
			if rejected != test.want {
				t.Fatal("CLI usage source ownership guard accepted a raw constructor or rejected its owner")
			}
		})
	}
}

func allowedPlainDiagnostic(path, owner, pkg, name string) bool {
	if pkg == "errors" && name == "New" {
		return path == "internal/safeerror/diagnostic.go" && owner == "New" || path == "cli/arguments.go" && owner == "Usage"
	}
	// CLI tooling presents subprocess/tool diagnostics to the invoking operator;
	// these are not application callback errors or runtime reports.
	switch path {
	case "internal/agent/wire.go", "internal/agent/document.go", "internal/agent/session.go", "internal/agent/batch.go", "internal/frameworkinfo/build.go", "cmd/foundry/contracts.go", "cmd/foundry/scaffold.go":
		return true
	}
	return false
}

func TestPlainErrorConstructionGuard(t *testing.T) {
	for _, call := range []struct{ pkg, name string }{{"errors", "New"}, {"fmt", "Errorf"}} {
		if allowedPlainDiagnostic("cache/new.go", "run", call.pkg, call.name) {
			t.Fatal("raw runtime diagnostic accepted")
		}
	}
	if allowedPlainDiagnostic("cli/arguments.go", "anotherConstructor", "errors", "New") {
		t.Fatal("public constructor allowance spread to another function")
	}
}
