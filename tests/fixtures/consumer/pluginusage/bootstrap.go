// Package pluginusage is a thin independent application consumer of two
// separately built plugin modules. It contains no provider wrapper for plugins.
package pluginusage

import (
	"context"

	"foundry.test/pluginbase"
	"foundry.test/plugindep"
	foundry "github.com/weiloon1234/Foundry-Go"
	"github.com/weiloon1234/Foundry-Go/config"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/plugin/assets"
	"github.com/weiloon1234/Foundry-Go/plugin/scaffold"
)

func Builder(prefix string) (*foundation.Builder, error) {
	base, err := pluginbase.New(config.Inputs[pluginbase.Settings]{Overrides: []config.Override[pluginbase.Settings]{pluginbase.Prefix.Set(prefix)}})
	if err != nil {
		return nil, err
	}
	reports, err := plugindep.New()
	if err != nil {
		return nil, err
	}
	return foundry.New().RegisterPlugin(reports, base), nil
}

func Inspect(ctx context.Context, prefix string) (foundation.Inspection, error) {
	builder, err := Builder(prefix)
	if err != nil {
		return foundation.Inspection{}, err
	}
	return builder.Inspect(ctx)
}

func Bundles(resolver foundation.Resolver) ([]*assets.Bundle, error) { return assets.Bundles(resolver) }
func ReportScaffold(resolver foundation.Resolver) (*scaffold.Scaffold[plugindep.ScaffoldInput], error) {
	return scaffold.Resolve[plugindep.ScaffoldInput](resolver, plugindep.ID, "report")
}

func OverrideTitle(builder *foundation.Builder, title string) *foundation.Builder {
	return builder.OverrideContributions("application", func(r *foundation.Registrar) error { return foundation.Provide(r, pluginbase.Title, title) })
}
