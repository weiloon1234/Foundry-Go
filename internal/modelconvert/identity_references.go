package modelconvert

import (
	"context"
	"go/token"
	"go/types"
	"sort"

	"github.com/weiloon1234/Foundry-Go/internal/generate"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

// Include test variants in reference discovery even though declarations in tests
// are never converted. Exported identities also require module-wide callers.
func identityReferences(ctx context.Context, directory string, packages []*generate.AnalysisPackage) (map[string]map[string][]token.Position, error) {
	exported := false
	for _, pkg := range packages {
		for _, name := range pkg.Types.Scope().Names() {
			object, ok := pkg.Types.Scope().Lookup(name).(*types.Var)
			if ok && object.Exported() && isModelIdentity(object.Type()) {
				exported = true
			}
		}
	}
	if exported {
		root, err := moduleRoot(directory)
		if err != nil {
			return nil, err
		}
		callers, err := generate.LoadAnalysis(ctx, root, []string{"./..."})
		if err != nil {
			return nil, safeerror.Wrap("load exported identity references", err)
		}
		packages = append(append([]*generate.AnalysisPackage(nil), packages...), callers...)
	}
	result := map[string]map[string][]token.Position{}
	seen := map[token.Position]bool{}
	for _, pkg := range packages {
		for id, object := range pkg.TypesInfo.Uses {
			variable, ok := object.(*types.Var)
			if !ok || variable.Pkg() == nil || variable.Parent() != variable.Pkg().Scope() || !isModelIdentity(variable.Type()) {
				continue
			}
			position := pkg.Fset.Position(id.Pos())
			if seen[position] {
				continue
			}
			seen[position] = true
			path := variable.Pkg().Path()
			if result[path] == nil {
				result[path] = map[string][]token.Position{}
			}
			result[path][variable.Name()] = append(result[path][variable.Name()], position)
		}
	}
	for _, variables := range result {
		for _, positions := range variables {
			sort.Slice(positions, func(i, j int) bool {
				if positions[i].Filename != positions[j].Filename {
					return positions[i].Filename < positions[j].Filename
				}
				return positions[i].Offset < positions[j].Offset
			})
		}
	}
	return result, nil
}
func isModelIdentity(t types.Type) bool {
	n, ok := types.Unalias(t).(*types.Named)
	return ok && n.Obj().Pkg() != nil && n.Obj().Pkg().Path() == ormPath && n.Obj().Name() == "ModelIdentity"
}
