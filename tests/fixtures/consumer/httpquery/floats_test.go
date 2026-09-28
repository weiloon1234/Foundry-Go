package httpquery_test

import (
	"math"
	"reflect"
	"testing"

	"foundry.test/consumer/httpquery"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
)

func TestConsumerFloatQuery(t *testing.T) {
	limits := foundryhttp.QueryLimits{Bytes: 1024, Pairs: 16, Issues: 4}
	descriptor := httpquery.NearbyInputDescriptor()
	input, err := descriptor.Decode(t.Context(), "latitude=1.000000059604644775390626&distance=0.1&distance=1e%2B20&maximum=-0", limits)
	if err != nil {
		t.Fatal(err)
	}
	maximum, set := input.Maximum.Get()
	if !set || !math.Signbit(float64(maximum)) || float32(input.Latitude) != math.Nextafter32(1, 2) || !reflect.DeepEqual(input.Distances, httpquery.Distances{.1, 1e20}) {
		t.Fatal("generated float values changed")
	}
	raw, err := descriptor.Encode(t.Context(), input, limits)
	if err != nil {
		t.Fatal(err)
	}
	again, err := descriptor.Decode(t.Context(), raw, limits)
	if err != nil || !reflect.DeepEqual(input, again) {
		t.Fatalf("float round trip: %v", err)
	}
	location, err := httpquery.PositionPathDescriptor().URL(httpquery.PositionPath{Latitude: .1})
	if err != nil || location != "/positions/0.1" {
		t.Fatalf("float path: %q, %v", location, err)
	}
}
