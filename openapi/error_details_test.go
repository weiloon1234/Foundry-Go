package openapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"maps"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/contract/manifest"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/openapi"
)

type RegistryErrorDetails struct {
	Existing RegistryReference `json:"existing"`
}

type RegistryReference struct {
	ID string `json:"id"`
}

type SeatErrorDetails struct {
	Seat SeatReference `json:"seat"`
}

type SeatReference struct {
	Available bool `json:"available"`
}

const registryDetailsType contract.TypeID = "github.com/weiloon1234/Foundry-Go/openapi_test.RegistryErrorDetails"
const seatDetailsType contract.TypeID = "github.com/weiloon1234/Foundry-Go/openapi_test.SeatErrorDetails"

func registryError() foundryhttp.DetailedErrorDeclaration[RegistryErrorDetails] {
	return foundryhttp.DefineErrorDetails("registry_exists", 409, "Registry already exists.", contract.DefineJSON[RegistryErrorDetails](contract.Schema{
		Root: registryDetailsType, Types: []contract.Type{
			{ID: registryDetailsType, Kind: contract.ObjectKind, Properties: []contract.Property{{Name: "existing", Type: "registry.reference", Required: true}}},
			{ID: "registry.reference", Kind: contract.ObjectKind, Properties: []contract.Property{{Name: "id", Type: "string", Required: true}}},
			{ID: "string", Kind: contract.StringKind},
		},
	}))
}

func seatError(status int) foundryhttp.DetailedErrorDeclaration[SeatErrorDetails] {
	return foundryhttp.DefineErrorDetails("seat_unavailable", status, "Seat is unavailable.", contract.DefineJSON[SeatErrorDetails](contract.Schema{
		Root: seatDetailsType, Types: []contract.Type{
			{ID: seatDetailsType, Kind: contract.ObjectKind, Properties: []contract.Property{{Name: "seat", Type: "seat.reference", Required: true}}},
			{ID: "seat.reference", Kind: contract.ObjectKind, Properties: []contract.Property{{Name: "available", Type: "bool", Required: true}}},
			{ID: "bool", Kind: contract.BooleanKind},
		},
	}))
}

func errorDetailsRoute(id foundryhttp.RouteID, failure error, declarations ...foundryhttp.ErrorDeclarationOption) foundryhttp.RouteRegistration {
	endpoint := foundryhttp.DefineEndpoint(foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: id, Method: foundryhttp.POST, Access: foundryhttp.Public}, foundryhttp.StaticPath("/"+string(id))), foundryhttp.EmptyQuery(), foundryhttp.EmptyBody(), foundryhttp.EmptyResponse(204), foundryhttp.Errors(declarations...))
	return endpoint.Handle(func(context.Context, noInput) (foundryhttp.NoContent, error) {
		return foundryhttp.NoContent{}, failure
	})
}

func errorDetailsManifest(t *testing.T, registrations ...foundryhttp.RouteRegistration) *manifest.Manifest {
	t.Helper()
	router, err := foundryhttp.NewRouter(registrations...)
	if err != nil {
		t.Fatal(err)
	}
	source, err := manifest.Build(t.Context(), manifest.Sources{HTTP: router})
	if err != nil {
		t.Fatal(err)
	}
	return source
}

func renderErrorDocument(t *testing.T, source *manifest.Manifest) ([]byte, map[string]any) {
	t.Helper()
	data, err := openapi.Render(source, openapi.Options{Title: "Errors", APIVersion: "1"})
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	return data, document
}

func errorObject(t *testing.T, value any, keys ...string) map[string]any {
	t.Helper()
	for _, key := range keys {
		object, ok := value.(map[string]any)
		if !ok {
			t.Fatalf("expected object at %v", keys)
		}
		value = object[key]
	}
	object, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("expected object at %v", keys)
	}
	return object
}

func errorResponseSchema(t *testing.T, document map[string]any, path, status string) map[string]any {
	t.Helper()
	return errorObject(t, document, "paths", path, "post", "responses", status, "content", "application/json", "schema")
}

func errorTypeRef(t *testing.T, document map[string]any, id contract.TypeID) map[string]any {
	t.Helper()
	for name, raw := range errorObject(t, document, "components", "schemas") {
		if errorObject(t, raw)["x-foundry-type-id"] == string(id) {
			return map[string]any{"$ref": "#/components/schemas/" + name}
		}
	}
	t.Fatalf("missing component for %s", id)
	return nil
}

func TestOpenAPIErrorDetailsComposeCompleteBranches(t *testing.T) {
	detailed := registryError()
	plain := foundryhttp.DefineError("registry_closed", 409, "Registry is closed.")
	failure := detailed.New(RegistryErrorDetails{Existing: RegistryReference{ID: "registry-123"}})
	router, err := foundryhttp.NewRouter(errorDetailsRoute("registry.create", failure, detailed, plain))
	if err != nil {
		t.Fatal(err)
	}
	source, err := manifest.Build(t.Context(), manifest.Sources{HTTP: router})
	if err != nil {
		t.Fatal(err)
	}
	_, document := renderErrorDocument(t, source)
	schema := errorResponseSchema(t, document, "/registry.create", "409")
	branches, _ := schema["oneOf"].([]any)
	if len(branches) != 2 || len(schema) != 1 {
		t.Fatalf("expected one plain group and one detailed branch: %v", schema)
	}
	snapshot, err := source.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	baseRef := errorTypeRef(t, document, snapshot.ErrorType)
	base := errorObject(t, document, "components", "schemas", strings.TrimPrefix(baseRef["$ref"].(string), "#/components/schemas/"))
	plainBranch := errorObject(t, branches[0])
	wantPlain := map[string]any{"allOf": []any{baseRef, map[string]any{"properties": map[string]any{
		"status": map[string]any{"const": float64(409)}, "error_code": map[string]any{"enum": []any{"conflict", "registry_closed"}},
	}}}}
	if !reflect.DeepEqual(plainBranch, wantPlain) {
		t.Fatal("detail-free codes changed their legacy shape", plainBranch)
	}
	branch := errorObject(t, branches[1])
	for _, key := range []string{"title", "x-foundry-type-id"} {
		if _, exists := branch[key]; exists {
			t.Fatal("detailed branch inherited base envelope identity", key)
		}
	}
	properties := errorObject(t, branch, "properties")
	baseProperties := errorObject(t, base, "properties")
	if branch["type"] != "object" || branch["additionalProperties"] != false || branch["allOf"] != nil || len(properties) != len(baseProperties)+1 {
		t.Fatal("detailed branch is not a complete closed envelope", branch)
	}
	for name, raw := range baseProperties {
		want := errorObject(t, raw)
		if name == "status" || name == "error_code" {
			want = maps.Clone(want)
			if name == "status" {
				want["const"] = float64(409)
			} else {
				want["const"] = "registry_exists"
			}
		}
		if !reflect.DeepEqual(properties[name], want) {
			t.Fatalf("envelope property %s changed: %v", name, properties[name])
		}
	}
	if !reflect.DeepEqual(properties["details"], errorTypeRef(t, document, registryDetailsType)) {
		t.Fatal("details lost their type reference", properties["details"])
	}
	wantRequired := append(slices.Clone(base["required"].([]any)), "details")
	if !reflect.DeepEqual(branch["required"], wantRequired) || baseProperties["details"] != nil {
		t.Fatal("required fields or base envelope changed", branch, base)
	}

	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest("POST", "/registry.create", nil))
	var envelope map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if response.Code != 409 || envelope["error_code"] != "registry_exists" || errorObject(t, envelope, "details", "existing")["id"] != "registry-123" {
		t.Fatal("unexpected real error envelope", response.Code, envelope)
	}
	// There is no schema validator in this package. Check the closed object's
	// properties and required list directly against the real wire envelope.
	for name := range envelope {
		if properties[name] == nil {
			t.Fatalf("closed branch rejects real envelope property %s", name)
		}
	}
	for _, name := range branch["required"].([]any) {
		if _, present := envelope[name.(string)]; !present {
			t.Fatalf("real envelope lacks required property %s", name)
		}
	}
	// Literal code sets must be disjoint: no wire code can select two branches.
	plainCodes := errorObject(t, plainBranch["allOf"].([]any)[1], "properties", "error_code")["enum"].([]any)
	detailedCode := errorObject(t, properties, "error_code")["const"]
	if slices.Contains(plainCodes, detailedCode) || detailedCode != envelope["error_code"] {
		t.Fatal("oneOf branches overlap or reject the actual error code")
	}

	// A different, detail-free status keeps the legacy schema, including validation.
	validation := errorResponseSchema(t, document, "/registry.create", "422")
	if validation["oneOf"] != nil || validation["allOf"] == nil {
		t.Fatal("detail-free validation response changed", validation)
	}
}

func TestOpenAPIErrorDetailsVariantsAndDeterminism(t *testing.T) {
	registry, seat := registryError(), seatError(409)
	first := errorDetailsRoute("registry.create", nil, seat, registry)
	second := errorDetailsRoute("registry.update", nil, registry)
	source := errorDetailsManifest(t, first, second)
	data, document := renderErrorDocument(t, source)
	reordered := errorDetailsManifest(t, second, errorDetailsRoute("registry.create", nil, registry, seat))
	again, _ := renderErrorDocument(t, reordered)
	if !bytes.Equal(data, again) {
		t.Fatal("route or declaration order changed OpenAPI bytes")
	}
	branches, _ := errorResponseSchema(t, document, "/registry.create", "409")["oneOf"].([]any)
	if len(branches) != 3 {
		t.Fatal("expected builtin conflict and two detailed variants", branches)
	}
	seenCodes := make(map[any]bool)
	for _, code := range errorObject(t, errorObject(t, branches[0])["allOf"].([]any)[1], "properties", "error_code")["enum"].([]any) {
		seenCodes[code] = true
	}
	for i, tc := range []struct {
		code         string
		root, nested contract.TypeID
		property     string
	}{
		{"registry_exists", registryDetailsType, "registry.reference", "existing"},
		{"seat_unavailable", seatDetailsType, "seat.reference", "seat"},
	} {
		branch := errorObject(t, branches[i+1])
		properties := errorObject(t, branch, "properties")
		code := errorObject(t, properties, "error_code")["const"]
		if seenCodes[code] || !slices.Contains(branch["required"].([]any), "details") {
			t.Fatal("detailed alternatives overlap or allow missing details")
		}
		seenCodes[code] = true
		if errorObject(t, properties, "error_code")["const"] != tc.code || errorObject(t, properties, "status")["const"] != float64(409) || !reflect.DeepEqual(properties["details"], errorTypeRef(t, document, tc.root)) {
			t.Fatalf("wrong correlated branch for %s: %v", tc.code, properties)
		}
		rootRef := errorTypeRef(t, document, tc.root)
		root := errorObject(t, document, "components", "schemas", strings.TrimPrefix(rootRef["$ref"].(string), "#/components/schemas/"))
		if !reflect.DeepEqual(errorObject(t, root, "properties")[tc.property], errorTypeRef(t, document, tc.nested)) {
			t.Fatal("nested details reference changed", root)
		}
	}
	other := errorResponseSchema(t, document, "/registry.update", "409")["oneOf"].([]any)
	if len(other) != 2 || bytes.Contains(must(json.Marshal(other)), []byte("seat_unavailable")) {
		t.Fatal("operation acquired an undeclared detailed variant", other)
	}
	snapshot, err := source.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if len(errorObject(t, document, "components", "schemas")) != len(snapshot.Types) {
		t.Fatal("synthetic envelope component appeared")
	}
}

func TestOpenAPIErrorDetailsWithoutPlainVariant(t *testing.T) {
	source := errorDetailsManifest(t, errorDetailsRoute("seat.reserve", nil, seatError(418)))
	_, document := renderErrorDocument(t, source)
	schema := errorResponseSchema(t, document, "/seat.reserve", "418")
	branches, _ := schema["oneOf"].([]any)
	if len(branches) != 1 {
		t.Fatal("detailed-only status must have one variant", schema)
	}
	properties := errorObject(t, branches[0], "properties")
	if errorObject(t, properties, "status")["const"] != float64(418) || errorObject(t, properties, "error_code")["const"] != "seat_unavailable" || !reflect.DeepEqual(properties["details"], errorTypeRef(t, document, seatDetailsType)) || !slices.Contains(errorObject(t, branches[0])["required"].([]any), "details") {
		t.Fatal("detailed-only status lost its contract", branches[0])
	}
}

func TestOpenAPIErrorDetailsProjection(t *testing.T) {
	source := errorDetailsManifest(t, errorDetailsRoute("registry.create", nil, registryError()), errorDetailsRoute("seat.reserve", nil, seatError(409)))
	before, err := source.JSON()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		route            foundryhttp.RouteID
		keep, nested     contract.TypeID
		drop, dropNested contract.TypeID
		dropCode         string
	}{
		{"registry.create", registryDetailsType, "registry.reference", seatDetailsType, "seat.reference", "seat_unavailable"},
		{"seat.reserve", seatDetailsType, "seat.reference", registryDetailsType, "registry.reference", "registry_exists"},
	} {
		t.Run(string(tc.route), func(t *testing.T) {
			projected, err := source.Project(manifest.Selection{Routes: []foundryhttp.RouteID{tc.route}})
			if err != nil {
				t.Fatal(err)
			}
			data, document := renderErrorDocument(t, projected)
			if len(errorObject(t, document, "paths")) != 1 || bytes.Contains(data, []byte(tc.drop)) || bytes.Contains(data, []byte(tc.dropNested)) || bytes.Contains(data, []byte(tc.dropCode)) {
				t.Fatal("unselected error details leaked into projection")
			}
			errorTypeRef(t, document, tc.keep)
			errorTypeRef(t, document, tc.nested)
			branches := errorResponseSchema(t, document, "/"+string(tc.route), "409")["oneOf"].([]any)
			if len(branches) != 2 || !reflect.DeepEqual(errorObject(t, branches[1], "properties")["details"], errorTypeRef(t, document, tc.keep)) {
				t.Fatal("selected detailed variant was lost", branches)
			}
		})
	}
	after, err := source.JSON()
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("rendering projections mutated the source", err)
	}
}

// The fixture retains the pre-details failure route, its reachable schemas and
// exact OpenAPI bytes. It covers builtin errors, a custom code sharing status 409,
// validation errors and the closed base envelope without a release-cohort reader.
func TestOpenAPIDetailFreeGoldenBytesOnlyChangeVersion(t *testing.T) {
	old, err := os.ReadFile(filepath.Join("testdata", "detail_free_manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	current := bytes.Replace(old, []byte(`"version": 8`), []byte(`"version": `+strconv.Itoa(manifest.Version)), 1)
	source, err := manifest.Decode(current)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := source.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	for _, definition := range snapshot.Errors {
		if definition.DetailsType != "" {
			t.Fatal("baseline must remain detail-free")
		}
	}
	data, err := openapi.Render(source, openapi.Options{Title: "Consumer", APIVersion: "1"})
	if err != nil {
		t.Fatal(err)
	}
	golden, err := os.ReadFile(filepath.Join("testdata", "detail_free_openapi.json"))
	if err != nil {
		t.Fatal(err)
	}
	expected := bytes.Replace(golden, []byte(`"x-foundry-manifest-version": 8`), []byte(`"x-foundry-manifest-version": `+strconv.Itoa(manifest.Version)), 1)
	if !bytes.Equal(data, expected) {
		offset := 0
		for offset < min(len(data), len(expected)) && data[offset] == expected[offset] {
			offset++
		}
		t.Fatalf("detail-free OpenAPI changed at byte %d: got %.160q; want %.160q", offset, data[offset:], expected[offset:])
	}
}
