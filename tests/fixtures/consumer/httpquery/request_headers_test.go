package httpquery_test

import (
	"reflect"
	"testing"

	"foundry.test/consumer/httpquery"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
)

func TestConsumerRequestHeaderDeclarations(t *testing.T) {
	parameters := httpquery.RequestParametersDescriptor()
	headers, err := foundryhttp.RequestHeaderParameters(parameters)
	if err != nil {
		t.Fatal(err)
	}
	if len(headers) != 2 || headers[0].Name != "X-App-Version" || !headers[0].Required ||
		headers[1].Name != "X-Device-Id" || headers[1].Required || headers[0].Repeated || headers[1].Repeated {
		t.Fatalf("required and optional header declarations: %+v", headers)
	}
	for _, header := range headers {
		if header.Scalar == nil || header.Scalar.Syntax != foundryhttp.TextURLSyntax {
			t.Fatalf("header scalar metadata missing: %+v", header)
		}
	}
	query, err := foundryhttp.QueryParameters(parameters)
	if err != nil || len(query) != 1 || query[0].Name != "q" || query[0].Required {
		t.Fatalf("query projection: %+v, %v", query, err)
	}
	info, err := foundryhttp.InspectEndpoint(httpquery.RequestHeadersEndpoint())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(info.Headers, headers) || !reflect.DeepEqual(info.Query, query) {
		t.Fatalf("endpoint metadata differs from generated declarations: %+v", info)
	}
	headers[0].Name = "X-Changed"
	info.Headers[1].Name = "X-Changed"
	again, err := foundryhttp.RequestHeaderParameters(httpquery.RequestParametersDescriptor())
	if err != nil || again[0].Name != "X-App-Version" || again[1].Name != "X-Device-Id" {
		t.Fatalf("inspection changed the cached descriptor: %+v, %v", again, err)
	}
}
