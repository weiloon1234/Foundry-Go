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
	if len(args) != 0 && (len(args) != 1 || args["labels"] == "") {
		return e, p.diagnostic(spec.Pos(), "enum directive only accepts labels=message.prefix")
	}
	e.labels = args["labels"]
	if err := validateEnumLabels(e.labels); err != nil {
		return e, p.diagnostic(spec.Pos(), err.Error())
	}
	base, ok := named.Underlying().(*types.Basic)
	if !ok || base.Kind() == types.Uintptr || (base.Info()&types.IsInteger == 0 && base.Kind() != types.String) {
		return e, p.diagnostic(spec.Pos(), "enum must have a string or integer underlying type")
	}
	e.base = base
	constants := p.enumConstants()
	seen := make(map[string]string)
	for _, name := range p.types.Scope().Names() {
		v, ok := p.types.Scope().Lookup(name).(*types.Const)
		if !ok || !types.Identical(v.Type(), named) {
			continue
		}
		// Cases are exported constants declared as values. Aliases of another
		// case and explicitly ignored constants keep their Go meaning without
		// becoming wire values. An unexported value was a case before, so it is
		// never dropped silently: export it or mark it ignored.
		declaration := constants[name]
		if declaration.ignored || p.enumAlias(declaration.value, named) {
			continue
		}
		if !v.Exported() {
			return e, p.diagnostic(v.Pos(), fmt.Sprintf("unexported constant %s of enum %s declares its own value; export it to keep it as a case, or mark it //%s to exclude it from the enum's wire values", name, e.name, enumIgnoreDirective))
		}
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
func (p *packageInput) enumAlias(value ast.Expr, named *types.Named) bool {
	for {
		switch expression := value.(type) {
		case *ast.ParenExpr:
			value = expression.X
			continue
		case *ast.CallExpr:
			if len(expression.Args) != 1 {
				return false
			}
			if conversion, ok := p.types.Scope().Lookup(identifierName(expression.Fun)).(*types.TypeName); !ok || !types.Identical(conversion.Type(), named) {
				return false
			}
			value = expression.Args[0]
			continue
		case *ast.Ident:
			target, ok := p.types.Scope().Lookup(expression.Name).(*types.Const)
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

// enumConstants resolves each package-level constant's declared expression,
// following Go's implicit repetition of the previous expression list.
func (p *packageInput) enumConstants() map[string]enumConstant {
	result := make(map[string]enumConstant)
	for _, file := range p.files {
		for _, decl := range file.syntax.Decls {
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
