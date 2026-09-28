package clientcontracts_test

import (
	"reflect"
	"testing"

	"foundry.test/consumer/clientcontracts"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
)

func TestExplicitMapKeepsArbitraryNamesAndConcreteValues(t *testing.T) {
	codec, err := clientcontracts.ExplicitLabelsJSON()
	if err != nil {
		t.Fatal(err)
	}
	limits := foundryhttp.DefaultEndpointLimits().Response
	for _, input := range []clientcontracts.ExplicitLabels{nil, {}, {"": "empty name", "__proto__": "ordinary name", "01": "text key", "é/~/": "unicode"}} {
		wire, err := codec.Encode(t.Context(), input, limits)
		if err != nil {
			t.Fatal(err)
		}
		got, err := codec.Decode(t.Context(), wire, limits)
		if err != nil || !reflect.DeepEqual(got, input) {
			t.Fatal("explicit map contract changed", err)
		}
	}
	for _, wire := range []string{`{"name":1}`, `{"name":null}`, `{"name":"one","name":"two"}`} {
		if got, err := codec.Decode(t.Context(), []byte(wire), limits); err == nil || got != nil {
			t.Fatal("invalid map escaped the value contract")
		}
	}
}
