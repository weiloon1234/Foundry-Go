package typescript_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/contract/manifest"
	"github.com/weiloon1234/Foundry-Go/enum"
	"github.com/weiloon1234/Foundry-Go/fault"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/i18n"
	"github.com/weiloon1234/Foundry-Go/openapi"
	"github.com/weiloon1234/Foundry-Go/typescript"
	"github.com/weiloon1234/Foundry-Go/validation"
)

func clientManifest(t *testing.T) *manifest.Manifest {
	t.Helper()
	extra := contract.Schema{Root: "consumer.Payload", Types: []contract.Type{
		{ID: "consumer.Payload", Kind: contract.ObjectKind, Properties: []contract.Property{{Name: "required_nullable", Type: "consumer.Nullable", Required: true}, {Name: "optional", Type: "string"}, {Name: "wide", Type: "consumer.Integer", Required: true}, {Name: "quoted", Type: "consumer.Quoted", Required: true}}},
		{ID: "string", Kind: contract.StringKind}, {ID: "consumer.Nullable", Kind: contract.AliasKind, Element: "string", Nullable: true}, {ID: "consumer.Integer", Kind: contract.IntegerKind, Bits: 64, Signed: true}, {ID: "consumer.Quoted", Kind: contract.QuotedKind, Element: "consumer.Integer"},
	}}
	endpoint := foundryhttp.DefineEndpoint(foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "health.read", Method: foundryhttp.GET, Access: foundryhttp.Public}, foundryhttp.StaticPath("/health")), foundryhttp.EmptyQuery(), foundryhttp.EmptyBody(), foundryhttp.JSONResponse(201, contract.StringJSON[string]()))
	router, err := foundryhttp.NewRouter(endpoint.Handle(func(context.Context, foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, foundryhttp.NoBody]) (string, error) {
		t.Fatal("export ran a handler")
		return "", nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	source, err := manifest.Build(t.Context(), manifest.Sources{HTTP: router, Schemas: []contract.Schema{extra}})
	if err != nil {
		t.Fatal(err)
	}
	return source
}

func TestRenderOwnsStableTypedClientAndExactManifest(t *testing.T) {
	source := clientManifest(t)
	first, err := typescript.Render(source)
	if err != nil {
		t.Fatal(err)
	}
	second, err := typescript.Render(source)
	if err != nil || !bytes.Equal(first, second) {
		t.Fatal("unstable SDK", err)
	}
	for _, expected := range []string{`readonly "required_nullable":`, `readonly "optional"?:`, `"healthRead"`, `new JSONNumber`, `Promise<Operations["healthRead"]["response"]>`, `"consumer.Payload"`, `export function contractMetadata`, `export class ContractError`} {
		if !strings.Contains(string(first), expected) {
			t.Fatal("missing generated contract", expected)
		}
	}
	first[0] = '!'
	third, err := typescript.Render(source)
	if err != nil || !bytes.Equal(second, third) {
		t.Fatal("caller mutation changed renderer", err)
	}
	if _, err := typescript.Render(nil); err == nil {
		t.Fatal("nil manifest accepted")
	}
}

func TestClientGenerationPublishesAllAdaptersTogether(t *testing.T) {
	source := clientManifest(t)
	options := typescript.Options{Dir: t.TempDir(), Prefix: "api", OpenAPI: openapi.Options{Title: "Consumer", APIVersion: "1"}}
	// The full entry, its two shared runtime modules, the manifest and OpenAPI.
	report, err := typescript.Generate(t.Context(), source, options)
	if err != nil || len(report.Written) != 5 {
		t.Fatal(report, err)
	}
	options.Check = true
	report, err = typescript.Generate(t.Context(), source, options)
	if err != nil || len(report.Written) != 0 {
		t.Fatal(report, err)
	}
	options.Prefix = "next"
	if _, err := typescript.Generate(t.Context(), source, options); err == nil {
		t.Fatal("rename accepted by check")
	}
	options.Check = false
	report, err = typescript.Generate(t.Context(), source, options)
	if err != nil || len(report.Written) != 5 || len(report.Removed) != 5 {
		t.Fatal(report, err)
	}
	options.Prefix = "../bad"
	if _, err := typescript.Generate(t.Context(), source, options); err == nil {
		t.Fatal("unsafe prefix accepted")
	}
}

func TestOptionalFormAdaptersUseOwnedAtomicPublication(t *testing.T) {
	source := clientManifest(t)
	options := typescript.Options{Dir: t.TempDir(), Prefix: "example", OpenAPI: openapi.Options{Title: "Consumer", APIVersion: "1"}, React: true, Vue: true}
	report, err := typescript.Generate(t.Context(), source, options)
	if err != nil || len(report.Written) != 7 {
		t.Fatal(report, err)
	}
	for _, adapter := range []string{"react", "vue"} {
		data, err := os.ReadFile(filepath.Join(options.Dir, "example_"+adapter+"_foundry.gen.ts"))
		if err != nil || !bytes.Contains(data, []byte(`"./example_foundry.gen.js"`)) {
			t.Fatal("adapter prefix/import mismatch", err)
		}
	}
	core, err := os.ReadFile(filepath.Join(options.Dir, "example_foundry.gen.ts"))
	if err != nil || bytes.Contains(core, []byte(`from "react"`)) || bytes.Contains(core, []byte(`from "vue"`)) {
		t.Fatal("core imported optional UI dependencies", err)
	}
	options.Check = true
	if _, err := typescript.Generate(t.Context(), source, options); err != nil {
		t.Fatal(err)
	}
	options.React = false
	if _, err := typescript.Generate(t.Context(), source, options); err == nil {
		t.Fatal("check ignored obsolete adapter")
	}
	options.Check = false
	report, err = typescript.Generate(t.Context(), source, options)
	if err != nil || len(report.Removed) != 1 || report.Removed[0] != "example_react_foundry.gen.ts" {
		t.Fatal(report, err)
	}
}

func TestRenderMapWithoutKeyConstraint(t *testing.T) {
	schema := contract.Schema{Root: "example.Labels", Types: []contract.Type{
		{ID: "example.Labels", Kind: contract.MapKind, Element: "string", Nullable: true},
		{ID: "string", Kind: contract.StringKind},
	}}
	source, err := manifest.Build(t.Context(), manifest.Sources{Schemas: []contract.Schema{schema}})
	if err != nil {
		t.Fatal(err)
	}
	data, err := source.JSON()
	if err != nil {
		t.Fatal(err)
	}
	restored, err := manifest.Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	for _, current := range []*manifest.Manifest{source, restored} {
		if _, err := typescript.Render(current); err != nil {
			t.Fatal(err)
		}
		if _, err := openapi.Render(current, openapi.Options{Title: "Maps", APIVersion: "1"}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestSchemaNamesAvoidRuntimeDeclarations(t *testing.T) {
	colliding := contract.Schema{Root: "consumer/api.Payload", Types: []contract.Type{
		{ID: "consumer/api.Payload", Kind: contract.ObjectKind, Properties: []contract.Property{{Name: "map", Type: "consumer/api.Map", Required: true}}},
		{ID: "consumer/api.Map", Kind: contract.StringKind},
		{ID: "consumer/api.OperationInputs", Kind: contract.StringKind},
		{ID: "consumer/api.FieldDescriptor", Kind: contract.StringKind},
	}}
	source, err := manifest.Build(t.Context(), manifest.Sources{Schemas: []contract.Schema{colliding}})
	if err != nil {
		t.Fatal(err)
	}
	sdk, err := typescript.Render(source)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"export type Api_Payload = ", "export type Api_Map = string;", `readonly "consumer/api.Payload": Api_Payload;`, "export type Api_OperationInputs = string;", "export type Api_FieldDescriptor = string;"} {
		if !strings.Contains(string(sdk), want) {
			t.Fatal("reserved runtime name was not qualified", want)
		}
	}
}

func TestRenderTypesStatusesRedirectsAndRawBodies(t *testing.T) {
	route := func(id foundryhttp.RouteID, method foundryhttp.Method, path string) foundryhttp.Route[foundryhttp.NoPath] {
		return foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: id, Method: method, Access: foundryhttp.Public}, foundryhttp.StaticPath(path))
	}
	type none = foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, foundryhttp.NoBody]
	upsert := foundryhttp.DefineEndpoint(route("items.upsert", foundryhttp.PUT, "/upsert"), foundryhttp.EmptyQuery(), foundryhttp.EmptyBody(), foundryhttp.JSONResponses(contract.StringJSON[string](), 200, 201))
	moved := foundryhttp.DefineEndpoint(route("session.continue", foundryhttp.POST, "/continue"), foundryhttp.EmptyQuery(), foundryhttp.EmptyBody(), foundryhttp.RedirectResponse(303))
	events := foundryhttp.DefineEndpoint(route("items.events", foundryhttp.GET, "/events"), foundryhttp.EmptyQuery(), foundryhttp.EmptyBody(), foundryhttp.EventStreamResponse(contract.StringJSON[string]()))
	raw := foundryhttp.DefineEndpoint(route("files.raw", foundryhttp.POST, "/raw"), foundryhttp.EmptyQuery(), foundryhttp.RawRequestBody("application/octet-stream"), foundryhttp.JSONResponse(201, contract.StringJSON[string]()))
	router, err := foundryhttp.NewRouter(
		upsert.Handle(func(context.Context, none) (foundryhttp.Statused[string], error) {
			return foundryhttp.Statused[string]{}, nil
		}),
		moved.Handle(func(context.Context, none) (foundryhttp.Redirect, error) { return foundryhttp.RedirectTo("/"), nil }),
		events.Handle(func(context.Context, none) (foundryhttp.Events[string], error) {
			return foundryhttp.EventsFrom(func(context.Context, *foundryhttp.EventSink[string]) error { return nil }), nil
		}),
		raw.Handle(func(context.Context, foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, foundryhttp.RawBody]) (string, error) {
			return "", nil
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	source, err := manifest.Build(t.Context(), manifest.Sources{HTTP: router})
	if err != nil {
		t.Fatal(err)
	}
	output, err := typescript.Render(source)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{`readonly response: StatusResult<200 | 201, `, `readonly response: RedirectResult<303>`, `readonly response: EventStreamResult<`, `readonly body: { readonly data: RawData; readonly mediaType?: "application/octet-stream" }`} {
		if !strings.Contains(string(output), expected) {
			t.Fatal("missing generated contract", expected)
		}
	}
}

func TestRenderEnumDeclarationMerging(t *testing.T) {
	statusDesc := enum.Describe("app", "Status",
		enum.Case[string]{Name: "Draft", Value: "draft", LabelKey: "status.draft"},
		enum.Case[string]{Name: "Ready", Value: "ready", LabelKey: "status.ready"},
	)
	statusDef, err := statusDesc.Definition()
	if err != nil {
		t.Fatal(err)
	}
	roleDesc := enum.Describe("app", "Role",
		enum.Case[string]{Name: "Admin", Value: "admin", LabelKey: "role.admin"},
		enum.Case[string]{Name: "User", Value: "user"},
	)
	roleDef, err := roleDesc.Definition()
	if err != nil {
		t.Fatal(err)
	}
	priorityDesc := enum.Describe("app", "Priority",
		enum.Case[string]{Name: "Low", Value: "low"},
		enum.Case[string]{Name: "High", Value: "high"},
	)
	priorityDef, err := priorityDesc.Definition()
	if err != nil {
		t.Fatal(err)
	}
	wideDesc := enum.Describe("app", "Wide",
		enum.Case[int64]{Name: "First", Value: 9007199254740993, LabelKey: "wide.first"},
		enum.Case[int64]{Name: "Second", Value: 9007199254740994},
	)
	wideDef, err := wideDesc.Definition()
	if err != nil {
		t.Fatal(err)
	}

	schemas := []contract.Schema{
		{Root: "app.Status", Types: []contract.Type{{ID: "app.Status", Kind: contract.StringKind, Cases: []json.RawMessage{json.RawMessage(`"draft"`), json.RawMessage(`"ready"`)}}}},
		{Root: "app.Role", Types: []contract.Type{{ID: "app.Role", Kind: contract.StringKind, Cases: []json.RawMessage{json.RawMessage(`"admin"`), json.RawMessage(`"user"`)}}}},
		{Root: "app.Priority", Types: []contract.Type{{ID: "app.Priority", Kind: contract.StringKind, Cases: []json.RawMessage{json.RawMessage(`"low"`), json.RawMessage(`"high"`)}}}},
		{Root: "app.Wide", Types: []contract.Type{{ID: "app.Wide", Kind: contract.IntegerKind, Bits: 64, Signed: true, Cases: []json.RawMessage{json.RawMessage("9007199254740993"), json.RawMessage("9007199254740994")}}}},
	}

	endpoint := foundryhttp.DefineEndpoint(
		foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "health.read", Method: foundryhttp.GET, Access: foundryhttp.Public}, foundryhttp.StaticPath("/health")),
		foundryhttp.EmptyQuery(), foundryhttp.EmptyBody(), foundryhttp.JSONResponse(200, contract.StringJSON[string]()),
	)
	router, err := foundryhttp.NewRouter(endpoint.Handle(func(context.Context, foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, foundryhttp.NoBody]) (string, error) {
		return "", nil
	}))
	if err != nil {
		t.Fatal(err)
	}

	source, err := manifest.Build(t.Context(), manifest.Sources{
		HTTP:    router,
		Schemas: schemas,
		Enums:   []enum.Definition{statusDef, roleDef, priorityDef, wideDef},
	})
	if err != nil {
		t.Fatal(err)
	}

	outputBytes, err := typescript.Render(source)
	if err != nil {
		t.Fatal(err)
	}
	output := string(outputBytes)

	// Status enum: full labels -> satisfies Record<Status, CatalogMessageKey>
	if !strings.Contains(output, `export type Status = "draft" | "ready";`) {
		t.Error("missing Status type declaration")
	}
	if !strings.Contains(output, `export const Status = /* @__PURE__ */ Object.freeze({
  cases: ["draft", "ready"] as const,
  labels: /* @__PURE__ */ Object.freeze({
    draft: "status.draft",
    ready: "status.ready",
  }) satisfies Record<Status, CatalogMessageKey>,
});`) {
		t.Error("missing or incorrect Status const declaration")
	}

	// Role enum: partial labels -> satisfies Partial<Record<Role, CatalogMessageKey>>
	if !strings.Contains(output, `export const Role = /* @__PURE__ */ Object.freeze({
  cases: ["admin", "user"] as const,
  labels: /* @__PURE__ */ Object.freeze({
    admin: "role.admin",
  }) satisfies Partial<Record<Role, CatalogMessageKey>>,
});`) {
		t.Error("missing or incorrect Role const declaration")
	}

	// Priority enum: no labels -> no labels property
	if !strings.Contains(output, `export const Priority = /* @__PURE__ */ Object.freeze({
  cases: ["low", "high"] as const,
});`) {
		t.Error("missing or incorrect Priority const declaration")
	}

	// Wide integer enum: wide integers are quoted strings in cases and labels
	if !strings.Contains(output, `export type Wide = "9007199254740993" | "9007199254740994";`) {
		t.Error("missing Wide type declaration")
	}
	if !strings.Contains(output, `export const Wide = /* @__PURE__ */ Object.freeze({
  cases: ["9007199254740993", "9007199254740994"] as const,
  labels: /* @__PURE__ */ Object.freeze({
    "9007199254740993": "wide.first",
  }) satisfies Partial<Record<Wide, CatalogMessageKey>>,
});`) {
		t.Error("missing or incorrect Wide const declaration")
	}

	// Generic enumLabel helper exported
	if !strings.Contains(output, "export function enumLabel<V extends string | number, K extends string = string>") {
		t.Error("missing enumLabel helper")
	}

	// schemas object
	if !strings.Contains(output, "export const schemas = /* @__PURE__ */ Object.freeze({") {
		t.Error("missing schemas mapping")
	}
	if !strings.Contains(output, `Status: "app.Status"`) {
		t.Error("missing Status in schemas")
	}

	// newIdentity helper
	if !strings.Contains(output, "export function newIdentity<Owner extends string>") {
		t.Error("missing newIdentity helper")
	}

	// ErrorCode object and type
	if !strings.Contains(output, "export const ErrorCode = /* @__PURE__ */ Object.freeze({") {
		t.Error("missing ErrorCode const")
	}
	if !strings.Contains(output, "export type ErrorCode = (typeof ErrorCode)[keyof typeof ErrorCode];") {
		t.Error("missing ErrorCode type")
	}
	if !strings.Contains(output, `ValidationFailed: "validation_failed"`) {
		t.Error("missing ValidationFailed in ErrorCode")
	}
}

func TestRenderErrorCodeCollisionFails(t *testing.T) {
	source := clientManifest(t)
	doc, err := source.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	doc.Errors = append(doc.Errors,
		foundryhttp.ErrorDefinition{Code: "user_not_found", Status: 404, Message: "User not found."},
		foundryhttp.ErrorDefinition{Code: "user.not-found", Status: 404, Message: "User not found."},
	)
	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	m, err := manifest.Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	_, err = typescript.Render(m)
	if err == nil {
		t.Fatal("expected conflict error on colliding error codes, got nil")
	}
	if !errors.Is(err, fault.Conflict) {
		t.Fatalf("expected fault.Conflict, got %v", err)
	}
}

func TestRenderFileUploadConstraints(t *testing.T) {
	type uploadForm struct {
		Doc foundryhttp.UploadedFile
	}
	docField := validation.DefineField("doc", func(u uploadForm) foundryhttp.UploadedFile { return u.Doc })
	rules := validation.Rules(
		docField,
		validation.FileMaxSize[foundryhttp.UploadedFile](2048),
		validation.FileContentTypes[foundryhttp.UploadedFile]("image/jpeg", "application/pdf"),
		validation.FileExtensions[foundryhttp.UploadedFile]("jpg", "pdf"),
	)
	body := foundryhttp.MultipartDirectory(
		foundryhttp.DefineMultipart(
			foundryhttp.FilePart("doc", func(u *uploadForm) *foundryhttp.UploadedFile { return &u.Doc }),
		),
		t.TempDir(),
	)
	route := foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "doc.upload", Method: foundryhttp.POST, Access: foundryhttp.Public}, foundryhttp.StaticPath("/doc"))
	endpoint := foundryhttp.ValidateBody(foundryhttp.DefineEndpoint(route, foundryhttp.EmptyQuery(), foundryhttp.MultipartBody(body), foundryhttp.JSONResponse(201, contract.StringJSON[string]())), rules)
	router, err := foundryhttp.NewRouter(endpoint.Handle(func(context.Context, foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, uploadForm]) (string, error) {
		return "", nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	source, err := manifest.Build(t.Context(), manifest.Sources{HTTP: router})
	if err != nil {
		t.Fatal(err)
	}
	outputBytes, err := typescript.Render(source)
	if err != nil {
		t.Fatal(err)
	}
	output := string(outputBytes)
	if !strings.Contains(output, "accept") || !strings.Contains(output, "maxBytes") {
		t.Error("expected accept and maxBytes in descriptor implementation")
	}
}

func TestRenderFrontendMessages(t *testing.T) {
	frontendFS := fstest.MapFS{
		"frontend/en/auth.json": {
			Data: []byte(`{
				"auth.welcome": "Welcome back.",
				"auth.locked_minutes": {
					"$plural": {
						"one": "Try again in {{minutes}} minute.",
						"other": "Try again in {{minutes}} minutes."
					}
				}
			}`),
		},
	}
	locSet, err := i18n.NewLocaleSet("en", "en")
	if err != nil {
		t.Fatal(err)
	}
	source, err := manifest.Build(t.Context(), manifest.Sources{Frontend: frontendFS, Locales: locSet})
	if err != nil {
		t.Fatalf("unexpected build error: %v", err)
	}
	outputBytes, err := typescript.Render(source)
	if err != nil {
		t.Fatalf("unexpected render error: %v", err)
	}
	output := string(outputBytes)

	for _, want := range []string{
		"export interface FrontendMessageArguments {",
		`readonly "auth.locked_minutes": { readonly "minutes": number | string };`,
		`readonly "auth.welcome": Readonly<Record<string, never>>;`,
		"export type FrontendMessageKey = keyof FrontendMessageArguments;",
		"function frontendDefinitions(): ReadonlyMap<string, { readonly plural?: string; readonly plural_kind?: \"cardinal\" | \"ordinal\" }>",
		"export function formatFrontendMessage<K extends FrontendMessageKey>(messages: ValidationMessages | undefined, key: K,",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("missing %q in output\nOutput excerpt:\n%s", want, output)
		}
	}
}

func TestRenderPublicConfig(t *testing.T) {
	settings := []manifest.PublicSetting{
		{Name: "appName", Env: "VITE_APP_NAME", Description: "Product name", Fallback: "Foundry", Kind: "string", MaxLength: 60},
		{Name: "locale", Env: "VITE_LOCALE", Description: "BCP 47 locale", Fallback: "en", Kind: "locale"},
		{Name: "currency", Env: "VITE_CURRENCY", Description: "ISO currency", Fallback: "USD", Kind: "currency"},
		{Name: "timeZone", Env: "VITE_TIME_ZONE", Description: "IANA zone", Fallback: "UTC", Kind: "time_zone"},
	}
	source, err := manifest.Build(t.Context(), manifest.Sources{PublicConfig: settings})
	if err != nil {
		t.Fatalf("unexpected build error: %v", err)
	}
	outputBytes, err := typescript.Render(source)
	if err != nil {
		t.Fatalf("unexpected render error: %v", err)
	}
	output := string(outputBytes)

	for _, want := range []string{
		"export interface PublicEnvEntry {",
		"export const publicEnvSchema = {",
		`VITE_APP_NAME: {`,
		`description: "Product name"`,
		`fallback: "Foundry"`,
		`isValid: (value: string) => value.trim().length <= 60`,
		`VITE_LOCALE: {`,
		`isValid: isLocale`,
		`VITE_CURRENCY: {`,
		`isValid: isCurrency`,
		`VITE_TIME_ZONE: {`,
		`isValid: isTimeZone`,
		"export type PublicEnvKey = keyof typeof publicEnvSchema;",
		"export function isPublicEnvKey(key: string): key is PublicEnvKey",
		"export function findPublicEnvProblems(env: Record<string, unknown>, prefix: string): string[]",
		"export function isValidPublicValue(key: PublicEnvKey, value: unknown): boolean",
		"export interface PublicEnv {",
		"readonly appName: string;",
		"readonly locale: string;",
		"readonly currency: string;",
		"readonly timeZone: string;",
		"export function decodePublicEnv(env: Record<string, string | undefined>): PublicEnv",
		`appName: read("VITE_APP_NAME")`,
		`locale: read("VITE_LOCALE")`,
		`currency: read("VITE_CURRENCY")`,
		`timeZone: read("VITE_TIME_ZONE")`,
		"function isLocale(value: string): boolean",
		"function isCurrency(value: string): boolean",
		"function isTimeZone(value: string): boolean",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("missing %q in output\nOutput excerpt:\n%s", want, output)
		}
	}
}
