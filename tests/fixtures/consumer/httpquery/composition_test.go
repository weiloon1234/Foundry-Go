package httpquery_test

import (
	"reflect"
	"strings"
	"testing"

	"foundry.test/consumer/httpquery"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
)

func TestGeneratedQueryCompositionAndDefaults(t *testing.T) {
	t.Parallel()
	limits := foundryhttp.QueryLimits{Bytes: 4096, Pairs: 128, Issues: 10}
	const user = "0192f915-3cf3-7000-8000-000000000001"
	descriptor := httpquery.SearchWindowParameters
	input, err := descriptor.Decode(t.Context(), "user="+user+"&q=hello%2Bworld", limits)
	if err != nil {
		t.Fatal(err)
	}
	if input.Filters.User.String() != user || input.Size != 20 {
		t.Fatalf("typed filter/default lost: %+v", input)
	}
	if term, _ := input.Filters.Search.Get(); term != "hello+world" {
		t.Fatal("filter escaping changed")
	}
	encoded, err := descriptor.Encode(t.Context(), input, limits)
	if err != nil || !strings.Contains(encoded, "size=20") {
		t.Fatalf("bad encoding: %q %v", encoded, err)
	}
	roundtrip, err := descriptor.Decode(t.Context(), encoded, limits)
	if err != nil || !reflect.DeepEqual(input, roundtrip) {
		t.Fatal("composed consumer query lost round trip")
	}
	metadata, err := descriptor.Parameters()
	if err != nil {
		t.Fatal(err)
	}
	hasDefault := false
	for _, field := range metadata {
		if field.Name == "size" {
			text, present := field.DefaultURL.Get()
			hasDefault = present && text == "20" && !field.Required && field.Scalar != nil
		}
	}
	if !hasDefault {
		t.Fatal("runtime default not exposed in typed metadata")
	}
}
