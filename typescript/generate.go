package typescript

import (
	"context"
	"regexp"

	"github.com/weiloon1234/Foundry-Go/contract/manifest"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/generate"
	"github.com/weiloon1234/Foundry-Go/openapi"
)

// Options selects an existing output directory and stable file prefix. OpenAPI
// requires the application's title/version. Check is strictly read-only.
type Options struct {
	Dir     string
	Prefix  string
	Check   bool
	OpenAPI openapi.Options
}
type Report struct{ Written, Removed []string }

var prefixPattern = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_]{0,63}$`)

// Generate publishes the manifest, SDK and OpenAPI together. Obsolete owned
// artifacts are removed, edited/unowned files are refused, and an interrupted
// publication uses the same recovery journal as foundry generate.
func Generate(ctx context.Context, source *manifest.Manifest, options Options) (Report, error) {
	if ctx == nil {
		return Report{}, fault.New(fault.Invalid, "client generation requires a context")
	}
	if err := ctx.Err(); err != nil {
		return Report{}, err
	}
	if options.Dir == "" {
		return Report{}, fault.New(fault.Invalid, "client generation requires an output directory")
	}
	if options.Prefix == "" {
		options.Prefix = "contracts"
	}
	if !prefixPattern.MatchString(options.Prefix) {
		return Report{}, fault.New(fault.Invalid, "invalid client artifact prefix")
	}
	sdk, err := Render(source)
	if err != nil {
		return Report{}, err
	}
	api, err := openapi.Render(source, options.OpenAPI)
	if err != nil {
		return Report{}, err
	}
	metadata, err := source.JSON()
	if err != nil {
		return Report{}, err
	}
	outputs := map[string][]byte{
		options.Prefix + "_foundry.gen.ts":            sdk,
		options.Prefix + "_manifest_foundry.gen.json": metadata,
		options.Prefix + "_openapi_foundry.gen.json":  api,
	}
	report, err := generate.PublishArtifacts(ctx, options.Dir, outputs, options.Check)
	return Report{Written: report.Written, Removed: report.Removed}, err
}
