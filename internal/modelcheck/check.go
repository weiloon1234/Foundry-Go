package modelcheck

import (
	"context"
	"go/ast"
	"go/token"
	"go/types"
	"strings"

	"github.com/weiloon1234/Foundry-Go/internal/frameworkinfo"
)

const directiveReplacement = "declare the table with the //foundry:model directive on the struct, then run foundry generate"

type defineCall struct {
	call  *ast.CallExpr
	model types.Type
	table types.Type
}

type structFinding struct {
	pos   token.Pos
	name  string
	model types.Type
}

// Analysis retains one package's type information and function-alias definitions.
type Analysis struct {
	Package        *types.Package
	Context        context.Context
	Info           *types.Info
	Files          *token.FileSet
	aliases        map[types.Object][]ast.Expr
	manifestCache  *manifestCache
	nameParameters map[declarationFunction][]int
}

func NewAnalysis(info *types.Info, files *token.FileSet) *Analysis {
	return &Analysis{
		Context:       context.Background(),
		Info:          info,
		Files:         files,
		aliases:       make(map[types.Object][]ast.Expr),
		manifestCache: newManifestCache(),
	}
}

func (a *Analysis) Index(file *ast.File) {
	if a.Context != nil && a.Context.Err() != nil {
		return
	}
	ast.Inspect(file, func(n ast.Node) bool {
		if a.Context != nil && a.Context.Err() != nil {
			return false
		}
		switch n := n.(type) {
		case *ast.ValueSpec:
			if len(n.Names) == len(n.Values) {
				for i, id := range n.Names {
					if obj := a.Info.ObjectOf(id); obj != nil {
						a.aliases[obj] = append(a.aliases[obj], n.Values[i])
					}
				}
			}
		case *ast.AssignStmt:
			if len(n.Lhs) == len(n.Rhs) {
				for i, left := range n.Lhs {
					if id, ok := left.(*ast.Ident); ok {
						if obj := a.Info.ObjectOf(id); obj != nil {
							a.aliases[obj] = append(a.aliases[obj], n.Rhs[i])
						}
					}
				}
			}
		}
		return true
	})
}

// Function resolves direct references, import aliases, dot imports, and
// function values whose assignments all select the same function.
func (a *Analysis) Function(expr ast.Expr) *types.Func {
	return a.function(expr, make(map[types.Object]bool))
}

func (a *Analysis) function(expr ast.Expr, seen map[types.Object]bool) *types.Func {
	if a.Context != nil && a.Context.Err() != nil {
		return nil
	}
	switch e := expr.(type) {
	case *ast.CallExpr:
		if len(e.Args) == 1 && a.Info.Types[e.Fun].IsType() {
			if typ := a.Info.TypeOf(e); typ != nil {
				if _, ok := typ.Underlying().(*types.Signature); ok {
					return a.function(e.Args[0], seen)
				}
			}
		}
	case *ast.ParenExpr:
		return a.function(e.X, seen)
	case *ast.IndexExpr:
		return a.function(e.X, seen)
	case *ast.IndexListExpr:
		return a.function(e.X, seen)
	case *ast.SelectorExpr:
		fn, _ := a.Info.ObjectOf(e.Sel).(*types.Func)
		return fn
	case *ast.Ident:
		object := a.Info.ObjectOf(e)
		if fn, ok := object.(*types.Func); ok {
			return fn
		}
		if object == nil || seen[object] {
			return nil
		}
		seen[object] = true
		defer delete(seen, object)
		var fn *types.Func
		for _, value := range a.aliases[object] {
			next := a.function(value, seen)
			if next == nil || (fn != nil && fn != next) {
				return nil
			}
			fn = next
		}
		return fn
	}
	return nil
}

func isDeclarationFunction(fn *types.Func) bool {
	if fn == nil || fn.Pkg() == nil {
		return false
	}
	if fn.Pkg().Path() == frameworkinfo.ModulePath+"/database/orm" {
		return fn.Name() == "Define"
	}
	return false
}

func (a *Analysis) extractDefineTypes(call *ast.CallExpr) (types.Type, types.Type) {
	if len(call.Args) >= 2 {
		declareArg := unparen(call.Args[1])
		declareType := a.Info.TypeOf(declareArg)
		if declareType != nil {
			if sig, ok := types.Unalias(declareType).(*types.Signature); ok {
				var modelType types.Type
				if sig.Params().Len() >= 2 {
					p1 := types.Unalias(sig.Params().At(1).Type())
					if ptr, ok := p1.(*types.Pointer); ok {
						modelType = types.Unalias(ptr.Elem())
					}
				}
				if modelType == nil && sig.Params().Len() >= 1 {
					p0 := types.Unalias(sig.Params().At(0).Type())
					if named, ok := p0.(*types.Named); ok && named.TypeArgs().Len() >= 1 {
						modelType = types.Unalias(named.TypeArgs().At(0))
					}
				}
				var tableType types.Type
				if sig.Results().Len() >= 1 {
					tableType = types.Unalias(sig.Results().At(0).Type())
				}
				if modelType != nil || tableType != nil {
					return modelType, tableType
				}
			}
		}
	}

	ident := extractIdent(call.Fun)
	if ident != nil {
		if inst, ok := a.Info.Instances[ident]; ok && inst.TypeArgs.Len() >= 1 {
			var modelType, tableType types.Type
			modelType = inst.TypeArgs.At(0)
			if inst.TypeArgs.Len() >= 2 {
				tableType = inst.TypeArgs.At(1)
			}
			return modelType, tableType
		}
	}

	tType := a.Info.TypeOf(call)
	return nil, tType
}

func unparen(expr ast.Expr) ast.Expr {
	for {
		p, ok := expr.(*ast.ParenExpr)
		if !ok {
			return expr
		}
		expr = p.X
	}
}

func extractIdent(expr ast.Expr) *ast.Ident {
	expr = unparen(expr)
	switch e := expr.(type) {
	case *ast.Ident:
		return e
	case *ast.SelectorExpr:
		return e.Sel
	case *ast.IndexExpr:
		return extractIdent(e.X)
	case *ast.IndexListExpr:
		return extractIdent(e.X)
	}
	return nil
}

func isORMTable(t types.Type) (*types.Named, bool) {
	if t == nil {
		return nil, false
	}
	t = types.Unalias(t)
	if ptr, ok := t.(*types.Pointer); ok {
		t = types.Unalias(ptr.Elem())
	}
	named, ok := t.(*types.Named)
	if !ok {
		return nil, false
	}
	orig := named.Origin()
	if orig.Obj() != nil && orig.Obj().Pkg() != nil &&
		orig.Obj().Pkg().Path() == frameworkinfo.ModulePath+"/database/orm" &&
		orig.Obj().Name() == "Table" {
		return named, true
	}
	return nil, false
}

func targetString(modelType types.Type, fallbackTypeName string) string {
	if modelType != nil {
		modelType = types.Unalias(modelType)
		if ptr, ok := modelType.(*types.Pointer); ok {
			modelType = types.Unalias(ptr.Elem())
		}
		if named, ok := modelType.(*types.Named); ok {
			if _, isTypeParam := named.Underlying().(*types.TypeParam); !isTypeParam {
				if named.Obj() != nil && named.Obj().Name() != "" {
					return "model " + named.Obj().Name()
				}
			}
		}
	}
	if fallbackTypeName != "" {
		return "type " + fallbackTypeName
	}
	return "table declaration"
}

func isTestFile(filePath string) bool {
	return strings.HasSuffix(filePath, "_test.go")
}

func isTypeParameterized(t types.Type) bool {
	if t == nil {
		return false
	}
	t = types.Unalias(t)
	if ptr, ok := t.(*types.Pointer); ok {
		t = types.Unalias(ptr.Elem())
	}
	switch typ := t.(type) {
	case *types.TypeParam:
		return true
	case *types.Named:
		if typ.TypeParams() != nil && typ.TypeParams().Len() > 0 {
			return true
		}
		if orig := typ.Origin(); orig != nil && orig != typ {
			if orig.TypeParams() != nil && orig.TypeParams().Len() > 0 {
				return true
			}
		}
		if typeArgs := typ.TypeArgs(); typeArgs != nil {
			for i := 0; i < typeArgs.Len(); i++ {
				if isTypeParameterized(typeArgs.At(i)) {
					return true
				}
			}
		}
	case *types.Struct:
		for i := 0; i < typ.NumFields(); i++ {
			if isTypeParameterized(typ.Field(i).Type()) {
				return true
			}
		}
	case *types.Slice:
		return isTypeParameterized(typ.Elem())
	case *types.Array:
		return isTypeParameterized(typ.Elem())
	case *types.Map:
		return isTypeParameterized(typ.Key()) || isTypeParameterized(typ.Elem())
	}
	return false
}

func inPackageTree(pkg *types.Package, rootPath string) bool {
	if pkg == nil || rootPath == "" {
		return false
	}
	p := pkg.Path()
	return p == rootPath || strings.HasPrefix(p, rootPath+"/")
}

func (a *Analysis) filePackage(file *ast.File) *types.Package {
	if a.Package != nil {
		return a.Package
	}
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			if obj := a.Info.Defs[d.Name]; obj != nil && obj.Pkg() != nil {
				return obj.Pkg()
			}
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				switch s := spec.(type) {
				case *ast.TypeSpec:
					if obj := a.Info.Defs[s.Name]; obj != nil && obj.Pkg() != nil {
						return obj.Pkg()
					}
				case *ast.ValueSpec:
					for _, name := range s.Names {
						if obj := a.Info.Defs[name]; obj != nil && obj.Pkg() != nil {
							return obj.Pkg()
						}
					}
				}
			}
		}
	}
	return nil
}

// CheckPackage checks all syntax files of one package for low-level table declarations.
func (a *Analysis) CheckPackage(syntax []*ast.File) []Diagnostic {
	var result []Diagnostic
	var defineCalls []defineCall
	nameParameters := a.nameParameters
	if nameParameters == nil {
		nameParameters = a.declarationNameParameters(syntax)
	}
	declaredTableTypes := make(map[types.Object]bool)
	declaredModels := make(map[types.Object]bool)

	// Pass 1: Find calls declaring tables through orm.Define
	for _, file := range syntax {
		if a.Context != nil && a.Context.Err() != nil {
			return nil
		}
		filePath := a.Files.Position(file.Pos()).Filename
		if isTestFile(filePath) || a.isGeneratedOwned(filePath, file) {
			continue
		}
		currPkg := a.filePackage(file)
		ast.Inspect(file, func(n ast.Node) bool {
			if a.Context != nil && a.Context.Err() != nil {
				return false
			}
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			fn := a.Function(call.Fun)
			if inPackageTree(currPkg, frameworkinfo.ModulePath+"/database/orm") {
				return true
			}
			var modelType, tableType types.Type
			constantName := true
			if isDeclarationFunction(fn) {
				modelType, tableType = a.extractDefineTypes(call)
				constantName = len(call.Args) > 0 && a.Info.Types[call.Args[0]].Value != nil
			} else {
				if fn == nil {
					return true
				}
				parameters := nameParameters[declarationFunctionOf(fn)]
				if len(parameters) == 0 {
					return true
				}

				tableType = a.Info.TypeOf(call)
				table := embeddedTable(tableType, make(map[types.Type]bool))
				if table == nil {
					return true
				}
				modelType = table.TypeArgs().At(0)
				offset := 0
				if selector, ok := unparen(call.Fun).(*ast.SelectorExpr); ok {
					if selection := a.Info.Selections[selector]; selection != nil && selection.Kind() == types.MethodExpr {
						offset = 1
					}
				}
				constantName = false
				for _, parameter := range parameters {
					i := parameter + offset
					if i < len(call.Args) && a.Info.Types[call.Args[i]].Value != nil {
						constantName = true
					}
				}
			}
			if isTypeParameterized(modelType) || (modelType == nil && isTypeParameterized(tableType)) {
				return true
			}
			if !constantName {
				// The concrete table type belongs to a dynamic declaration too.
				if named, ok := types.Unalias(tableType).(*types.Named); ok {
					declaredTableTypes[named.Obj()] = true
				}
				return true
			}
			defineCalls = append(defineCalls, defineCall{call: call, model: modelType, table: tableType})
			if tableType != nil {
				if named, ok := types.Unalias(tableType).(*types.Named); ok && named.Obj() != nil {
					declaredTableTypes[named.Obj()] = true
				}
			}
			if modelType != nil {
				if named, ok := types.Unalias(modelType).(*types.Named); ok && named.Obj() != nil {
					declaredModels[named.Obj()] = true
				}
			}
			return true
		})
	}

	// Pass 2: Find handwritten struct types embedding orm.Table
	var structFindings []structFinding
	for _, file := range syntax {
		if a.Context != nil && a.Context.Err() != nil {
			return nil
		}
		filePath := a.Files.Position(file.Pos()).Filename
		if isTestFile(filePath) || a.isGeneratedOwned(filePath, file) {
			continue
		}
		currPkg := a.filePackage(file)
		ast.Inspect(file, func(n ast.Node) bool {
			if a.Context != nil && a.Context.Err() != nil {
				return false
			}
			spec, ok := n.(*ast.TypeSpec)
			if !ok {
				return true
			}
			st, ok := spec.Type.(*ast.StructType)
			if !ok || st.Fields == nil {
				return true
			}
			obj := a.Info.Defs[spec.Name]
			if obj != nil && declaredTableTypes[obj] {
				return true
			}
			for _, field := range st.Fields.List {
				if len(field.Names) != 0 {
					continue
				}
				fieldType := a.Info.TypeOf(field.Type)
				tableNamed, ok := isORMTable(fieldType)
				if !ok {
					continue
				}
				tablePkg := tableNamed.Origin().Obj().Pkg()
				if inPackageTree(currPkg, tablePkg.Path()) {
					continue
				}
				var modelType types.Type
				if tableNamed.TypeArgs().Len() >= 1 {
					modelType = tableNamed.TypeArgs().At(0)
				}
				if isTypeParameterized(modelType) || (spec.TypeParams != nil && spec.TypeParams.NumFields() > 0) {
					continue
				}
				if modelType != nil {
					if named, ok := types.Unalias(modelType).(*types.Named); ok && named.Obj() != nil {
						if declaredModels[named.Obj()] {
							continue
						}
					}
				}
				structFindings = append(structFindings, structFinding{
					pos:   spec.Pos(),
					name:  spec.Name.Name,
					model: modelType,
				})
				break
			}
			return true
		})
	}

	// Emit findings for calls
	for _, dc := range defineCalls {
		pos := a.Files.Position(dc.call.Pos())
		target := targetString(dc.model, "")
		if target == "table declaration" && dc.table != nil {
			if named, ok := types.Unalias(dc.table).(*types.Named); ok && named.Obj() != nil {
				target = "type " + named.Obj().Name()
			}
		}
		result = append(result, Diagnostic{
			Position: pos,
			Target:   target,
			Message:  directiveReplacement,
		})
	}

	// Emit findings for standalone table structs
	for _, sf := range structFindings {
		pos := a.Files.Position(sf.pos)
		target := targetString(sf.model, sf.name)
		result = append(result, Diagnostic{
			Position: pos,
			Target:   target,
			Message:  directiveReplacement,
		})
	}

	return result
}

func embeddedTable(t types.Type, seen map[types.Type]bool) *types.Named {
	if t == nil || seen[t] {
		return nil
	}
	seen[t] = true
	if table, ok := isORMTable(t); ok {
		return table
	}
	t = types.Unalias(t)
	if tuple, ok := t.(*types.Tuple); ok {
		for i := 0; i < tuple.Len(); i++ {
			if table := embeddedTable(tuple.At(i).Type(), seen); table != nil {
				return table
			}
		}
		return nil
	}
	if ptr, ok := t.(*types.Pointer); ok {
		t = ptr.Elem()
	}
	if st, ok := t.Underlying().(*types.Struct); ok {
		for i := 0; i < st.NumFields(); i++ {
			if st.Field(i).Embedded() {
				if table := embeddedTable(st.Field(i).Type(), seen); table != nil {
					return table
				}
			}
		}
	}
	return nil
}
