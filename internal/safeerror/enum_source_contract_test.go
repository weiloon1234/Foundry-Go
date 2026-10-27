package safeerror_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

type diagnosticSource struct {
	relative string
	file     *ast.File
}

// Discover constants from their declarations, rather than duplicating an enum
// list. Only direct error result positions are checked: classification values
// and application-supplied enum variables do not establish source ownership.
func unownedEnumReturns(positions *token.FileSet, sources []diagnosticSource) []string {
	const module = "github.com/weiloon1234/Foundry-Go/"
	stringTypes, errorTypes := map[string]bool{}, map[string]bool{}
	packageOf := func(source diagnosticSource) string { return module + filepath.ToSlash(filepath.Dir(source.relative)) }
	for _, source := range sources {
		pkg := packageOf(source)
		for _, declaration := range source.file.Decls {
			switch declaration := declaration.(type) {
			case *ast.GenDecl:
				for _, spec := range declaration.Specs {
					if typ, ok := spec.(*ast.TypeSpec); ok {
						if underlying, ok := typ.Type.(*ast.Ident); ok && underlying.Name == "string" && !typ.Assign.IsValid() {
							stringTypes[pkg+"."+typ.Name.Name] = true
						}
					}
				}
			case *ast.FuncDecl:
				if declaration.Name.Name == "Error" && declaration.Recv != nil && len(declaration.Recv.List) == 1 {
					if receiver, ok := declaration.Recv.List[0].Type.(*ast.Ident); ok {
						errorTypes[pkg+"."+receiver.Name] = true
					}
				}
			}
		}
	}
	constants := map[string]token.Pos{}
	for _, source := range sources {
		pkg := packageOf(source)
		for _, declaration := range source.file.Decls {
			group, ok := declaration.(*ast.GenDecl)
			if !ok || group.Tok != token.CONST {
				continue
			}
			var inherited ast.Expr
			for _, spec := range group.Specs {
				value := spec.(*ast.ValueSpec)
				if value.Type != nil || len(value.Values) != 0 {
					inherited = value.Type
				}
				typ, ok := inherited.(*ast.Ident)
				if !ok || !stringTypes[pkg+"."+typ.Name] || !errorTypes[pkg+"."+typ.Name] {
					continue
				}
				for _, name := range value.Names {
					constants[pkg+"."+name.Name] = name.Pos()
				}
			}
		}
	}
	var violations []string
	for _, source := range sources {
		imports := map[string]string{}
		for _, imported := range source.file.Imports {
			path, _ := strconv.Unquote(imported.Path.Value)
			name := filepath.Base(path)
			if imported.Name != nil {
				name = imported.Name.Name
			}
			imports[name] = path
		}
		var inspectFunction func(*ast.FuncType, *ast.BlockStmt)
		inspectFunction = func(signature *ast.FuncType, body *ast.BlockStmt) {
			if body == nil {
				return
			}
			var errorResults []bool
			namedErrors := map[*ast.Object]bool{}
			if signature.Results != nil {
				for _, field := range signature.Results.List {
					name, ok := field.Type.(*ast.Ident)
					isError := ok && name.Name == "error" && name.Obj == nil
					for _, result := range field.Names {
						if isError {
							namedErrors[result.Obj] = true
						}
					}
					count := max(1, len(field.Names))
					for range count {
						errorResults = append(errorResults, isError)
					}
				}
			}
			ast.Inspect(body, func(node ast.Node) bool {
				if literal, ok := node.(*ast.FuncLit); ok {
					inspectFunction(literal.Type, literal.Body)
					return false
				}
				var expressions []ast.Expr
				var resultPositions []bool
				switch node := node.(type) {
				case *ast.ReturnStmt:
					expressions, resultPositions = node.Results, errorResults
				case *ast.AssignStmt:
					expressions = node.Rhs
					for _, lhs := range node.Lhs {
						name, ok := lhs.(*ast.Ident)
						resultPositions = append(resultPositions, ok && namedErrors[name.Obj])
					}
				default:
					return true
				}
				if len(expressions) != len(resultPositions) {
					return true
				}
				for index, expression := range expressions {
					if !resultPositions[index] {
						continue
					}
					for {
						parenthesized, ok := expression.(*ast.ParenExpr)
						if !ok {
							break
						}
						expression = parenthesized.X
					}
					pkg, name := diagnosticConstructor(expression, imports)
					if local, ok := expression.(*ast.Ident); ok {
						pkg, name = packageOf(source), local.Name
						if local.Obj != nil && (local.Obj.Kind != ast.Con || local.Obj.Pos() != constants[pkg+"."+name]) {
							continue
						}
					}
					if _, exists := constants[pkg+"."+name]; exists {
						line := strconv.Itoa(positions.Position(expression.Pos()).Line)
						violations = append(violations, source.relative+":"+line+": return a source-owned diagnostic through the enum package's failure/WithCause owner; bare "+name+" in an error result loses its framework diagnostic")
					}
				}
				return true
			})
		}
		for _, declaration := range source.file.Decls {
			if function, ok := declaration.(*ast.FuncDecl); ok {
				inspectFunction(function.Type, function.Body)
			} else {
				ast.Inspect(declaration, func(node ast.Node) bool {
					if literal, ok := node.(*ast.FuncLit); ok {
						inspectFunction(literal.Type, literal.Body)
						return false
					}
					return true
				})
			}
		}
	}
	return violations
}

func TestEnumSourceReturnGuard(t *testing.T) {
	for _, test := range []struct {
		name, body string
		want       int
	}{
		{"same package", `func f() error { return Missing }`, 1},
		{"tuple error", `func f() (int,error) { return 0, (Missing) }`, 1},
		{"bare named result", `func f() (err error) { err = Missing; return }`, 1},
		{"initializer callback", `var callback = func() error { return Missing }`, 1},
		{"named error", `func f() (n int, err error) { return 0, Missing }`, 1},
		{"classifier", `func f() Code { return Missing }`, 0},
		{"tuple classifier", `func f() (Code,error) { return Missing, nil }`, 0},
		{"callback error", `func f() Code { _ = func() error { return Missing }; return Missing }`, 1},
		{"callback classifier", `func f() error { _ = func() Code { return Missing }; return nil }`, 0},
		{"local shadow", `func f() error { Missing := error(nil); return Missing }`, 0},
		{"local constant shadow", `func f() error { const Missing Code = "application"; return Missing }`, 0},
		{"owned source", `func f() error { return failure(Missing,nil) }`, 0},
		{"qualified source", `func f() error { return enums.Missing }`, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			positions := token.NewFileSet()
			declarations := `package enums; type Code string; const Missing Code = "missing"; func (Code) Error() string { return "code" }`
			owner, err := parser.ParseFile(positions, "enums/errors.go", declarations, 0)
			if err != nil {
				t.Fatal(err)
			}
			path, prefix := "enums/source.go", "package enums; "
			if strings.Contains(test.body, "enums.Missing") {
				path, prefix = "consumer/source.go", `package consumer; import enums "github.com/weiloon1234/Foundry-Go/enums"; `
			}
			file, err := parser.ParseFile(positions, path, prefix+test.body, 0)
			if err != nil {
				t.Fatal(err)
			}
			got := unownedEnumReturns(positions, []diagnosticSource{{"enums/errors.go", owner}, {path, file}})
			if len(got) != test.want {
				t.Fatalf("got %d violations, want %d: %v", len(got), test.want, got)
			}
		})
	}
}
