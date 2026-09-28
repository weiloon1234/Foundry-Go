package plugin

import (
	"github.com/weiloon1234/Foundry-Go/config"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
)

// LoadConfig uses the existing typed configuration loader: owned defaults,
// ordered files, environment, then explicit application overrides. Every field
// must remain in this manifest's exact namespace. Unknown file/override keys
// still fail. Return the error from the plugin constructor before registration.
// The report contains provenance, never configuration values.
func LoadConfig[T any](declaration Manifest, schema *config.Schema[T], defaults T, inputs config.Inputs[T]) (T, config.Report, error) {
	var result T
	var report config.Report
	err := callback.Isolated("load plugin configuration", func() error {
		if err := declaration.Validate(); err != nil {
			return err
		}
		namespace, err := declaration.Namespace()
		if err != nil {
			return err
		}
		if err := schema.WithinNamespace(namespace); err != nil {
			return err
		}
		result, report, err = schema.Load(defaults, inputs)
		return err
	})
	if err != nil {
		var zero T
		return zero, config.Report{}, err
	}
	return result, report, nil
}
