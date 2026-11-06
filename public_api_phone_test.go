package foundry_test

import "testing"

// The source inventory owns discovery; phone exports need documentation but no
// private-receiver exceptions. Keep this list explicit without a stale snapshot
// of unrelated packages or source line numbers.
func TestPhoneConsumerAPIInventory(t *testing.T) {
	want := map[string]bool{
		"phone.ExtensionPolicy":               false,
		"phone.Options":                       false,
		"phone.Options.Validate":              false,
		"phone.E164":                          false,
		"phone.E164.String":                   false,
		"phone.E164.IsZero":                   false,
		"phone.Parse":                         false,
		"validation/phone.Number":             false,
		"validation/phone.For":                false,
		"validation/phone.MessageDefinitions": false,
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
			t.Errorf("phone API missing from source inventory: %s", name)
		}
	}
}
