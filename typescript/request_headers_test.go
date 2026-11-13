package typescript

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/contract/manifest"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/validation"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestRequestHeaderRuntime(t *testing.T) {
	type input struct {
		Version  string
		Enabled  value.Optional[bool]
		Sequence value.Optional[int64]
	}
	version := foundryhttp.HeaderParam("X-App-Version", foundryhttp.StringQuery[string](), func(q *input) *string { return &q.Version })
	enabled := foundryhttp.OptionalHeaderParam("X-Enabled", foundryhttp.BoolQuery[bool](), func(q *input) *value.Optional[bool] { return &q.Enabled })
	sequence := foundryhttp.OptionalHeaderParam("X-Sequence", foundryhttp.IntegerQuery[int64](), func(q *input) *value.Optional[int64] { return &q.Sequence })
	route := func(id foundryhttp.RouteID, params foundryhttp.Query[input]) foundryhttp.RouteRegistration {
		d := foundryhttp.DefineEndpoint(foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: id, Method: foundryhttp.GET, Access: foundryhttp.Public}, foundryhttp.StaticPath("/"+string(id))), params, foundryhttp.EmptyBody(), foundryhttp.EmptyResponse(204))
		return d.Handle(func(context.Context, foundryhttp.Input[foundryhttp.NoPath, input, foundryhttp.NoBody]) (foundryhttp.NoContent, error) {
			return foundryhttp.NoContent{}, nil
		})
	}
	router, err := foundryhttp.NewRouter(route("headers", foundryhttp.DefineQuery(version, enabled, sequence)), route("optional", foundryhttp.DefineQuery(enabled)))
	if err != nil {
		t.Fatal(err)
	}
	source, err := manifest.Build(t.Context(), manifest.Sources{HTTP: router})
	if err != nil {
		t.Fatal(err)
	}
	doc, _ := source.Snapshot()
	rule, err := validation.DescribeRule(validation.Rules(validation.DefineField("headers", func(q input) input { return q }), validation.Rules(validation.DefineField("X-Sequence", func(q input) value.Optional[int64] { return q.Sequence }), validation.Optional(validation.Min[int64](0)))))
	if err != nil {
		t.Fatal(err)
	}
	doc.HTTP[0].Validation = &rule
	doc.HTTP[0].Limits.Headers = foundryhttp.RequestHeaderLimits{FieldBytes: 20, Bytes: 50, Fields: 3, Values: 3, Issues: 4}
	data, _ := json.Marshal(doc)
	source, err = manifest.Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	output, err := Render(source)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(output), `readonly headers?: { readonly "X-Enabled"?:`) {
		t.Fatal("optional-only headers require an object")
	}
	output = append(output, []byte(`
function headerTypeChecks(api: API) {
  void api.optional({});
  void api.optional({ headers: { "X-Enabled": false } });
  void api.headers({ headers: { "X-App-Version": "", "X-Enabled": false, "X-Sequence": "0" } });
  // @ts-expect-error Required header member.
  void api.headers({});
  // @ts-expect-error Scalar types are retained.
  void api.optional({ headers: { "X-Enabled": "false" } });
  // @ts-expect-error Explicit undefined is not omission.
  void api.optional({ headers: { "X-Enabled": undefined } });
}
`)...)
	runReleaseTypeScript(t, output, `
const sent = [];
const transport = async request => { sent.push(request); return { status: 204, headers: {}, body: "", close: async () => {} }; };
const api = sdk.createClient(transport);
await api.optional({});
await api.optional({headers:{"X-Enabled": false}});
assert.equal(sent[0].headers["x-enabled"], undefined);
assert.equal(sent[1].headers["x-enabled"], "false");
await api.headers({headers:{"X-App-Version":"", "X-Sequence":"0"}});
assert.equal(sent[2].headers["x-app-version"], "");
assert.equal(sent[2].headers["x-sequence"], "0");
assert.equal(sdk.operation("headers").field("headers", "X-Sequence").path, "/headers/X-Sequence");
const report = sdk.validateRequest("headers", {headers:{"X-App-Version":"2", "X-Sequence":"-1"}});
assert.equal(report.complete, true);
assert.equal(report.issues[0].path, "/headers/X-Sequence");
await assert.rejects(api.headers({headers:{"X-App-Version":"2", "X-Sequence":"-1"}}), sdk.ContractError);
for (const request of [{headers:null}, {headers:{"x-enabled":true}}, {headers:{"X-Enabled":null}}, {headers:{"X-Enabled":undefined}}, {headers:{"X-Enabled":"false"}}]) {
  await assert.rejects(api.optional(request), sdk.ContractError);
}
await assert.rejects(api.optional({}, {headers:{"X-Enabled":"false"}}), sdk.ContractError);
await assert.rejects(sdk.createClient(transport, {headers:{"x-enabled":"false"}}).optional({}), sdk.ContractError);
for (const headers of [
  {"X-App-Version":"a".repeat(21)},
  {"X-App-Version":"a".repeat(20), "X-Sequence":"1234567890123456789"},
  {"X-App-Version":"2", "X-Sequence":1},
  {"X-App-Version":"2", "X-Sequence":"01"},
  {"X-App-Version":"2", "X-Sequence":"9223372036854775808"},
]) await assert.rejects(api.headers({headers}), sdk.ContractError);
assert.equal(sent.length, 3);
// Exact per-field and aggregate boundaries: 13+20 + 10+7 = 50 bytes.
await api.headers({headers:{"X-App-Version":"a".repeat(20), "X-Sequence":"1234567"}});
assert.equal(sent.length, 4);
`)
}

func TestRequestHeaderCustomFormatterAndPreparation(t *testing.T) {
	type input struct{ Text string }
	endpoint := foundryhttp.DefineEndpoint(foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "custom", Method: foundryhttp.GET, Access: foundryhttp.Public}, foundryhttp.StaticPath("/custom")), foundryhttp.DefineQuery(foundryhttp.HeaderParam("X-Custom", foundryhttp.StringQuery[string](), func(q *input) *string { return &q.Text })), foundryhttp.EmptyBody(), foundryhttp.EmptyResponse(204))
	router, err := foundryhttp.NewRouter(endpoint.Handle(func(context.Context, foundryhttp.Input[foundryhttp.NoPath, input, foundryhttp.NoBody]) (foundryhttp.NoContent, error) {
		return foundryhttp.NoContent{}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	source, err := manifest.Build(t.Context(), manifest.Sources{HTTP: router})
	if err != nil {
		t.Fatal(err)
	}
	doc, _ := source.Snapshot()
	doc.HTTP[0].Headers[0].Syntax = foundryhttp.CustomURLSyntax
	doc.HTTP[0].Preparation = true
	data, _ := json.Marshal(doc)
	source, err = manifest.Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	output, err := Render(source)
	if err != nil {
		t.Fatal(err)
	}
	runReleaseTypeScript(t, output, `
let calls = 0;
const transport = async request => { calls++; assert.equal(request.headers["x-custom"], "a\tb"); return {status:204, headers:{}, body:"", close:async()=>{}}; };
const input = {headers:{"X-Custom":"semantic"}};
const options = text => ({urlCodecs:{string:{format: () => text}}});
await assert.rejects(sdk.createClient(transport).custom(input), sdk.ContractError);
for (const text of [" leading", "trailing\t", "\r", "\n", "\0", "é", "x".repeat(8193), null]) {
  await assert.rejects(sdk.createClient(transport, options(text)).custom(input), sdk.ContractError);
}
assert.equal(calls, 0);
await sdk.createClient(transport, options("a\tb")).custom(input);
assert.equal(calls, 1);
const report = sdk.validateRequest("custom", input, options("a\tb"));
assert.equal(report.complete, false);
assert.deepEqual(report.skipped, ["request_preparation"]);
`)
}
