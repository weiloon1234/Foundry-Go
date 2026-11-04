package typescript

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/contract/manifest"
	"github.com/weiloon1234/Foundry-Go/enum"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/value"
)

type RegistryErrorDetails struct {
	ExistingRegistryID string                       `json:"existing_registry_id"`
	State              RegistryErrorState           `json:"state"`
	Count              int32                        `json:"count"`
	Revision           int64                        `json:"revision"`
	Nested             RegistryErrorReference       `json:"nested"`
	Quoted             int32                        `json:"quoted,string"`
	Float              float64                      `json:"float"`
	Decimal            json.Number                  `json:"decimal"`
	Choice             RegistryErrorChoice          `json:"choice"`
	Pair               [2]int32                     `json:"pair"`
	Map                map[string]int32             `json:"map"`
	EnumMap            map[RegistryErrorState]int32 `json:"em"`
	Dynamic            any                          `json:"dynamic"`
}
type RegistryErrorState string

type RegistryErrorReference struct {
	Label string `json:"label"`
}
type RegistryErrorChoice struct {
	Kind  string `json:"kind"`
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
				{Name: "quoted", Type: "details.Quoted", Required: true},
				{Name: "float", Type: "details.Float", Required: true},
				{Name: "decimal", Type: "details.Decimal", Required: true},
				{Name: "choice", Type: "details.Choice", Required: true},
				{Name: "pair", Type: "details.Pair", Required: true},
				{Name: "map", Type: "details.Map", Required: true},
				{Name: "em", Type: "details.EnumMap", Required: true},
				{Name: "dynamic", Type: "details.Dynamic", Required: true},
			}},
			{ID: "details.RegistryErrorReference", Kind: contract.ObjectKind, Properties: []contract.Property{{Name: "label", Type: "string", Required: true}}},
			{ID: "details.Quoted", Kind: contract.QuotedKind, Element: "int32"},
			{ID: "details.Float", Kind: contract.NumberKind, Bits: 64},
			{ID: "details.Decimal", Kind: contract.NumberKind},
			{ID: "details.Choice", Kind: contract.UnionKind, Discriminator: "kind", Variants: []contract.Variant{{Tag: "existing", Type: "details.RegistryErrorReference"}}},
			{ID: "details.Pair", Kind: contract.ArrayKind, Element: "int32", Length: value.Set(2)},
			{ID: "details.Map", Kind: contract.MapKind, Element: "int32"},
			contract.JSONMapType[map[RegistryErrorState]int32]("details.EnumMap", "int32", "details.RegistryState", contract.EnumJSONKey(enum.Describe("details", "RegistryState", enum.Case[RegistryErrorState]{Name: "Active", Value: "active"}))),
			{ID: "details.Dynamic", Kind: contract.DynamicKind, Nullable: true},
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
	head := foundryhttp.DefineEndpoint(foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "registry.head", Method: foundryhttp.HEAD, Access: foundryhttp.Public}, foundryhttp.StaticPath("/registry.head")), foundryhttp.EmptyQuery(), foundryhttp.EmptyBody(), foundryhttp.EmptyResponse(204)).Handle(func(context.Context, foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, foundryhttp.NoBody]) (foundryhttp.NoContent, error) {
		return foundryhttp.NoContent{}, nil
	})
	router, err := foundryhttp.NewRouter(route("registry.create", registry, foundryhttp.DefineError("registry_closed", 409, "Registry closed.")), route("seat.reserve", seat), head)
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
   const quoted: number = error.response.details.quoted;
   const float: number = error.response.details.float;
   const decimal: string = error.response.details.decimal;
   const choice: "existing" = error.response.details.choice.kind;
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

const detailedErrorEnvelope = `
const details = {existing_registry_id:"11111111-1111-4111-8111-111111111111",state:"active",count:3,revision:9007199254740991,nested:{label:"existing"},quoted:"7",float:1.25,decimal:123.45,choice:{kind:"existing",label:"selected"},pair:[1,2],map:{first:3},em:{active:4},dynamic:{precise:9007199254740991}};
const envelope = {status:409,error_code:"registry_exists",message:"Registry already exists.",details};
`

const detailedErrorRuntime = detailedErrorEnvelope + `
let wire = JSON.stringify(envelope), status = 409, closes = 0;
const transport = async () => ({status,headers:{"content-type":"application/json"},body:wire,close(){closes++;}});
const client = sdk.createClient(transport);
const caught = async (api=client, name="registryCreate") => { try { await api[name]({}); assert.fail("expected failure"); } catch(error) { return error; } };
const healthy = async () => {status=204; wire=""; assert.equal(await client.registryCreate({}),undefined); status=409; wire=JSON.stringify(envelope);};
let error = await caught();
assert(error instanceof sdk.APIError);
error.attemptNumber=1; error.cause=new Error("retry cause"); Error.captureStackTrace(error);
assert.equal(error.attemptNumber,1); assert.equal(error.cause.message,"retry cause"); assert.equal(typeof error.stack,"string");
assert(!Object.isFrozen(error)); assert(!Object.isFrozen(error.response)); assert(!Object.isFrozen(error.response.details));
assert(sdk.isOperationError("registryCreate",error));
assert(!sdk.isOperationError("seatReserve",error));
assert.equal(error.response.details.existing_registry_id,details.existing_registry_id);
assert.equal(error.response.details.count,3);
assert.equal(error.response.details.revision,"9007199254740991");
assert.equal(error.response.details.nested.label,"existing");
assert.equal(error.response.details.quoted,7); assert.equal(error.response.details.float,1.25); assert.equal(error.response.details.decimal,"123.45");
assert.equal(error.response.details.choice.kind,"existing"); assert.equal(error.response.details.choice.label,"selected");
assert.deepEqual(error.response.details.pair,[1,2]); assert.equal(error.response.details.map.first,3); assert.equal(error.response.details.em.active,4);
assert(error.response.details.dynamic.precise instanceof sdk.JSONNumber);
const received=error.response;
error.response.details.count="wrong"; assert(!sdk.isOperationError("registryCreate",error)); error.response.details.count=3;
await healthy();
wire=JSON.stringify({...envelope,details:{...details,state:"future"}}).replaceAll("9007199254740991","9007199254740993");
error=await caught();
assert.equal(error.response.details.revision,"9007199254740993");
assert.equal(error.response.details.dynamic.precise.text,"9007199254740993");
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
for (const malformed of [{},{status:{}},{status:"409"},{status:null},{status:true}]) {
 const body={...envelope,...malformed}; if (!Object.hasOwn(malformed,"status")) delete body.status;
 wire=JSON.stringify(body); const failed=await caught();
 assert(failed instanceof sdk.ResponseContractError); assert.equal(failed.issues[0].path,"/status");
 assert.equal(failed.issues[0].code,!Object.hasOwn(malformed,"status")?"required":malformed.status===null?"null":"value");
 await healthy();
}
wire=JSON.stringify({...envelope,status:410}); assert(await caught() instanceof sdk.ResponseContractError); await healthy();
wire=JSON.stringify({status:409,error_code:"seat_unavailable",message:"No seat",details:{seat:"A"}});
assert(await caught() instanceof sdk.ResponseContractError);
const other=await caught(client,"seatReserve");
assert(sdk.isOperationError("seatReserve",other)); assert(!sdk.isOperationError("registryCreate",other)); await healthy();
assert(sdk.isOperationError("registryCreate",new sdk.APIError(409,"registry_exists",received)));
for (const forged of [new sdk.APIError(410,"registry_exists",received),new sdk.APIError(409,"registry_exists",{...received,details:{}}),new sdk.APIError(409,"registry_closed",received),new sdk.APIError(409,"registry_exists",{...received,details:{...received.details,state:"future"}}),{status:409,code:"registry_exists",response:received}]) {
 assert(!sdk.isOperationError("registryCreate",forged));
}
for (const changed of [{quoted:"7"},{float:"1.25"},{decimal:123.45},{pair:new Array(2)},{em:{future:{evil:true}}},{count:new sdk.UnknownEnumValue("details.RegistryState","future")},{state:new sdk.UnknownEnumValue("string","future")}]) {
 assert(!sdk.isOperationError("registryCreate",new sdk.APIError(409,"registry_exists",{...received,details:{...received.details,...changed}})));
}
assert(!sdk.isOperationError("registryHead",new sdk.APIError(409,"conflict",{status:"409",error_code:"conflict",message:"Conflict"})));
wire=JSON.stringify({...envelope,details:{...details,nested:{label:"existing",details:{secret:"hidden"}}}});
const nested=await caught(sdk.createClient(transport,{strictResponses:true}));
assert(nested instanceof sdk.ResponseContractError); assert.equal(nested.issues[0].path,"/details/nested/details"); assert.equal(nested.issues[0].code,"unknown");
wire=JSON.stringify({...envelope,details:{...details,em:{future:{evil:true}}}}); assert(await caught() instanceof sdk.ResponseContractError); await healthy();
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
for (const candidate of [
 new sdk.APIError(409,"registry_exists",{...received,issues:[{path:"/body/name",code:"name_taken",secret:"zz"}]}),
 new sdk.APIError(409,"name_taken",{status:"409",error_code:"name_taken",message:"Taken",issues:[{path:"/body/name",code:"name_taken",secret:"zz"}]}),
 new sdk.APIError(409,"seat_unavailable",{status:"409",error_code:"seat_unavailable",message:"Taken",issues:[{path:"/body/name",code:"name_taken",secret:"zz"}]})
]) {
 await form.submit({registryCreate:async()=>{throw candidate;}});
 assert.deepEqual(form.getSnapshot().issues.map(issue=>({...issue})),[{path:"/body/name",code:"name_taken"}]);
 assert(!JSON.stringify(form.getSnapshot().issues).includes("zz"));
}
for (const candidate of [other,new sdk.APIError(409,"name_taken",{...received,error_code:"name_taken"}),new sdk.APIError(409,"registry_exists",{...received,details:{}}),new sdk.APIError(409,"garbage",{status:{},issues:[{path:1,code:false}]})]) {
 await form.submit({registryCreate:async()=>{throw candidate;}});
 assert.deepEqual(form.getSnapshot().issues.map(issue=>({...issue})),[{path:"",code:"form_request"}]);
}
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
	for _, want := range []string{`readonly error: OperationErrorVariants[`, `readonly code: "registry_exists"`, `readonly details: RegistryErrorDetailsReceived`, `export function isOperationError`, `\"details_type\":\"github.com/weiloon1234/Foundry-Go/typescript.RegistryErrorDetails\"`} {
		if !bytes.Contains(output, []byte(want)) {
			t.Fatalf("missing %s", want)
		}
	}
	runReleaseTypeScript(t, append(output, []byte(detailedErrorTypes)...), detailedErrorRuntime)
}

func TestDetailedOperationErrorModules(t *testing.T) {
	source := detailedClientManifest(t)
	outputs, err := renderModules(source, "api", []Surface{{Name: "registry", Routes: []foundryhttp.RouteID{"registry"}}, {Name: "plain", Routes: []foundryhttp.RouteID{"registry.head"}}})
	if err != nil {
		t.Fatal(err)
	}
	for _, excluded := range []string{"OperationErrorVariants", "isOperationError", "RegistryErrorDetails"} {
		if bytes.Contains(outputs["api_plain_foundry.gen.ts"], []byte(excluded)) {
			t.Fatal("plain portal acquired", excluded)
		}
	}
	portal := string(outputs["api_registry_foundry.gen.ts"])
	for _, excluded := range []string{"SeatErrorDetails", "seatReserve", "seat_unavailable", "SeatUnavailable"} {
		if strings.Contains(portal, excluded) {
			t.Fatal("portal leaked", excluded)
		}
	}
	for _, want := range []string{"RegistryErrorReference", "RegistryErrorDetailsReceived", "matchesOperationError", "export function isOperationError", `readonly error: OperationErrorVariants[`} {
		if !strings.Contains(portal, want) {
			t.Fatal("portal lost", want)
		}
	}
	outputs["input.ts"] = []byte(`export * from "./api_foundry.gen.js";
import * as full from "./api_foundry.gen.js";
import * as portal from "./api_registry_foundry.gen.js";
import * as plain from "./api_plain_foundry.gen.js";
export {portal};
// @ts-expect-error Detail-free portals do not export the guard.
plain.isOperationError;
// @ts-expect-error Detail-free portals have no operation error variants.
type PlainVariant = plain.OperationErrorVariants;
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
	runReleaseTypeScript(t, output, detailedErrorEnvelope+`
const body=JSON.stringify(envelope).replaceAll("9007199254740991","9007199254740993");
const nodes=value=>1+(Array.isArray(value)?value.reduce((total,item)=>total+nodes(item),0):value!==null&&typeof value==="object"?Object.entries(value).reduce((total,[key,item])=>total+1+nodes(item),0):0);
let status=409,closed=0;
const transport=async()=>({status,headers:{"content-type":"application/json"},body:status===409?body:"",close(){closed++;}});
// Envelope: 15 visits/property checks; details and their children: 56.
const exact={Bytes:new TextEncoder().encode(body).length,Depth:3,Nodes:nodes(JSON.parse(body)),Steps:71};
const client=sdk.boundedClient(transport,exact);
await assert.rejects(client.registryCreate(),error=>sdk.isOperationError("registryCreate",error)&&error.response.details.revision==="9007199254740993");
for (const limit of [{Bytes:exact.Bytes-1},{Depth:2},{Nodes:exact.Nodes-1},{Steps:exact.Steps-1}]) {
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
	if strings.Contains(text, "OperationErrorVariants") || strings.Contains(text, "isOperationError") {
		t.Fatal("detail-free entry acquired detailed error declarations")
	}
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

func TestOperationErrorVariantsSharedAcrossOperations(t *testing.T) {
	source := detailedClientManifest(t)
	doc, err := source.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	operation := doc.HTTP[0]
	for i := range 1000 {
		copy := operation
		copy.Name = fmt.Sprintf("create%d", i)
		copy.Route.ID = foundryhttp.RouteID(fmt.Sprintf("create.%d", i))
		copy.Route.Path = fmt.Sprintf("/create/%d", i)
		doc.HTTP = append(doc.HTTP, copy)
	}
	names, err := newNaming(doc)
	if err != nil {
		t.Fatal(err)
	}
	r := renderer{document: doc, naming: names}
	if err := r.http(); err != nil {
		t.Fatal(err)
	}
	output := r.out.String()
	if strings.Count(output, "readonly details:") != 2 || strings.Count(output, "readonly error: OperationErrorVariants[") != len(doc.HTTP) {
		t.Fatal("operation count duplicates variants")
	}
}
