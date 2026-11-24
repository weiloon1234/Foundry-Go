package modelconvert

import (
	"go/ast"
	"go/types"
	"maps"
	"strings"

	"github.com/weiloon1234/Foundry-Go/internal/generate"
)

type behaviorBinding struct{ Field, Option, Name string }

// checkBehaviorBindings proves which named function each generated field selects,
// independently of runtime codec closures sharing an entry point.
func checkBehaviorBindings(pkg *generate.AnalysisPackage, plan *Plan) error {
	c := &converter{plan: &Plan{analysis: pkg}}
	reference := func(e ast.Expr) string {
		if id, ok := e.(*ast.Ident); ok && id.Name == "nil" {
			return ""
		}
		object, ok := c.function(e).(*types.Func)
		if !ok || object.Pkg() != pkg.Types {
			return "<unresolved>"
		}
		return object.Name()
	}
	declarations := map[string]ast.Expr{}
	for _, file := range pkg.Syntax {
		for _, decl := range file.Decls {
			if gen, ok := decl.(*ast.GenDecl); ok {
				for _, spec := range gen.Specs {
					if value, ok := spec.(*ast.ValueSpec); ok && len(value.Names) == 1 && len(value.Values) == 1 {
						declarations[value.Names[0].Name] = value.Values[0]
					}
				}
			}
		}
	}
	for _, model := range plan.Models {
		expected := map[behaviorBinding]int{}
		for _, behavior := range model.Behaviors {
			if behavior.Option != "" {
				expected[behaviorBinding{behavior.Field, behavior.Option, behavior.Name}]++
			}
		}
		actual := map[behaviorBinding]int{}
		declaration := declarations[model.Variable]
		if declaration == nil {
			return reject("missing generated declaration for " + model.Name)
		}
		ast.Inspect(declaration, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			name := c.ormFunction(call.Fun)
			var option string
			switch name {
			case "WithHooks":
				option = "hooks"
			case "WithGlobalScopes":
				option = "scopes"
			case "WithRetrievalHooks":
				option = "retrieval"
			}
			if option != "" && len(call.Args) == 1 {
				actual[behaviorBinding{"", option, reference(call.Args[0])}]++
				return false
			}
			if !strings.HasSuffix(name, "Of") || len(call.Args) < 2 {
				return true
			}
			fieldName := ""
			if name == "InputKeyOf" {
				// The generated TableOf is the single owner of the key field.
				ast.Inspect(declaration, func(n ast.Node) bool {
					key, ok := n.(*ast.CallExpr)
					if ok && c.ormFunction(key.Fun) == "TableOf" && len(key.Args) >= 2 {
						if pointer, ok := key.Args[1].(*ast.UnaryExpr); ok {
							if field, ok := pointer.X.(*ast.SelectorExpr); ok {
								fieldName = field.Sel.Name
							}
						}
					}
					return true
				})
			} else if pointer, ok := call.Args[1].(*ast.UnaryExpr); ok {
				if field, ok := pointer.X.(*ast.SelectorExpr); ok {
					fieldName = field.Sel.Name
				}
			}
			if fieldName == "" {
				return true
			}
			if strings.Contains(name, "Input") && len(call.Args) > 2 {
				if ref := reference(call.Args[2]); ref != "" {
					actual[behaviorBinding{fieldName, "mutate", ref}]++
				}
			}
			for _, argument := range call.Args[2:] {
				codec, ok := argument.(*ast.CallExpr)
				if !ok || c.ormFunction(codec.Fun) != "Codec" || len(codec.Args) != 1 {
					continue
				}
				factory, ok := codec.Args[0].(*ast.CallExpr)
				if !ok {
					actual[behaviorBinding{fieldName, "codec", "<unresolved>"}]++
					continue
				}
				actual[behaviorBinding{fieldName, "codec", reference(factory.Fun)}]++
			}
			return true
		})
		if !maps.Equal(expected, actual) {
			return reject("model " + model.Name + ": generated behavior bindings changed")
		}
	}
	return nil
}
