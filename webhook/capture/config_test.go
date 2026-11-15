package capture

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/fault"
)

func TestConfigMaskNamesValidation(t *testing.T) {
	for _, test := range []struct {
		name  string
		names []string
		want  string
	}{
		{"default", nil, ""},
		{"empty list", []string{}, ""},
		{"count boundary", slices.Repeat([]string{"query"}, 64), ""},
		{"length boundary", []string{strings.Repeat("é", 64)}, ""},
		{"over count", slices.Repeat([]string{"query"}, 65), "webhook capture has too many mask names"},
		{"count precedes name validation", make([]string, 65), "webhook capture has too many mask names"},
		{"empty name", []string{""}, "webhook capture has an invalid mask name"},
		{"over byte length", []string{strings.Repeat("é", 65)}, "webhook capture has an invalid mask name"},
		{"control", []string{"query\n"}, "webhook capture has an invalid mask name"},
		{"unicode control", []string{"query\u0085"}, "webhook capture has an invalid mask name"},
	} {
		t.Run(test.name, func(t *testing.T) {
			config := DefaultConfig()
			config.MaskNames = test.names
			err := config.Validate()
			if test.want == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if !errors.Is(err, fault.Invalid) || err.Error() != "invalid: "+test.want {
				t.Fatalf("Validate returned %v, want %s", err, test.want)
			}
			if _, err := NewRecorder(config); !errors.Is(err, fault.Invalid) {
				t.Fatal("recorder accepted invalid mask names", err)
			}
		})
	}
}
