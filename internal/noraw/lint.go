// Package noraw checks resolved Go calls for Foundry's explicit raw SQL APIs.
package noraw

import (
	"context"
	"fmt"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"go/ast"
	"go/token"
	"go/types"
	"sort"
	"strings"

	"github.com/weiloon1234/Foundry-Go/internal/frameworkinfo"
)

const databasePath = frameworkinfo.ModulePath + "/database"

type Diagnostic struct {
	Position token.Position
	Symbol   string
}

func (d Diagnostic) String() string {
	return fmt.Sprintf("%s:%d", d.Position.Filename, d.Position.Line)
}

// Check diagnoses calls in consumer packages, including tests. Loading failures
// are errors even when the selected packages belong to the framework itself.
func Check(ctx context.Context, dir string, patterns []string) ([]Diagnostic, error) {
	graph, err := load(ctx, dir, patterns)
	if err != nil {
		return nil, err
	}
	var contextType types.Type
	if _, ok := graph.exports["context"]; ok {
		p, err := graph.importer.Import("context")
		if err != nil {
			return nil, safeerror.New(fmt.Sprintf("no-raw: cannot load context type information"))
		}
		contextType = p.Scope().Lookup("Context").Type()
	}
	signatures, executorType, err := graph.rawSignatures()
	if err != nil {
		return nil, err
	}
	var diagnostics []Diagnostic
	seen := map[string]bool{}
	for _, pkg := range graph.selected {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		path, _, _ := strings.Cut(pkg.ImportPath, " [")
		if path == frameworkinfo.ModulePath || strings.HasPrefix(path, frameworkinfo.ModulePath+"/") {
			continue
		}
		files, info, err := graph.check(pkg)
		if err != nil {
			return nil, err
		}
		resolver := callResolver{info: info, contextType: contextType, executorType: executorType, signatures: signatures}
		for _, file := range files {
			ast.Inspect(file, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok || info.Types[call.Fun].IsType() {
					return true
				}
				symbol := resolver.target(call.Fun)
				if symbol == "" {
					return true
				}
				pos := graph.fset.PositionFor(call.Pos(), false)
				d := Diagnostic{pos, symbol}
				if key := d.String(); !seen[key] {
					seen[key] = true
					diagnostics = append(diagnostics, d)
				}
				return true
			})
		}
	}
	sort.Slice(diagnostics, func(i, j int) bool {
		a, b := diagnostics[i].Position, diagnostics[j].Position
		if a.Filename != b.Filename {
			return a.Filename < b.Filename
		}
		return a.Offset < b.Offset
	})
	return diagnostics, nil
}

func frameworkRaw(f *types.Func) string {
	if f.Pkg() == nil {
		return ""
	}
	path, name := f.Pkg().Path(), f.Name()
	sig := f.Type().(*types.Signature)
	if sig.Recv() == nil {
		switch path {
		case databasePath:
			if name == "ScanOne" || name == "ReadQuery" || name == "ForEach" {
				return "database." + name
			}
		case databasePath + "/orm":
			if name == "WhereRaw" {
				return "orm.WhereRaw"
			}
		case databasePath + "/orm/rows":
			if name == "Raw" {
				return "rows.Raw"
			}
		case databasePath + "/orm/expr":
			if name == "Raw" || name == "RawWithCodec" {
				return "expr." + name
			}
		}
		return ""
	}
	if path == databasePath && (name == "Exec" || name == "Query" || name == "QueryRead" || name == "ScanOne") {
		return "database." + name
	}
	if path == databasePath+"/orm" && name == "Raw" {
		typ := sig.Recv().Type()
		if p, ok := typ.(*types.Pointer); ok {
			typ = p.Elem()
		}
		if n, ok := types.Unalias(typ).(*types.Named); ok && n.Origin().Obj().Name() == "Table" {
			return "orm.Table.Raw"
		}
	}
	return ""
}

// Result types deliberately do not participate: Runner[R] and constraint methods
// can expose raw SQL without fixing the driver's concrete result representation.
func executorMethod(f *types.Func, receiver, contextType, executorType types.Type) string {
	if contextType == nil {
		return ""
	}
	switch f.Name() {
	case "Exec", "Query", "QueryRead", "ScanOne", "Raw":
	default:
		return ""
	}
	if receiver == nil {
		return ""
	}
	if _, ok := receiver.Underlying().(*types.Interface); !ok {
		return ""
	}
	method := types.NewMethodSet(receiver).Lookup(f.Pkg(), f.Name())
	if method == nil {
		return ""
	}
	sig := method.Obj().Type().(*types.Signature)
	params := sig.Params()
	if !sig.Variadic() || params.Len() < 3 || !types.Identical(params.At(0).Type(), contextType) {
		return ""
	}
	sqlIndex := 1
	if f.Name() == "Raw" {
		if executorType == nil || params.Len() != 4 || !types.Identical(params.At(1).Type(), executorType) {
			return ""
		}
		sqlIndex = 2
	}
	if !types.Identical(params.At(sqlIndex).Type(), types.Typ[types.String]) {
		return ""
	}
	count := params.Len()
	if count != sqlIndex+2 && !(f.Name() == "ScanOne" && count == 4) {
		return ""
	}
	anySlice := types.NewSlice(types.Universe.Lookup("any").Type())
	for i := sqlIndex + 1; i < count; i++ {
		if !types.Identical(params.At(i).Type(), anySlice) {
			return ""
		}
	}
	if f.Name() == "Raw" {
		return "orm.Table.Raw (interface contract)"
	}

	return "database." + f.Name() + " (interface contract)"
}
