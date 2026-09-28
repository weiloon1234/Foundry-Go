package invalid

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/contract/manifest"
	"github.com/weiloon1234/Foundry-Go/openapi"
	"github.com/weiloon1234/Foundry-Go/typescript"
)

func invalid(ctx context.Context, source *manifest.Manifest) {
	_, _ = typescript.Generate(ctx, source, openapi.Options{})
}
