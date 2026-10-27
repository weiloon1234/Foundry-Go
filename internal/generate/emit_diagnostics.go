package generate

import (
	"bytes"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"strconv"
	"strings"
)

// Framework declarations share the handwritten runtime's diagnostic owner.
// Consumer output cannot import internal packages and remains application code.
func (e *emitter) frameworkDiagnostics(body []byte) ([]byte, error) {
	if e.pkg.path != framework && !strings.HasPrefix(e.pkg.path, framework+"/") {
		return body, nil
	}
	faultAlias, fmtAlias := e.imports[framework+"/fault"], e.imports["fmt"]
	if faultAlias == "" && fmtAlias == "" {
		return body, nil
	}
	files := token.NewFileSet()
	file, err := parser.ParseFile(files, "generated.go", append([]byte("package generated\n"), body...), parser.ParseComments)
	if err != nil {
		return nil, err
	}
	changed := false
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		pkg, ok := selector.X.(*ast.Ident)
		if !ok {
			return true
		}
		method := ""
		if pkg.Name == faultAlias && faultAlias != "" {
			switch selector.Sel.Name {
			case "New":
				method = "NewFault"
			case "Wrap":
				method = "WrapFault"
			}
		}
		if pkg.Name == fmtAlias && fmtAlias != "" && selector.Sel.Name == "Errorf" && len(call.Args) == 2 {
			if literal, ok := call.Args[0].(*ast.BasicLit); ok && literal.Kind == token.STRING {
				text, _ := strconv.Unquote(literal.Value)
				if strings.HasSuffix(text, ": %w") && strings.Count(text, "%") == 1 {
					method = "Prefix"
					literal.Value = strconv.Quote(strings.TrimSuffix(text, ": %w"))
				}
			}
		}
		if method != "" {
			selector.X = ast.NewIdent(e.useNamed(framework+"/internal/safeerror", "foundrydiagnostic"))
			selector.Sel = ast.NewIdent(method)
			changed = true
		}
		return true
	})
	if !changed {
		return body, nil
	}
	fmtUsed := false
	ast.Inspect(file, func(node ast.Node) bool {
		if selector, ok := node.(*ast.SelectorExpr); ok {
			if pkg, ok := selector.X.(*ast.Ident); ok && pkg.Name == fmtAlias {
				fmtUsed = true
			}
		}
		return true
	})
	if !fmtUsed {
		delete(e.imports, "fmt")
	}
	var output bytes.Buffer
	if err := format.Node(&output, files, file); err != nil {
		return nil, err
	}
	return bytes.TrimPrefix(output.Bytes(), []byte("package generated\n")), nil
}
