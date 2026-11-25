// Package modelcheck checks that table-backed models are declared only through the generator.
package modelcheck

import (
	"context"
	"maps"
	"path/filepath"
	"sort"

	"github.com/weiloon1234/Foundry-Go/internal/diagnostic"
	"github.com/weiloon1234/Foundry-Go/internal/generate"
)

// Load includes test sources and honours the selected module's build configuration.
// Loading failures are errors, never an empty successful check.
func Load(ctx context.Context, dir string, patterns []string) ([]*generate.AnalysisPackage, error) {
	return generate.LoadAnalysis(ctx, dir, patterns)
}

// Report is what Check found and what it examined.
type Report struct {
	Findings []diagnostic.Diagnostic
	// EnumColumns is the number of enum columns of the models; EnumCompared those
	// whose CHECK constraints the rule read and compared with the enum.
	EnumColumns, EnumCompared int
}

// Check inspects packages below dir matching patterns for low-level table declarations.
func Check(ctx context.Context, dir string, patterns []string) (report Report, resultErr error) {
	defer func() {
		if ctx.Err() != nil {
			report = Report{}
			resultErr = ctx.Err()
		}
	}()
	var result []diagnostic.Diagnostic
	pkgs, err := Load(ctx, dir, patterns)
	if err != nil {
		return Report{}, err
	}
	analyses := make([]*Analysis, len(pkgs))
	nameParameters := make(map[declarationFunction][]int)
	for i, pkg := range pkgs {
		if ctx.Err() != nil {
			return Report{}, ctx.Err()
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
			return Report{}, ctx.Err()
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
	root, err := filepath.Abs(dir)
	if err != nil {
		return Report{}, err
	}
	analysisByPackage := make(map[*generate.AnalysisPackage]*Analysis, len(pkgs))
	for i, pkg := range pkgs {
		analysisByPackage[pkg] = analyses[i]
	}
	enumFindings, stats := checkEnumLists(root, pkgs, analysisByPackage)
	for _, d := range enumFindings {
		key := d.String()
		if !seen[key] {
			seen[key] = true
			result = append(result, d)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		a, b := result[i], result[j]
		if a.Path != b.Path {
			return a.Path < b.Path
		}
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		if a.Column != b.Column {
			return a.Column < b.Column
		}
		return a.Subject < b.Subject
	})
	return Report{Findings: result, EnumColumns: stats.Fields, EnumCompared: stats.Compared}, nil
}
