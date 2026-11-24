// Package modelcheck checks that table-backed models are declared only through the generator.
package modelcheck

import (
	"context"
	"fmt"
	"go/token"
	"maps"
	"sort"

	"github.com/weiloon1234/Foundry-Go/internal/generate"
)

// Load includes test sources and honours the selected module's build configuration.
// Loading failures are errors, never an empty successful check.
func Load(ctx context.Context, dir string, patterns []string) ([]*generate.AnalysisPackage, error) {
	return generate.LoadAnalysis(ctx, dir, patterns)
}

// Diagnostic names the source location, model or type, and the replacement action.
type Diagnostic struct {
	Position token.Position `json:"position"`
	Target   string         `json:"target"`
	Message  string         `json:"message"`
}

func (d Diagnostic) String() string {
	return fmt.Sprintf("%s: %s: %s", d.Position, d.Target, d.Message)
}

// Check inspects packages below dir matching patterns for low-level table declarations.
func Check(ctx context.Context, dir string, patterns []string) (result []Diagnostic, resultErr error) {
	defer func() {
		if ctx.Err() != nil {
			result = nil
			resultErr = ctx.Err()
		}
	}()
	pkgs, err := Load(ctx, dir, patterns)
	if err != nil {
		return nil, err
	}
	analyses := make([]*Analysis, len(pkgs))
	nameParameters := make(map[declarationFunction][]int)
	for i, pkg := range pkgs {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		a := NewAnalysis(pkg.TypesInfo, pkg.Fset)
		a.Package = pkg.Types
		a.Context = ctx
		for _, file := range pkg.Syntax {
			a.Index(file)
		}
		maps.Copy(nameParameters, a.declarationNameParameters(pkg.Syntax))
		analyses[i] = a
	}
	seen := make(map[string]bool)
	for i, pkg := range pkgs {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		a := analyses[i]
		a.nameParameters = nameParameters
		for _, d := range a.CheckPackage(pkg.Syntax) {
			key := d.String()
			if !seen[key] {
				seen[key] = true
				result = append(result, d)
			}
		}
	}
	sort.Slice(result, func(i, j int) bool {
		a, b := result[i], result[j]
		if a.Position.Filename != b.Position.Filename {
			return a.Position.Filename < b.Position.Filename
		}
		if a.Position.Offset != b.Position.Offset {
			return a.Position.Offset < b.Position.Offset
		}
		return a.Target < b.Target
	})
	return result, nil
}
