package httpquery_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"foundry.test/consumer/httpquery"
	"foundry.test/consumer/models"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestConsumerQueryContracts(t *testing.T) {
	ctx := context.Background()
	limits := foundryhttp.QueryLimits{Bytes: 2048, Pairs: 32, Issues: 8}
	parameters := httpquery.SearchInputDescriptor()
	input, err := parameters.Decode(ctx, "q=Jane+Doe&status=active&status=disabled&user=0192f915-3cf3-7000-8000-000000000001", limits)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(input.Statuses, []models.Status{models.StatusActive, models.StatusDisabled}) {
		t.Fatal("generated enum codec was not reused")
	}
	input.Search = value.Set("李 + Smith")
	encoded, err := parameters.Encode(ctx, input, limits)
	if err != nil {
		t.Fatal(err)
	}
	again, err := parameters.Decode(ctx, encoded, limits)
	if err != nil || !reflect.DeepEqual(input, again) {
		t.Fatalf("round trip: %v", err)
	}
	_, err = parameters.Decode(ctx, "status=active&status=unknown&user="+input.User.String(), limits)
	var failure *foundryhttp.QueryError
	if !errors.As(err, &failure) || failure.Issues()[0].Path != "/status/1" {
		t.Fatalf("enum membership failure: %v", err)
	}
	input.Statuses = []models.Status{"unknown"}
	if output, err := parameters.Encode(ctx, input, limits); output != "" || err == nil {
		t.Fatal("invalid enum output accepted")
	}
}
