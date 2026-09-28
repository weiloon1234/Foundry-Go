package httpdto_test

import (
	"errors"
	"strings"
	"testing"

	"foundry.test/consumer/httpdto"
	"github.com/weiloon1234/Foundry-Go/contract"
)

func TestGeneratedStreamingJSONValues(t *testing.T) {
	d := httpdto.StreamResponseJSON()
	limits := contract.JSONLimits{Bytes: 1024, Depth: 16, Nodes: 100, Steps: 200, Issues: 10}
	for _, text := range []string{
		`{"Label":"label:first","History":["label:second"]}`,
		`{"Label":"label:first","History":[],"Next":null}`,
		`{"Label":"label:first","History":[],"Next":"label:third"}`,
	} {
		input, err := d.Decode(t.Context(), []byte(text), limits)
		if err != nil || input.Label.Text() != "first" {
			t.Fatal("streaming consumer decode", err)
		}
		output, err := d.Encode(t.Context(), input, limits)
		if err != nil || !strings.Contains(string(output), `"Label":"label:first"`) {
			t.Fatal("streaming consumer encode", err)
		}
	}
	for _, text := range []string{
		`{"Label":"missing-prefix","History":[]}`,
		`{"Label":"label:first","History":["wrong"]}`,
		`{"Label":"label:first","History":[],"Next":123}`,
	} {
		input, err := d.Decode(t.Context(), []byte(text), limits)
		var invalid *contract.DecodeError
		if !errors.As(err, &invalid) || input.Label.Text() != "" || input.History != nil || input.Next.IsSet() {
			t.Fatal("streaming partial value returned", err)
		}
	}
}
