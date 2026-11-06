package foundry_test

import (
	"context"
	"testing"

	"github.com/weiloon1234/Foundry-Go/application"
	"github.com/weiloon1234/Foundry-Go/diagnostics"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/telemetry"
)

// Pin the configured surface alongside the source-owned API discovery inventory.
var (
	_ func(*application.Builder, foundation.Key[*diagnostics.Runtime]) *application.Builder = (*application.Builder).TelemetryReadiness
	_ func(application.Services) (*telemetry.Engine, error)                                 = application.Services.Telemetry
	_ foundation.Key[*telemetry.Engine]                                                     = application.TelemetryKey
	_ foundation.ProviderID                                                                 = application.TelemetryProvider
	_ application.TelemetrySettings                                                         = application.FeatureSettings{}.Telemetry
	_ func(*telemetry.Engine, context.Context) error                                        = (*telemetry.Engine).Close
)

func TestApplicationTelemetryConsumerAPIInventory(t *testing.T) {
	want := map[string]bool{"TelemetrySettings": false, "Builder.TelemetryReadiness": false, "Services.Telemetry": false}
	for _, declaration := range consumerAPIDeclarations(t) {
		if declaration.Package != "application" {
			continue
		}
		name := declaration.Name
		if declaration.Receiver != "" {
			name = declaration.Receiver + "." + name
		}
		if _, ok := want[name]; !ok {
			continue
		}
		if !declaration.Documented {
			t.Errorf("application.%s needs documentation", name)
		}
		want[name] = true
	}
	for name, found := range want {
		if !found {
			t.Errorf("application telemetry API missing: %s", name)
		}
	}
}
