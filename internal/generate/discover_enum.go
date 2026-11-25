package generate

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// enumIgnoreDirective excludes a typed constant, or every constant in a
// documented const group, from the cases of its enum type.
const enumIgnoreDirective = "foundry:ignore"

// enumConstant is the syntax of one package-level constant, including the
// expression it inherits through implicit repetition in a const group.
type enumConstant struct {
	value   ast.Expr
	ignored bool
}

func discoverEnum(p *packageInput, spec *ast.TypeSpec, named *types.Named, args map[string]string) (enum, error) {
	e := enum{name: spec.Name.Name, typ: named, position: p.fset.Position(spec.Pos())}
	trim, trimmed := args["trim_type"]
	if len(args) != 0 && (args["labels"] == "" || len(args) != 1 && (len(args) != 2 || !trimmed)) || trimmed && trim != "true" {
		return e, p.diagnostic(spec.Pos(), "enum directive only accepts labels=message.prefix and trim_type=true")
	}
	e.labels, e.trimType = args["labels"], trimmed
	if err := validateEnumLabels(e.labels); err != nil {
		return e, p.diagnostic(spec.Pos(), err.Error())
	}
	base, ok := named.Underlying().(*types.Basic)
	if !ok || base.Kind() == types.Uintptr || (base.Info()&types.IsInteger == 0 && base.Kind() != types.String) {
		return e, p.diagnostic(spec.Pos(), "enum must have a string or integer underlying type")
	}
	e.base = base
	files := make([]*ast.File, len(p.files))
	for i, file := range p.files {
		files[i] = file.syntax
	}
	cases, unexported := enumCaseConstants(p.types.Scope(), enumConstants(files), named)
	if unexported != nil {
		return e, p.diagnostic(unexported.Pos(), fmt.Sprintf("unexported constant %s of enum %s declares its own value; export it to keep it as a case, or mark it //%s to exclude it from the enum's wire values", unexported.Name(), e.name, enumIgnoreDirective))
	}
	seen := make(map[string]string)
	for _, v := range cases {
		name := v.Name()
		literal := v.Val().ExactString()
		if v.Val().Kind() == constant.String {
			if !utf8.ValidString(constant.StringVal(v.Val())) {
				return e, p.diagnostic(v.Pos(), "enum strings must be valid UTF-8")
			}
			literal = strconv.Quote(constant.StringVal(v.Val()))
		}
		if previous, exists := seen[literal]; exists {
			return e, p.diagnostic(v.Pos(), fmt.Sprintf("enum contains duplicate serialized values: %s and %s both encode %s; declare an alias as `%s = %s` or mark a non-case constant with //%s", previous, name, literal, name, previous, enumIgnoreDirective))
		}
		seen[literal] = name
		e.values = append(e.values, enumValue{name, literal, p.fset.Position(v.Pos())})
	}
	if len(e.values) == 0 {
		return e, p.diagnostic(spec.Pos(), "enum requires exported typed constant values")
	}
	if err := validateEnumCaseLabels(e); err != nil {
		return e, p.diagnostic(spec.Pos(), err.Error())
	}
	sort.Slice(e.values, func(i, j int) bool {
		a, b := e.values[i].position, e.values[j].position
		if a.Filename != b.Filename {
			return a.Filename < b.Filename
		}
		return a.Offset < b.Offset
	})
	return e, nil
}

// enumAlias reports a constant whose declared value is another constant of the
// same enum type, directly or through an explicit conversion to that type.
func enumAlias(scope *types.Scope, value ast.Expr, named *types.Named) bool {
	for {
		switch expression := value.(type) {
		case *ast.ParenExpr:
			value = expression.X
			continue
		case *ast.CallExpr:
			if len(expression.Args) != 1 {
				return false
			}
			if conversion, ok := scope.Lookup(identifierName(expression.Fun)).(*types.TypeName); !ok || !types.Identical(conversion.Type(), named) {
				return false
			}
			value = expression.Args[0]
			continue
		case *ast.Ident:
			target, ok := scope.Lookup(expression.Name).(*types.Const)
			return ok && types.Identical(target.Type(), named)
		}
		return false
	}
}

func identifierName(expression ast.Expr) string {
	for {
		switch current := expression.(type) {
		case *ast.ParenExpr:
			expression = current.X
		case *ast.Ident:
			return current.Name
		default:
			return ""
		}
	}
}

// enumCaseConstants returns the constants that are cases of named, in scope
// order. Cases are exported constants declared as values. Aliases of another
// case and explicitly ignored constants keep their Go meaning without becoming
// wire values. An unexported value was a case before, so it is never dropped
// silently: the first one is returned separately, to export or mark ignored.
func enumCaseConstants(scope *types.Scope, declarations map[string]enumConstant, named *types.Named) ([]*types.Const, *types.Const) {
	var cases []*types.Const
	for _, name := range scope.Names() {
		v, ok := scope.Lookup(name).(*types.Const)
		if !ok || !types.Identical(v.Type(), named) {
			continue
		}
		declaration := declarations[name]
		if declaration.ignored || enumAlias(scope, declaration.value, named) {
			continue
		}
		if !v.Exported() {
			return nil, v
		}
		cases = append(cases, v)
	}
	return cases, nil
}

// enumConstants resolves each package-level constant's declared expression,
// following Go's implicit repetition of the previous expression list.
func enumConstants(files []*ast.File) map[string]enumConstant {
	result := make(map[string]enumConstant)
	for _, file := range files {
		for _, decl := range file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.CONST {
				continue
			}
			groupIgnored := hasCommentDirective(gen.Doc, enumIgnoreDirective)
			var previous []ast.Expr
			for _, spec := range gen.Specs {
				value := spec.(*ast.ValueSpec)
				if len(value.Values) != 0 {
					previous = value.Values
				}
				ignored := groupIgnored || hasCommentDirective(value.Doc, enumIgnoreDirective) || hasCommentDirective(value.Comment, enumIgnoreDirective)
				for i, name := range value.Names {
					declaration := enumConstant{ignored: ignored}
					if i < len(previous) {
						declaration.value = previous[i]
					}
					result[name.Name] = declaration
				}
			}
		}
	}
	return result
}

func hasCommentDirective(group *ast.CommentGroup, directive string) bool {
	if group == nil {
		return false
	}
	for _, comment := range group.List {
		if strings.TrimSpace(strings.TrimPrefix(comment.Text, "//")) == directive {
			return true
		}
	}
	return false
}
