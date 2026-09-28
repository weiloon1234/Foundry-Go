package httpquery_test

import (
	"testing"

	"foundry.test/consumer/httpendpoints"
	"foundry.test/consumer/httpkernel"
	"foundry.test/consumer/httpquery"
	"github.com/weiloon1234/Foundry-Go/contract"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
)

func TestGeneratedURLScalarMetadataReusesTypedEnumAndModelDeclarations(t *testing.T) {
	parameters, err := httpquery.SearchParameters.Parameters()
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]foundryhttp.QueryParameterInfo{}
	for _, parameter := range parameters {
		if parameter.Scalar == nil {
			t.Fatalf("generated parameter %s lost its codec metadata", parameter.Name)
		}
		found[parameter.Name] = parameter
	}
	if !found["user"].Required || found["user"].Scalar.Syntax != foundryhttp.ModelIDURLSyntax || found["user"].Scalar.Value.Format != contract.UUIDFormat {
		t.Fatal("model-owned identity metadata missing")
	}
	status := found["status"]
	if !status.Repeated || status.Scalar.Syntax != foundryhttp.EnumURLSyntax || len(status.Scalar.Value.Cases) != 2 {
		t.Fatalf("generated imported enum metadata: %+v", status)
	}
	if found["q"].Required || found["q"].Scalar.Value.Kind != contract.StringKind {
		t.Fatal("optional string metadata missing")
	}
	path, err := httpkernel.UserFeedPathDescriptor().Parameters()
	if err != nil {
		t.Fatal(err)
	}
	seenEnum := false
	for _, parameter := range path {
		if parameter.Scalar == nil {
			t.Fatal("generated path has no scalar metadata")
		}
		if parameter.Scalar.Syntax == foundryhttp.EnumURLSyntax {
			seenEnum = true
		}
	}
	if !seenEnum {
		t.Fatal("generated path enum descriptor missing")
	}
	endpoint, err := httpendpoints.Update.Description()
	if err != nil || len(endpoint.Path) != 1 || endpoint.Path[0].Scalar == nil || endpoint.Body == nil || endpoint.Response == nil {
		t.Fatalf("complete endpoint input metadata: %+v %v", endpoint, err)
	}
}

func TestCustomTrackingScalarRetainsDomainParsingAndTypedMetadata(t *testing.T) {
	parameters := httpquery.TrackingParameters()
	metadata, err := parameters.Parameters()
	if err != nil || len(metadata) != 1 || metadata[0].Required || metadata[0].Scalar == nil ||
		metadata[0].Scalar.Syntax != foundryhttp.CustomURLSyntax || metadata[0].Scalar.Value.Kind != contract.StringKind {
		t.Fatalf("custom typed scalar metadata: %+v %v", metadata, err)
	}
	limits := foundryhttp.QueryLimits{Bytes: 1024, Pairs: 8, Issues: 8}
	input, err := parameters.Decode(t.Context(), "tracking=track_123", limits)
	if err != nil {
		t.Fatal(err)
	}
	code, present := input.Code.Get()
	if !present || code != httpquery.TrackingCode("track_123") {
		t.Fatal("custom value type or presence was lost")
	}
	encoded, err := parameters.Encode(t.Context(), input, limits)
	if err != nil || encoded != "tracking=track_123" {
		t.Fatal("custom scalar did not round trip", encoded, err)
	}
	if invalid, err := parameters.Decode(t.Context(), "tracking=not-tracking", limits); err == nil || invalid.Code.IsSet() {
		t.Fatal("metadata replaced the domain parser or returned partial input")
	}
}
