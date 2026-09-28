package plugindep

import (
	"context"
	"fmt"
	"go/ast"
	"go/format"
	"go/token"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/plugin/assets"
)

type ScaffoldInput struct{ TypeName string }

func renderReport(ctx context.Context, input ScaffoldInput) ([]assets.File, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(input.TypeName) > 128 || !token.IsIdentifier(input.TypeName) || !ast.IsExported(input.TypeName) {
		return nil, fault.New(fault.Invalid, "report scaffold requires an exported Go type name of at most 128 bytes")
	}
	data, err := format.Source(fmt.Appendf(nil, "package report\n\ntype %s struct { Title string }\n", input.TypeName))
	if err != nil {
		return nil, err
	}
	return []assets.File{{Path: "report.go", Data: data}}, nil
}
