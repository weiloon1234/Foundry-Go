package typescript

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/contract/manifest"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
)

type RegistryErrorDetails struct {
	ExistingRegistryID string                 `json:"existing_registry_id"`
	State              string                 `json:"state"`
	Count              int32                  `json:"count"`
	Revision           int64                  `json:"revision"`
	Nested             RegistryErrorReference `json:"nested"`
}
type RegistryErrorReference struct {
	Label string `json:"label"`
}
type SeatErrorDetails struct {
	Seat string `json:"seat"`
}

func detailedClientManifest(t *testing.T) *manifest.Manifest {
	t.Helper()
	registry := foundryhttp.DefineErrorDetails("registry_exists", 409, "Registry already exists.", contract.DefineJSON[RegistryErrorDetails](contract.Schema{
		Root: "github.com/weiloon1234/Foundry-Go/typescript.RegistryErrorDetails",
		Types: []contract.Type{
			{ID: "github.com/weiloon1234/Foundry-Go/typescript.RegistryErrorDetails", Kind: contract.ObjectKind, Properties: []contract.Property{
				{Name: "existing_registry_id", Type: "details.RegistryID", Required: true},
				{Name: "state", Type: "details.RegistryState", Required: true},
				{Name: "count", Type: "int32", Required: true},
				{Name: "revision", Type: "int64", Required: true},
				{Name: "nested", Type: "details.RegistryErrorReference", Required: true},
			}},
			{ID: "details.RegistryErrorReference", Kind: contract.ObjectKind, Properties: []contract.Property{{Name: "label", Type: "string", Required: true}}},
			{ID: "details.RegistryID", Kind: contract.StringKind, Format: contract.UUIDFormat},
			{ID: "details.RegistryState", Kind: contract.StringKind, Cases: []json.RawMessage{json.RawMessage(`"active"`)}},
			{ID: "string", Kind: contract.StringKind},
			{ID: "int32", Kind: contract.IntegerKind, Bits: 32, Signed: true},
			{ID: "int64", Kind: contract.IntegerKind, Bits: 64, Signed: true},
		},
	}))
	seat := foundryhttp.DefineErrorDetails("seat_unavailable", 409, "Seat unavailable.", contract.DefineJSON[SeatErrorDetails](contract.Schema{
		Root: "github.com/weiloon1234/Foundry-Go/typescript.SeatErrorDetails",
		Types: []contract.Type{
			{ID: "github.com/weiloon1234/Foundry-Go/typescript.SeatErrorDetails", Kind: contract.ObjectKind, Properties: []contract.Property{{Name: "seat", Type: "string", Required: true}}},
			{ID: "string", Kind: contract.StringKind},
		},
	}))
	route := func(id foundryhttp.RouteID, declarations ...foundryhttp.ErrorDeclarationOption) foundryhttp.RouteRegistration {
		endpoint := foundryhttp.DefineEndpoint(foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: id, Method: foundryhttp.POST, Access: foundryhttp.Public}, foundryhttp.StaticPath("/"+string(id))), foundryhttp.EmptyQuery(), foundryhttp.EmptyBody(), foundryhttp.EmptyResponse(204), foundryhttp.Errors(declarations...))
		return endpoint.Handle(func(context.Context, foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, foundryhttp.NoBody]) (foundryhttp.NoContent, error) {
			return foundryhttp.NoContent{}, nil
		})
	}
	router, err := foundryhttp.NewRouter(route("registry.create", registry, foundryhttp.DefineError("registry_closed", 409, "Registry closed.")), route("seat.reserve", seat))
	if err != nil {
		t.Fatal(err)
	}
	source, err := manifest.Build(t.Context(), manifest.Sources{HTTP: router})
	if err != nil {
		t.Fatal(err)
	}
	return source
}

const detailedErrorTypes = `
function checkError(error: unknown) {
 if (isOperationError("registryCreate", error)) {
  const declared: Operations["registryCreate"]["error"] = error;
  // @ts-expect-error A plain variant has no details.
  error.response.details;
  if (error.code === ErrorCode.RegistryExists) {
   const id: RegistryID = error.response.details.existing_registry_id;
   const count: number = error.response.details.count;
   const revision: string = error.response.details.revision;
   const status: 409 = error.status;
   const code: "registry_exists" = error.response.error_code;
   const state: "active" | UnknownEnumValue = error.response.details.state;
  }
  if (error.code === ErrorCode.RegistryClosed) {
   // @ts-expect-error Plain declarations do not acquire details.
   error.response.details;
  }
 }
 // @ts-expect-error A caught value needs narrowing before reading details.
 error.response.details;
 // @ts-expect-error Guards accept only this entry's operation names.
 isOperationError("missingOperation", error);
}
`

const detailedErrorRuntime = `
const details = {existing_registry_id:"11111111-1111-4111-8111-111111111111",state:"active",count:3,revision:9007199254740991,nested:{label:"existing"}};
const envelope = {status:409,error_code:"registry_exists",message:"Registry already exists.",details};
let wire = JSON.stringify(envelope), status = 409, closes = 0;
const transport = async () => ({status,headers:{"content-type":"application/json"},body:wire,close(){closes++;}});
const client = sdk.createClient(transport);
const caught = async (api=client, name="registryCreate") => { try { await api[name]({}); assert.fail("expected failure"); } catch(error) { return error; } };
const healthy = async () => {status=204; wire=""; assert.equal(await client.registryCreate({}),undefined); status=409; wire=JSON.stringify(envelope);};
let error = await caught();
assert(error instanceof sdk.APIError);
assert(Object.isFrozen(error)); assert(Object.isFrozen(error.response.details));
assert.throws(()=>{error.code="registry_closed";},TypeError);
assert(sdk.isOperationError("registryCreate",error));
assert(!sdk.isOperationError("seatReserve",error));
assert.equal(error.response.details.existing_registry_id,details.existing_registry_id);
assert.equal(error.response.details.count,3);
assert.equal(error.response.details.revision,"9007199254740991");
assert.equal(error.response.details.nested.label,"existing");
await healthy();
wire=JSON.stringify({...envelope,details:{...details,state:"future"}});
error=await caught();
assert(error.response.details.state instanceof sdk.UnknownEnumValue);
assert(sdk.isOperationError("registryCreate",error));
const publicError = new sdk.APIError(409,"registry_exists",error.response);
assert(sdk.isOperationError("registryCreate",publicError));
assert(!sdk.isOperationError("seatReserve",publicError));
await healthy();
for (const broken of [undefined,{},null,{...details,count:"3"},{...details,existing_registry_id:"not-an-id"},{...details,nested:{label:2}}]) {
 wire=JSON.stringify({...envelope,details:broken});
 assert(await caught() instanceof sdk.ResponseContractError);
 await healthy();
}
wire=JSON.stringify({...envelope,status:410}); assert(await caught() instanceof sdk.ResponseContractError); await healthy();
wire=JSON.stringify({status:409,error_code:"seat_unavailable",message:"No seat",details:{seat:"A"}});
assert(await caught() instanceof sdk.ResponseContractError);
const other=await caught(client,"seatReserve");
assert(sdk.isOperationError("seatReserve",other)); assert(!sdk.isOperationError("registryCreate",other)); await healthy();
const received={...envelope,status:"409",details:{...details,revision:"9007199254740991"}};
assert(sdk.isOperationError("registryCreate",new sdk.APIError(409,"registry_exists",received)));
for (const forged of [new sdk.APIError(410,"registry_exists",received),new sdk.APIError(409,"registry_exists",{...received,details:{}}),new sdk.APIError(409,"registry_closed",received),new sdk.APIError(409,"registry_exists",{...received,details:{...received.details,state:"future"}}),{status:409,code:"registry_exists",response:received}]) {
 assert(!sdk.isOperationError("registryCreate",forged));
}
wire=JSON.stringify({...envelope,error_code:"registry_closed"});
error=await caught(); assert(sdk.isOperationError("registryCreate",error)); assert(!Object.hasOwn(error.response,"details"));
assert(await caught(sdk.createClient(transport,{strictResponses:true})) instanceof sdk.ResponseContractError); await healthy();
wire=JSON.stringify({...envelope,details:{...details,state:"future"}});
assert(await caught(sdk.createClient(transport,{strictResponses:true})) instanceof sdk.ResponseContractError); await healthy();
const form=sdk.createForm(sdk.operation("registryCreate"),{});
wire=JSON.stringify({...envelope,details:{...details,state:"future"}});
const result=await form.submit(client);
assert.equal(result.status,"failed"); assert.equal(result.outcome,"error_response");
assert(sdk.isOperationError("registryCreate",result.error)); assert.equal(form.getSnapshot().issues[0].code,"registry_exists");
const wrapped=sdk.createClient(async()=>{throw publicError;});
const wrappedResult=await form.submit(wrapped);
assert.equal(wrappedResult.error,publicError); assert.equal(form.getSnapshot().issues[0].code,"registry_exists");
wire=JSON.stringify({status:422,error_code:"validation_failed",message:"Invalid",issues:[{path:"/body/name",code:"required",message:"Required"}]}); status=422;
const invalid=await form.submit(client); assert.equal(invalid.status,"failed"); assert.equal(form.getSnapshot().issues[0].path,"/body/name");
await healthy(); const success=await form.submit(sdk.createClient(async()=>({status:204,headers:{},body:"",close(){}}))); assert.equal(success.status,"succeeded"); form.dispose();
assert(closes>10);
`

func TestDetailedOperationErrors(t *testing.T) {
	source := detailedClientManifest(t)
	output, err := Render(source)
	if err != nil {
		t.Fatal(err)
	}
	again, err := Render(source)
	if err != nil || !bytes.Equal(output, again) {
		t.Fatal("non-deterministic output", err)
	}
	for _, want := range []string{`readonly error: (APIError &`, `readonly code: "registry_exists"`, `readonly details: RegistryErrorDetailsReceived`, `export function isOperationError`, `\"details_type\":\"github.com/weiloon1234/Foundry-Go/typescript.RegistryErrorDetails\"`} {
		if !bytes.Contains(output, []byte(want)) {
			t.Fatalf("missing %s", want)
		}
	}
	runReleaseTypeScript(t, append(output, []byte(detailedErrorTypes)...), detailedErrorRuntime)
}

func TestDetailedOperationErrorModules(t *testing.T) {
	source := detailedClientManifest(t)
	outputs, err := renderModules(source, "api", []Surface{{Name: "registry", Routes: []foundryhttp.RouteID{"registry"}}})
	if err != nil {
		t.Fatal(err)
	}
	portal := string(outputs["api_registry_foundry.gen.ts"])
	for _, excluded := range []string{"SeatErrorDetails", "seatReserve", "seat_unavailable", "SeatUnavailable"} {
		if strings.Contains(portal, excluded) {
			t.Fatal("portal leaked", excluded)
		}
	}
	for _, want := range []string{"RegistryErrorReference", "RegistryErrorDetailsReceived", "matchesOperationError", "export function isOperationError", `readonly error: (APIError &`} {
		if !strings.Contains(portal, want) {
			t.Fatal("portal lost", want)
		}
	}
	outputs["input.ts"] = []byte(`export * from "./api_foundry.gen.js";
import * as full from "./api_foundry.gen.js";
import * as portal from "./api_registry_foundry.gen.js";
export {portal};
import {APIError, isOperationError, ErrorCode, UnknownEnumValue} from "./api_registry_foundry.gen.js";
import type {Operations, RegistryID} from "./api_registry_foundry.gen.js";
` + detailedErrorTypes + `
function checkPortal(error: unknown) {
 // @ts-expect-error This operation belongs to a different portal.
 portal.isOperationError("seatReserve",error);
 // @ts-expect-error Projected error codes exclude the other operation's code.
 portal.ErrorCode.SeatUnavailable;
 if (full.isOperationError("registryCreate",error) && error.code===full.ErrorCode.RegistryExists) {
  const id: full.RegistryID = error.response.details.existing_registry_id;
 }
}
`)
	node, compiler := os.Getenv("FOUNDRY_TEST_NODE"), os.Getenv("FOUNDRY_TEST_TYPESCRIPT")
	if node == "" || compiler == "" {
		t.Skip("set FOUNDRY_TEST_NODE and FOUNDRY_TEST_TYPESCRIPT")
	}
	runReleaseTypeScriptFiles(t, node, compiler, outputs, detailedErrorRuntime+`
wire=JSON.stringify({...envelope,details:{...details,state:"future"}});
const shared=await caught(); assert(shared instanceof sdk.portal.APIError); assert(sdk.portal.isOperationError("registryCreate",shared));
assert(!sdk.portal.isOperationError("seatReserve",other));
const portalClient=sdk.portal.createClient(transport);
const portalError=await caught(portalClient);
assert(sdk.portal.isOperationError("registryCreate",portalError)); assert(sdk.isOperationError("registryCreate",portalError));
`)
}

func TestDetailedErrorEnvelopeBudget(t *testing.T) {
	output, err := Render(detailedClientManifest(t))
	if err != nil {
		t.Fatal(err)
	}
	// Each client uses the same generated operation, with one response limit varied.
	output = append(output, []byte(`
export function boundedClient(transport: HTTPTransport, limits: Partial<JSONLimits>) {
 const document=loadRuntimeDocument();
 const engine=createHTTPInvoker({...document,http:document.http.map(operation=>({...operation,limits:{...operation.limits,Response:{...operation.limits.Response,...limits}}}))},transport,{});
 return {registryCreate:()=>engine.invoke("registryCreate",{})};
}
`)...)
	runReleaseTypeScript(t, output, `
const body='{"status":409,"error_code":"registry_exists","message":"Exists","details":{"existing_registry_id":"11111111-1111-4111-8111-111111111111","state":"active","count":3,"revision":9007199254740993,"nested":{"label":"existing"}}}';
const nodes=value=>1+(Array.isArray(value)?value.reduce((total,item)=>total+nodes(item),0):value!==null&&typeof value==="object"?Object.entries(value).reduce((total,[key,item])=>total+1+nodes(item),0):0);
let status=409,closed=0;
const transport=async()=>({status,headers:{"content-type":"application/json"},body:status===409?body:"",close(){closed++;}});
const exact={Bytes:new TextEncoder().encode(body).length,Depth:3,Nodes:nodes(JSON.parse(body))};
const client=sdk.boundedClient(transport,exact);
await assert.rejects(client.registryCreate(),error=>sdk.isOperationError("registryCreate",error)&&error.response.details.revision==="9007199254740993");
for (const limit of [{Bytes:exact.Bytes-1},{Depth:2},{Nodes:exact.Nodes-1},{Steps:12}]) {
 const bounded=sdk.boundedClient(transport,{...exact,...limit});
 await assert.rejects(bounded.registryCreate(),sdk.ResponseContractError);
 status=204; assert.equal(await bounded.registryCreate(),undefined); status=409;
}
assert.equal(closed,9);
`)
}

func TestDetailFreeOperationShape(t *testing.T) {
	source, err := manifest.Build(t.Context(), manifest.Sources{})
	if err != nil {
		t.Fatal(err)
	}
	doc, err := source.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	names, err := newNaming(doc)
	if err != nil {
		t.Fatal(err)
	}
	doc.HTTP = []manifest.Operation{{Name: "plain", Errors: []foundryhttp.ErrorCode{foundryhttp.Conflict}}}
	r := renderer{document: doc, naming: names}
	if err := r.http(); err != nil {
		t.Fatal(err)
	}
	text := r.out.String()
	// The entry's existing public declarations remain unchanged without details.
	start := strings.Index(text, "export interface Operations")
	const want = `export interface Operations {
  readonly "plain": { readonly request: Readonly<Record<string, never>>; readonly response: void; readonly errorCode: "conflict" };
}
export interface API {
  "plain"(request: Operations["plain"]["request"], options?: CallOptions): Promise<Operations["plain"]["response"]>;
}
export type Client = API;
export function createClient(transport: HTTPTransport, options: ClientOptions = {}): API {
  const engine = createHTTPInvoker(runtimeDocument(), transport, options);
  return Object.freeze({
    ["plain"]: (request: Operations["plain"]["request"], options?: CallOptions) => engine.invoke("plain", request, options) as Promise<Operations["plain"]["response"]>,
  });
}
export function validateRequest<K extends keyof Operations>(operation: K, input: Operations[K]["request"], options: ClientOptions = {}): ValidationReport {
  return createHTTPInvoker(runtimeDocument(), async () => { throw new Error("Validation cannot send requests"); }, options).validate(operation, input);
}
`
	if start < 0 || text[start:] != want {
		t.Fatalf("detail-free HTTP declarations changed:\n%s", text)
	}
}
