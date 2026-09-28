package httpdto_test

import (
	"errors"
	"strings"
	"testing"

	"foundry.test/consumer/httpdto"
	"foundry.test/consumer/httpquery"
	"github.com/weiloon1234/Foundry-Go/contract"
)

func TestGeneratedCustomJSONContractSharesURLScalar(t *testing.T) {
	descriptor := httpdto.TrackingResponseJSON()
	schema, err := descriptor.Description()
	if err != nil {
		t.Fatal(err)
	}
	scalar, err := httpquery.TrackingScalar.Description()
	if err != nil {
		t.Fatal(err)
	}
	seen := 0
	for _, typ := range schema.Types {
		if typ.ID == scalar.ID {
			seen++
			if typ.Kind != scalar.Kind || typ.Format != scalar.Format {
				t.Fatal("custom URL/JSON contract drift")
			}
		}
	}
	if seen != 1 {
		t.Fatal("custom declaration duplicated or omitted", seen)
	}
	limits := contract.JSONLimits{Bytes: 1024, Depth: 16, Nodes: 100, Steps: 200, Issues: 10}
	for _, text := range []string{
		`{"code":"track_123"}`,
		`{"code":"track_123","history":["track_456"],"next":null}`,
		`{"code":"track_123","next":"track_789"}`,
	} {
		input, err := descriptor.Decode(t.Context(), []byte(text), limits)
		if err != nil || input.Code != httpquery.TrackingCode("track_123") {
			t.Fatal("custom decode", err)
		}
		output, err := descriptor.Encode(t.Context(), input, limits)
		if err != nil || !strings.Contains(string(output), `"code":"track_123"`) {
			t.Fatal("custom encode", err)
		}
	}
	for _, text := range []string{`{"code":123}`, `{"code":"wrong"}`, `{"code":"track_123","next":"wrong"}`} {
		output, err := descriptor.Decode(t.Context(), []byte(text), limits)
		var invalid *contract.DecodeError
		if !errors.As(err, &invalid) || output.Code != "" || output.Next.IsSet() {
			t.Fatal("custom error/partial value", err)
		}
	}
}
