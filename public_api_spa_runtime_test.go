package foundry_test

import "testing"

// The source inventory in public_api_guard_test.go owns discovery. The only new
// private-method exceptions are the proxy's io.Closer and HEAD writer adapters.
func TestSPARuntimeConsumerAPIInventory(t *testing.T) {
	want := map[string]bool{
		"http.SPARuntimeConfig":          false,
		"http.DefaultSPARuntimeConfig":   false,
		"http.SPARuntimeConfig.Validate": false,
		"http.SPARuntime":                false,
		"http.DefineSPARuntime":          false,
		"http.SPARuntime.Validate":       false,
		"http.SPARuntime.WrapProxy":      false,
	}
	for _, declaration := range consumerAPIDeclarations(t) {
		name := declaration.Package + "."
		if declaration.Receiver != "" {
			name += declaration.Receiver + "."
		}
		name += declaration.Name
		if _, exists := want[name]; exists {
			if !declaration.Documented {
				t.Errorf("%s needs documentation", name)
			}
			want[name] = true
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("SPA runtime API missing from source inventory: %s", name)
		}
	}
}
