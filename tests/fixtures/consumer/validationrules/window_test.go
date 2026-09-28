package validationrules_test

import (
	"errors"
	"testing"

	"foundry.test/consumer/validationrules"
	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/validation"
)

func TestGeneratedWindowRulesCompareInstantsAcrossOffsets(t *testing.T) {
	limits := contract.JSONLimits{Bytes: 4096, Depth: 16, Nodes: 500, Steps: 2000, Issues: 20}
	for _, tc := range []struct {
		end   string
		valid bool
	}{
		{"2026-01-02T00:00:00.000000001Z", true}, {"2026-01-02T00:00:00Z", false},
	} {
		input, err := validationrules.AvailabilityWindowJSON().Decode(t.Context(), []byte(`{"start":"2026-01-02T08:00:00+08:00","end":"`+tc.end+`"}`), limits)
		if err != nil {
			t.Fatal(err)
		}
		err = validationrules.WindowRules().Check(t.Context(), input, validation.DefaultLimits())
		if tc.valid {
			if err != nil {
				t.Fatal(err)
			}
			continue
		}
		var failure *validation.Errors
		if !errors.As(err, &failure) {
			t.Fatal(err)
		}
		issues := failure.Issues()
		if len(issues) != 1 || issues[0].Path != "/start" {
			t.Fatal("window diagnostics", issues)
		}
	}
}
