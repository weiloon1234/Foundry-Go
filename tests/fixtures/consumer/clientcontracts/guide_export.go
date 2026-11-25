package clientcontracts

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/cli"
	"github.com/weiloon1234/Foundry-Go/contract/manifest"
	"github.com/weiloon1234/Foundry-Go/openapi"
	"github.com/weiloon1234/Foundry-Go/typescript"
)

// doc-example:begin export-contracts
func ExportClient(ctx context.Context, source *manifest.Manifest, dir string) (typescript.Report, error) {
	return typescript.Generate(ctx, source, typescript.Options{
		Dir:     dir,
		OpenAPI: openapi.Options{Title: "Consumer", APIVersion: "1"},
	})
}

// doc-example:end export-contracts

// doc-example:begin export-command
func ClientExportCommand(sources typescript.Sources) (cli.Declaration, error) {
	return typescript.ExportCommand("contracts:export",
		openapi.Options{Title: "Consumer", APIVersion: "1"}, sources)
}

// doc-example:end export-command
