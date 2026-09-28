package tooling_test

import (
	"context"
	"strings"
	"testing"

	"foundry.test/consumer/outgoing"
	"foundry.test/consumer/tooling"
	"github.com/weiloon1234/Foundry-Go/cli"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	httptest "github.com/weiloon1234/Foundry-Go/testkit/http"
)

func TestTypedCommandsAndPureInspection(t *testing.T) {
	var output strings.Builder
	streams := cli.Streams{In: strings.NewReader(""), Out: &output, Err: &output}
	if err := tooling.Run(t.Context(), []string{"greet", "--name", "Ada", "--count", "2"}, streams); err != nil {
		t.Fatal(err)
	}
	if output.String() != "Hello Ada\nHello Ada\n" {
		t.Fatal("typed CLI behavior changed")
	}
	output.Reset()
	if err := tooling.Run(t.Context(), []string{"inspect", "--section", "commands", "--format", "json"}, streams); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), `"name":"greet"`) || !strings.Contains(output.String(), `"name":"inspect"`) {
		t.Fatal("command inspection lost registry")
	}
	output.Reset()
	if code := cli.Status(tooling.Run(t.Context(), []string{"greet", "--help"}, streams)); code != cli.Success {
		t.Fatal("help did not exit successfully", code)
	}
	if code := cli.Status(tooling.Run(t.Context(), []string{"greet", "--count", "0"}, streams)); code != cli.InvalidUsage {
		t.Fatal("bad arguments did not retain usage status", code)
	}
}

func TestHTTPHelperUsesGeneratedRequestAndResponseContracts(t *testing.T) {
	endpoint := foundryhttp.DefineEndpoint(foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "tooling.create", Method: foundryhttp.POST, Access: foundryhttp.Public}, foundryhttp.StaticPath("/accounts")), foundryhttp.EmptyQuery(), foundryhttp.JSONBody(outgoing.CreateAccountJSON()), foundryhttp.JSONResponse(201, outgoing.AccountReceiptJSON()))
	router, err := foundryhttp.NewRouter(endpoint.Handle(func(_ context.Context, input foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, outgoing.CreateAccount]) (outgoing.AccountReceipt, error) {
		return outgoing.AccountReceipt{ID: 7, Name: input.Body.Name}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	client := httptest.New(t, router)
	request, err := httptest.JSON(t.Context(), client.Post("/accounts"), outgoing.CreateAccountJSON(), outgoing.CreateAccount{Name: "Ada"})
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.Do(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	httptest.AssertStatus(t, response, 201)
	body, err := httptest.DecodeJSON(t.Context(), response, outgoing.AccountReceiptJSON())
	if err != nil || body.ID != 7 || body.Name != "Ada" {
		t.Fatal("typed response changed", err)
	}
}

func TestFactoryRecipeBuildsConcreteGeneratedDrafts(t *testing.T) {
	records, err := tooling.Records()
	if err != nil {
		t.Fatal(err)
	}
	draft, err := records.Draft(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	name, present := draft.Name().Get()
	if !present || name != "record-1" {
		t.Fatal("factory recipe lost generated draft")
	}
}
