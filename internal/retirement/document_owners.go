package retirement

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path"
	"slices"
	"strconv"
	"strings"

	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

type documentRules struct {
	Owners         []string          `json:"owners"`
	Constructors   map[string]string `json:"constructors"`
	RetainedOwners []string          `json:"retainedOwners"`
	CallArguments  map[string]int    `json:"callArguments"`
	Chains         []struct {
		Owner       string   `json:"owner"`
		Methods     []string `json:"methods"`
		Replacement string   `json:"replacement"`
	} `json:"chains"`
	denied       map[string]map[string]bool
	declarations map[string][]string
}

func loadDocumentRules(declarations map[string][]string) (documentRules, error) {
	var rules documentRules
	data, err := ledger.ReadFile("document_rules.json")
	if err != nil {
		return rules, err
	}
	if err := json.Unmarshal(data, &rules); err != nil {
		return rules, err
	}
	rules.declarations = declarations
	rules.denied = make(map[string]map[string]bool)
	for pkg, members := range declarations {
		for _, member := range members {
			owner, method, ok := strings.Cut(member, ".")
			owner = path.Base(pkg) + "." + owner
			if !ok || !slices.Contains(rules.Owners, owner) {
				continue
			}
			if rules.denied[method] == nil {
				rules.denied[method] = make(map[string]bool)
			}
			rules.denied[method][owner] = true
		}
	}
	return rules, nil
}

func (r documentRules) owns(pkg, member string) bool {
	owner, _, ok := strings.Cut(member, ".")
	return ok && slices.Contains(r.Owners, path.Base(pkg)+"."+owner)
}

// documentCode parses fragments without loading or type-checking an application.
// Parse errors leave unknown receivers ambiguous, never evidence of retirement.
func (r documentRules) checkCode(name, code string, line int, application bool) []error {
	if strings.HasPrefix(strings.TrimSpace(code), ".") {
		code = "unknown" + strings.TrimSpace(code)
	}
	fset := token.NewFileSet()
	prefix := "package documentation\nfunc example() {\n"
	suffix := "\n}"
	if strings.HasPrefix(strings.TrimSpace(code), "package ") {
		prefix, suffix = "", ""
	}
	if strings.HasPrefix(strings.TrimSpace(code), "import ") {
		prefix, suffix = "package documentation\n", ""
	}
	file, _ := parser.ParseFile(fset, "", prefix+code+suffix, parser.AllErrors)
	if file == nil {
		return nil
	}
	calls := make(map[*ast.SelectorExpr]*ast.CallExpr)
	ast.Inspect(file, func(node ast.Node) bool {
		if call, ok := node.(*ast.CallExpr); ok {
			if selected, ok := stripTypeArguments(call.Fun).(*ast.SelectorExpr); ok {
				calls[selected] = call
			}
		}
		return true
	})
	aliases := map[string]string{"foundryhttp": "http"}
	importedRetirements := make(map[string]bool)
	dotRetirements := make(map[string]bool)
	for _, spec := range file.Imports {
		imported, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			continue
		}
		alias := path.Base(imported)
		if spec.Name != nil {
			alias = spec.Name.Name
		}
		if strings.HasPrefix(imported, framework) {
			owner := strings.TrimPrefix(imported, framework)
			switch owner {
			case "http", "http/modelbinding", "http/pagination", "database/orm", "database/orm/rows", "validation", "translations", "password":
				aliases[alias] = path.Base(owner)
			default:
				aliases[alias] = "framework:" + owner
			}
			if application {
				for _, member := range r.declarations[owner] {
					if alias == "." {
						dotRetirements[member] = true
					} else if !r.owns(owner, member) {
						importedRetirements[aliases[alias]+"."+member] = true
					}
				}
			}
		} else {
			aliases[alias] = "external:" + imported
		}
	}
	var failures []error
	report := func(pos token.Pos, message string) {
		n := line + fset.Position(pos).Line - strings.Count(prefix, "\n") - 1
		failures = append(failures, safeerror.New(fmt.Sprintf("%s:%d: %s", name, n, message)))
	}
	ast.Inspect(file, func(node ast.Node) bool {
		if call, ok := node.(*ast.CallExpr); ok {
			if id, ok := stripTypeArguments(call.Fun).(*ast.Ident); ok && id.Obj == nil && dotRetirements[id.Name] {
				report(id.Pos(), "retired API "+id.Name+" from framework dot import")
			}
		}
		selected, ok := node.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if len(importedRetirements) != 0 {
			bindings := r.bindingsAt(file, selected.Pos(), aliases)
			if spelling := qualifiedName(selected, bindings, aliases); importedRetirements[spelling] {
				report(selected.Sel.Pos(), "retired API "+spelling)
			}
		}
		denied := r.denied[selected.Sel.Name]
		var chainOwner, replacement string
		receiver := selected.X
		for _, chain := range r.Chains {
			if len(chain.Methods) != 2 || selected.Sel.Name != chain.Methods[1] {
				continue
			}
			call, ok := selected.X.(*ast.CallExpr)
			if !ok {
				continue
			}
			var method string
			var base ast.Expr
			switch inner := call.Fun.(type) {
			case *ast.SelectorExpr:
				method, base = inner.Sel.Name, inner.X
			case *ast.Ident:
				method = inner.Name
			}
			if method == chain.Methods[0] && r.matchesCallArguments(method, call) {
				chainOwner, replacement = chain.Owner, chain.Replacement
				receiver = base
			}
		}
		if len(denied) == 0 && chainOwner == "" {
			return true
		}
		bindings := r.bindingsAt(file, selected.Pos(), aliases)
		owner := r.owner(receiver, bindings, aliases)
		if denied[owner] || chainOwner != "" && owner == chainOwner {
			message := "retired API " + owner + "." + selected.Sel.Name
			if replacement != "" {
				message += "; " + replacement
			}
			report(selected.Sel.Pos(), message)
		} else if owner == "" && r.matchesCallArguments(selected.Sel.Name, calls[selected]) {
			report(selected.Sel.Pos(), "ambiguous API teaching for ."+selected.Sel.Name+"; qualify the receiver's owner or replace the example with a Foundry guides: reference")
		}
		return true
	})
	return failures
}

// Only explicit single-value arguments can rule out a fixed-arity method.
// A sole call may return multiple values; spread arguments need owner evidence.
func (r documentRules) matchesCallArguments(method string, call *ast.CallExpr) bool {
	count, fixed := r.CallArguments[method]
	if !fixed || call == nil || call.Ellipsis.IsValid() {
		return true
	}
	if len(call.Args) == 1 {
		if _, expands := ast.Unparen(call.Args[0]).(*ast.CallExpr); expands {
			return true
		}
	}
	return len(call.Args) == count
}

func stripTypeArguments(expr ast.Expr) ast.Expr {
	switch e := ast.Unparen(expr).(type) {
	case *ast.IndexExpr:
		return stripTypeArguments(e.X)
	case *ast.IndexListExpr:
		return stripTypeArguments(e.X)
	case *ast.StarExpr:
		return stripTypeArguments(e.X)
	default:
		return e
	}
}

func qualifiedName(expr ast.Expr, bindings, aliases map[string]string) string {
	switch e := stripTypeArguments(expr).(type) {
	case *ast.Ident:
		if _, bound := bindings[e.Name]; bound {
			return ""
		}
		if alias, ok := aliases[e.Name]; ok {
			return alias
		}
		return e.Name
	case *ast.SelectorExpr:
		if parent := qualifiedName(e.X, bindings, aliases); parent != "" {
			return parent + "." + e.Sel.Name
		}
	}
	return ""
}

func (r documentRules) owner(expr ast.Expr, bindings, aliases map[string]string) string {
	switch e := stripTypeArguments(expr).(type) {
	case *ast.Ident:
		if owner, exists := bindings[e.Name]; exists {
			return owner
		}
		if slices.Contains(r.RetainedOwners, e.Name) {
			return e.Name
		}
		if ast.IsExported(e.Name) {
			shared := slices.ContainsFunc(r.Owners, func(owner string) bool { return strings.HasSuffix(owner, "."+e.Name) })
			if !shared {
				return e.Name
			}
		}
	case *ast.CompositeLit:
		return r.owner(e.Type, bindings, aliases)
	case *ast.UnaryExpr:
		return r.owner(e.X, bindings, aliases)
	case *ast.CallExpr:
		if owner := r.Constructors[qualifiedName(e.Fun, bindings, aliases)]; owner != "" {
			return owner
		}
		if method, ok := e.Fun.(*ast.SelectorExpr); ok && method.Sel.Name == "Query" && r.owner(method.X, bindings, aliases) == "orm.Table" {
			return "orm.Query"
		}
		// Only the scope's known fluent operation preserves an inferred owner.
		if method, ok := e.Fun.(*ast.SelectorExpr); ok && (method.Sel.Name == "WithMiddleware" || method.Sel.Name == "Within") {
			if owner := r.owner(method.X, bindings, aliases); owner == "http.Scope" {
				return owner
			}
		}
	case *ast.SelectorExpr:
		name := qualifiedName(e, bindings, aliases)
		if slices.Contains(r.Owners, name) || slices.Contains(r.RetainedOwners, name) || name == "translations.Text" {
			return name
		}
		// An imported or conventionally named package is a qualified owner, not
		// an unknown instance. This permits validation.Check and rows.ForUpdate.
		if root, ok := e.X.(*ast.Ident); ok {
			if _, bound := bindings[root.Name]; !bound {
				if _, imported := aliases[root.Name]; imported {
					return name
				}
			}
		}
	}
	if id, ok := expr.(*ast.Ident); ok {
		if _, bound := bindings[id.Name]; bound {
			return ""
		}
		if alias, imported := aliases[id.Name]; imported {
			return alias
		}
		switch id.Name {
		case "http", "modelbinding", "pagination", "validation", "orm", "rows", "translations":
			return id.Name
		}
	}
	return ""
}

// Only declarations visible at the use and earlier linear assignments provide
// evidence. Assignments in an earlier nested block invalidate outer evidence.
func (r documentRules) bindingsAt(file *ast.File, pos token.Pos, aliases map[string]string) map[string]string {
	bindings := make(map[string]string)
	ast.Inspect(file, func(node ast.Node) bool {
		if node == nil || node.Pos() >= pos {
			return false
		}
		switch n := node.(type) {
		case *ast.TypeSpec, *ast.CompositeLit:
			return false
		case *ast.FuncDecl:
			if n.End() < pos {
				return false
			}
		case *ast.FuncLit:
			if n.End() < pos {
				return false
			}
		case *ast.BlockStmt:
			if n.End() < pos {
				ast.Inspect(n, func(child ast.Node) bool {
					if assignment, ok := child.(*ast.AssignStmt); ok && assignment.Tok == token.ASSIGN {
						for _, lhs := range assignment.Lhs {
							if id, ok := lhs.(*ast.Ident); ok {
								bindings[id.Name] = ""
							}
						}
					}
					return true
				})
				return false
			}
		case *ast.Field:
			owner := r.owner(n.Type, bindings, aliases)
			for _, id := range n.Names {
				bindings[id.Name] = owner
			}
		case *ast.ValueSpec:
			owner := r.owner(n.Type, bindings, aliases)
			for i, id := range n.Names {
				value := owner
				if n.Type == nil && i < len(n.Values) {
					value = r.owner(n.Values[i], bindings, aliases)
				}
				bindings[id.Name] = value
			}
		case *ast.AssignStmt:
			if n.End() >= pos {
				return false
			}
			values := make([]string, len(n.Lhs))
			for i := range values {
				if i < len(n.Rhs) {
					values[i] = r.owner(n.Rhs[i], bindings, aliases)
				}
			}
			for i, lhs := range n.Lhs {
				if id, ok := lhs.(*ast.Ident); ok {
					bindings[id.Name] = values[i]
				}
			}
			return false
		}
		return true
	})
	return bindings
}
