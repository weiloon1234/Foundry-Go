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
	// React and Vue emit optional subscription adapters as separate modules.
	// They import their respective peer framework; the core SDK never does.
	React bool
	Vue   bool
	// Surfaces adds one client entry per declared surface beside the full one.
	Surfaces []Surface
}
type Report struct{ Written, Removed []string }

var prefixPattern = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_]{0,63}$`)

// Generate publishes the manifest, SDK and OpenAPI together. The SDK is the
// full entry <prefix>_foundry.gen.ts and one entry per surface, which import
// the shared runtime modules <prefix>_runtime_foundry.gen.ts and
// <prefix>_runtime_realtime_foundry.gen.ts. Obsolete owned artifacts are
// removed, edited/unowned files are refused, and an interrupted publication
// uses the same recovery journal as foundry generate.
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
	surfaces, err := validateSurfaces(options.Surfaces)
	if err != nil {
		return Report{}, err
	}
	outputs, err := renderModules(source, options.Prefix, surfaces)
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
	outputs[options.Prefix+"_manifest_foundry.gen.json"] = metadata
	outputs[options.Prefix+"_openapi_foundry.gen.json"] = api
	if options.React {
		outputs[options.Prefix+"_react_foundry.gen.ts"] = renderFormAdapter(options.Prefix, reactAdapter)
	}
	if options.Vue {
		outputs[options.Prefix+"_vue_foundry.gen.ts"] = renderFormAdapter(options.Prefix, vueAdapter)
	}
	report, err := generate.PublishArtifacts(ctx, options.Dir, outputs, options.Check)
	return Report{Written: report.Written, Removed: report.Removed}, err
}
