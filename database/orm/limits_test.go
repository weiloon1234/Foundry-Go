package orm_test

import "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"testing"

	"github.com/weiloon1234/Foundry-Go/internal/querycore"
)

func TestNativeLimitsShareOwners(t *testing.T) {
	limits := map[string][2]int{
		"MaxInsertRows":        {orm.MaxInsertRows, querycore.MaxInsertRows},
		"MaxPerModelWriteRows": {orm.MaxPerModelWriteRows, querycore.MaxPerModelWriteRows},
		"MaxRelationWriteRows": {orm.MaxRelationWriteRows, querycore.MaxPerModelWriteRows},
		"MaxIdentityBatch":     {orm.MaxIdentityBatch, querycore.MaxIdentityBatch},
		"MaxChunkSize":         {orm.MaxChunkSize, querycore.MaxChunkSize},
		"DefaultChunkSize":     {orm.DefaultChunkSize, querycore.DefaultChunkSize},
		"MaxPageSize":          {orm.MaxPageSize, querycore.MaxPageSize},
		"MaxCursorBytes":       {orm.MaxCursorBytes, querycore.MaxCursorBytes},
		"MaxCursorFields":      {orm.MaxCursorFields, querycore.MaxCursorFields},
		"MaxExpressionDepth":   {orm.MaxExpressionDepth, querycore.MaxExpressionDepth},
		"MaxExpressionNodes":   {orm.MaxExpressionNodes, querycore.MaxExpressionNodes},
		"MaxParameters":        {orm.MaxParameters, querycore.MaxParameters},
		"MaxMembershipValues":  {orm.MaxMembershipValues, querycore.MaxMembershipValues},
		"MaxScalarSQLBytes":    {orm.MaxScalarSQLBytes, querycore.MaxScalarSQLBytes},
	}
	for name, values := range limits {
		if values[0] != values[1] {
			t.Errorf("%s diverges from querycore: %v", name, values)
		}
	}
	// One public native spelling per limit; rows and expr share the statement
	// budgets, so they must not publish alternate copies of these constants.
	seen := make(map[string]int)
	for _, dir := range []string{".", "expr", "rows"} {
		paths, err := filepath.Glob(filepath.Join(dir, "*.go"))
		if err != nil {
			t.Fatal(err)
		}
		for _, path := range paths {
			file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
			if err != nil {
				t.Fatal(err)
			}
			for _, decl := range file.Decls {
				general, ok := decl.(*ast.GenDecl)
				if !ok || general.Tok != token.CONST {
					continue
				}
				for _, spec := range general.Specs {
					for _, name := range spec.(*ast.ValueSpec).Names {
						if _, tracked := limits[name.Name]; tracked {
							seen[name.Name]++
						}
					}
				}
			}
		}
	}
	for name := range limits {
		if seen[name] != 1 {
			t.Errorf("%s has %d public native spellings, want one", name, seen[name])
		}
	}
}
