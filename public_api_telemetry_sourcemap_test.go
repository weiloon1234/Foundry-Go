package foundry_test

import "testing"

func TestTelemetrySourceMapConsumerAPIInventory(t *testing.T) {
	want := map[string]bool{
		"Asset": false, "ParseAsset": false, "Config": false, "Map": false,
		"PruneResult": false, "Store": false, "New": false,
		"Store.Read": false, "Store.Prune": false,
	}
	for _, declaration := range consumerAPIDeclarations(t) {
		if declaration.Package != "telemetry/sourcemap" {
			continue
		}
		name := declaration.Name
		if declaration.Receiver != "" {
			name = declaration.Receiver + "." + name
		}
		if _, exists := want[name]; !exists {
			t.Errorf("unexpected sourcemap API: %s", name)
			continue
		}
		if !declaration.Documented {
			t.Errorf("sourcemap.%s needs documentation", name)
		}
		want[name] = true
	}
	for name, found := range want {
		if !found {
			t.Errorf("sourcemap API missing from source inventory: %s", name)
		}
	}
}
