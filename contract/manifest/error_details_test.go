package manifest

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/contract"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/websocket"
)

type RegistryErrorDetails struct {
	Existing RegistryReference `json:"existing"`
}
type RegistryReference struct {
	ID string `json:"id"`
}
type SeatErrorDetails struct {
	Existing SeatReference `json:"existing"`
}
type SeatReference struct {
	ID string `json:"id"`
}

func registryErrorSchema() contract.Schema {
	return contract.Schema{Root: "github.com/weiloon1234/Foundry-Go/contract/manifest.RegistryErrorDetails", Types: []contract.Type{
		{ID: "github.com/weiloon1234/Foundry-Go/contract/manifest.RegistryErrorDetails", Kind: contract.ObjectKind, Properties: []contract.Property{{Name: "existing", Type: "registry.reference", Required: true}}},
		{ID: "registry.reference", Kind: contract.ObjectKind, Properties: []contract.Property{{Name: "id", Type: "registry.id", Required: true}}},
		{ID: "registry.id", Kind: contract.StringKind},
	}}
}

func registryErrorDeclaration() foundryhttp.DetailedErrorDeclaration[RegistryErrorDetails] {
	return foundryhttp.DefineErrorDetails("registry_exists", 409, "Registry already exists.", contract.DefineJSON[RegistryErrorDetails](registryErrorSchema()))
}

func seatErrorDeclaration() foundryhttp.DetailedErrorDeclaration[SeatErrorDetails] {
	return foundryhttp.DefineErrorDetails("seat_unavailable", 409, "Seat is unavailable.", contract.DefineJSON[SeatErrorDetails](contract.Schema{
		Root: "github.com/weiloon1234/Foundry-Go/contract/manifest.SeatErrorDetails", Types: []contract.Type{
			{ID: "github.com/weiloon1234/Foundry-Go/contract/manifest.SeatErrorDetails", Kind: contract.ObjectKind, Properties: []contract.Property{{Name: "existing", Type: "seat.reference", Required: true}}},
			{ID: "seat.reference", Kind: contract.ObjectKind, Properties: []contract.Property{{Name: "id", Type: "seat.id", Required: true}}},
			{ID: "seat.id", Kind: contract.StringKind},
		},
	}))
}

func errorDetailsRoute(t *testing.T, id foundryhttp.RouteID, method foundryhttp.Method, declarations ...foundryhttp.ErrorDeclarationOption) foundryhttp.RouteRegistration {
	t.Helper()
	endpoint := foundryhttp.DefineEndpoint(foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: id, Method: method, Access: foundryhttp.Public}, foundryhttp.StaticPath("/"+string(id))), foundryhttp.EmptyQuery(), foundryhttp.EmptyBody(), foundryhttp.EmptyResponse(204), foundryhttp.Errors(declarations...))
	return endpoint.Handle(func(context.Context, foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, foundryhttp.NoBody]) (foundryhttp.NoContent, error) {
		t.Error("manifest building invoked a handler")
		return foundryhttp.NoContent{}, nil
	})
}

func errorDetailsManifest(t *testing.T, registrations ...foundryhttp.RouteRegistration) *Manifest {
	t.Helper()
	router, err := foundryhttp.NewRouter(registrations...)
	if err != nil {
		t.Fatal(err)
	}
	source, err := Build(t.Context(), Sources{HTTP: router})
	if err != nil {
		t.Fatal(err)
	}
	return source
}

func TestManifestDetailedErrorsBuildAndRoundTrip(t *testing.T) {
	plain := foundryhttp.DefineError("registry_closed", 403, "Registry is closed.")
	detailed := registryErrorDeclaration()
	first := errorDetailsRoute(t, "registry.create", foundryhttp.POST, plain, detailed)
	second := errorDetailsRoute(t, "registry.update", foundryhttp.POST, detailed)
	source := errorDetailsManifest(t, first, second)
	raw, err := source.JSON()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(raw, []byte(`"version": `+strconv.Itoa(Version))) || !bytes.Contains(raw, []byte(`"details_type":`)) {
		t.Fatal("missing v9 details")
	}
	decoded, err := Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	roundTrip, _ := decoded.JSON()
	reordered, _ := errorDetailsManifest(t, second, first).JSON()
	if !bytes.Equal(raw, roundTrip) || !bytes.Equal(raw, reordered) {
		t.Fatal("manifest output is not deterministic")
	}
	doc, _ := decoded.Snapshot()
	definition, _ := detailed.Description()
	plainDefinition, _ := plain.Description()
	if !slices.Contains(doc.Errors, definition) || !slices.Contains(doc.Errors, plainDefinition) {
		t.Fatal("declaration metadata changed", doc.Errors)
	}
	schema, _ := contract.DefineJSON[RegistryErrorDetails](registryErrorSchema()).Description()
	for _, typ := range schema.Types {
		i := slices.IndexFunc(doc.Types, func(candidate contract.Type) bool { return candidate.ID == typ.ID })
		if i < 0 {
			t.Fatal("details schema missing", typ.ID)
		}
		expected, err := json.Marshal(typ)
		if err != nil {
			t.Fatal(err)
		}
		actual, err := json.Marshal(doc.Types[i])
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(actual, expected) {
			t.Fatalf("details schema changed: %s != %s", actual, expected)
		}
	}
	if len(doc.Roots) != 0 {
		t.Fatal("details became globally public roots", doc.Roots)
	}
	base, err := Build(t.Context(), Sources{})
	if err != nil {
		t.Fatal(err)
	}
	baseDoc, _ := base.Snapshot()
	if doc.ErrorType != baseDoc.ErrorType || len(doc.Types) != len(baseDoc.Types)+len(schema.Types) {
		t.Fatal("synthetic envelope or extra types appeared")
	}
	for _, op := range doc.HTTP {
		if !slices.Contains(op.Errors, definition.Code) {
			t.Fatal("operation lost error reference")
		}
	}
	if !slices.Contains(doc.HTTP[0].Errors, plainDefinition.Code) {
		t.Fatal("plain error reference lost")
	}
	// Caller-owned schema graphs and decoded snapshots cannot mutate the frozen bytes.
	doc.Types[0].ID = "changed"
	again, _ := source.JSON()
	if !bytes.Equal(raw, again) {
		t.Fatal("snapshot mutated manifest")
	}
}

func TestManifestDetailedErrorsRefuseInvalidAssembly(t *testing.T) {
	detailed := registryErrorDeclaration()
	missing := registryErrorSchema()
	missing.Types = missing.Types[:1]
	password := registryErrorSchema()
	password.Types[1].Properties[0].Presentation.Kind = contract.PasswordPresentation
	conflict := registryErrorSchema()
	conflict.Types[2].Cases = []json.RawMessage{json.RawMessage(`"only-this-id"`)}
	for _, tc := range []struct {
		name         string
		method       foundryhttp.Method
		declarations []foundryhttp.ErrorDeclarationOption
	}{
		{"missing type", foundryhttp.POST, []foundryhttp.ErrorDeclarationOption{foundryhttp.DefineErrorDetails("registry_exists", 409, "Registry already exists.", contract.DefineJSON[RegistryErrorDetails](missing))}},
		{"reserved code", foundryhttp.POST, []foundryhttp.ErrorDeclarationOption{foundryhttp.DefineErrorDetails(foundryhttp.Conflict, 409, "Request conflicts with current state", contract.DefineJSON[RegistryErrorDetails](registryErrorSchema()))}},
		{"401", foundryhttp.POST, []foundryhttp.ErrorDeclarationOption{foundryhttp.DefineErrorDetails("registry_exists", 401, "Registry already exists.", contract.DefineJSON[RegistryErrorDetails](registryErrorSchema()))}},
		{"HEAD", foundryhttp.HEAD, []foundryhttp.ErrorDeclarationOption{detailed}},
		{"nested password", foundryhttp.POST, []foundryhttp.ErrorDeclarationOption{foundryhttp.DefineErrorDetails("registry_exists", 409, "Registry already exists.", contract.DefineJSON[RegistryErrorDetails](password))}},
		{"same type different schema", foundryhttp.POST, []foundryhttp.ErrorDeclarationOption{detailed, foundryhttp.DefineErrorDetails("registry_exists", 409, "Registry already exists.", contract.DefineJSON[RegistryErrorDetails](conflict))}},
		{"different type", foundryhttp.POST, []foundryhttp.ErrorDeclarationOption{detailed, foundryhttp.DefineErrorDetails("registry_exists", 409, "Registry already exists.", contract.DefineJSON[RegistryReference](contract.Schema{Root: "github.com/weiloon1234/Foundry-Go/contract/manifest.RegistryReference", Types: []contract.Type{{ID: "github.com/weiloon1234/Foundry-Go/contract/manifest.RegistryReference", Kind: contract.ObjectKind, Properties: []contract.Property{{Name: "id", Type: "string", Required: true}}}, {ID: "string", Kind: contract.StringKind}}}))}},
		{"plain and detailed", foundryhttp.POST, []foundryhttp.ErrorDeclarationOption{detailed, foundryhttp.DefineError("registry_exists", 409, "Registry already exists.")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			registrations := make([]foundryhttp.RouteRegistration, len(tc.declarations))
			for i, d := range tc.declarations {
				id := foundryhttp.RouteID("registry.create")
				if i != 0 {
					id = "registry.update"
				}
				registrations[i] = errorDetailsRoute(t, id, tc.method, d)
			}
			router, err := foundryhttp.NewRouter(registrations...)
			if err == nil {
				_, err = Build(t.Context(), Sources{HTTP: router})
			}
			if err == nil {
				t.Fatal("invalid declaration assembled")
			}
		})
	}
	// A valid router can still collide with another source's schema. The shared
	// graph must compare error details just as it compares response payloads.
	router, err := foundryhttp.NewRouter(errorDetailsRoute(t, "registry.create", foundryhttp.POST, detailed))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Build(t.Context(), Sources{HTTP: router, Schemas: []contract.Schema{conflict}}); err == nil {
		t.Fatal("details bypassed graph conflict detection")
	}
}

func TestManifestDetailedErrorsRefuseForgedDocuments(t *testing.T) {
	source := errorDetailsManifest(t, errorDetailsRoute(t, "registry.create", foundryhttp.POST, registryErrorDeclaration()))
	for _, tc := range []struct {
		name, want string
		edit       func(*Document, int)
	}{
		{"missing details type", "missing error details type", func(d *Document, i int) { d.Errors[i].DetailsType = "missing" }},
		{"reserved code", "builtin error definition changed", func(d *Document, i int) {
			for j := range d.Errors {
				if d.Errors[j].Code == foundryhttp.Conflict {
					d.Errors[j].DetailsType = d.Errors[i].DetailsType
				}
			}
		}},
		{"401", "cannot use status 401", func(d *Document, i int) { d.Errors[i].Status = 401 }},
		{"HEAD", "HEAD operations", func(d *Document, i int) { d.HTTP[0].Route.Method = foundryhttp.HEAD }},
		{"nested password", "password presentation", func(d *Document, i int) {
			for j := range d.Types {
				if d.Types[j].ID == "registry.reference" {
					d.Types[j].Properties[0].Presentation.Kind = contract.PasswordPresentation
				}
			}
		}},
		{"unreferenced password", "password presentation", func(d *Document, i int) {
			d.HTTP[0].Errors = slices.DeleteFunc(d.HTTP[0].Errors, func(code foundryhttp.ErrorCode) bool { return code == d.Errors[i].Code })
			for j := range d.Types {
				if d.Types[j].ID == "registry.reference" {
					d.Types[j].Properties[0].Presentation.Kind = contract.PasswordPresentation
				}
			}
		}},
		{"different details types", "duplicate error code", func(d *Document, i int) {
			other := d.Errors[i]
			other.DetailsType = "string"
			d.Errors = append(d.Errors, other)
		}},
		{"plain and detailed", "duplicate error code", func(d *Document, i int) {
			other := d.Errors[i]
			other.DetailsType = ""
			d.Errors = append(d.Errors, other)
		}},
		{"conflicting schema", "", func(d *Document, i int) {
			for _, typ := range d.Types {
				if typ.ID == "registry.id" {
					typ.Cases = []json.RawMessage{json.RawMessage(`"only-this-id"`)}
					d.Types = append(d.Types, typ)
					break
				}
			}
		}},
		{"unknown operation error", "invalid endpoint error reference", func(d *Document, i int) { d.HTTP[0].Errors = append(d.HTTP[0].Errors, "missing") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc, _ := source.Snapshot()
			i := slices.IndexFunc(doc.Errors, func(d foundryhttp.ErrorDefinition) bool { return d.Code == "registry_exists" })
			tc.edit(&doc, i)
			raw, err := json.Marshal(doc)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = Decode(raw); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("forged document: %v; want %s", err, tc.want)
			}
			// The in-memory freeze boundary uses exactly the same validation as Decode.
			if _, err = freeze(doc); err == nil {
				t.Fatal("invalid in-memory document accepted")
			}
		})
	}
}

func TestManifestDetailedErrorProjectionReachability(t *testing.T) {
	registry, seat := registryErrorDeclaration(), seatErrorDeclaration()
	registryPlain := foundryhttp.DefineError("registry_closed", 403, "Registry is closed.")
	seatPlain := foundryhttp.DefineError("seat_closed", 403, "Seat is closed.")
	source := errorDetailsManifest(t, errorDetailsRoute(t, "registry.create", foundryhttp.POST, registry, registryPlain), errorDetailsRoute(t, "seat.reserve", foundryhttp.POST, seat, seatPlain))
	before, _ := source.JSON()
	for _, tc := range []struct {
		route      foundryhttp.RouteID
		keep, drop []contract.TypeID
		codes      []foundryhttp.ErrorCode
	}{
		{"registry.create", []contract.TypeID{registryErrorSchema().Root, "registry.reference", "registry.id"}, []contract.TypeID{"github.com/weiloon1234/Foundry-Go/contract/manifest.SeatErrorDetails", "seat.reference", "seat.id"}, []foundryhttp.ErrorCode{"registry_exists", "registry_closed"}},
		{"seat.reserve", []contract.TypeID{"github.com/weiloon1234/Foundry-Go/contract/manifest.SeatErrorDetails", "seat.reference", "seat.id"}, []contract.TypeID{registryErrorSchema().Root, "registry.reference", "registry.id"}, []foundryhttp.ErrorCode{"seat_unavailable", "seat_closed"}},
	} {
		t.Run(string(tc.route), func(t *testing.T) {
			projected, err := source.Project(Selection{Routes: []foundryhttp.RouteID{tc.route}})
			if err != nil {
				t.Fatal(err)
			}
			raw, _ := projected.JSON()
			decoded, err := Decode(raw)
			if err != nil {
				t.Fatal(err)
			}
			again, _ := decoded.JSON()
			if !bytes.Equal(raw, again) {
				t.Fatal("projection bytes changed on decode")
			}
			repeated, err := projected.Project(Selection{Routes: []foundryhttp.RouteID{tc.route}})
			if err != nil {
				t.Fatal(err)
			}
			again, _ = repeated.JSON()
			if !bytes.Equal(raw, again) {
				t.Fatal("projection is not idempotent")
			}
			doc, _ := projected.Snapshot()
			if len(doc.Roots) != 0 || len(doc.HTTP) != 1 || doc.HTTP[0].Route.ID != tc.route {
				t.Fatal("projection scope changed")
			}
			for _, id := range tc.keep {
				if !slices.ContainsFunc(doc.Types, func(typ contract.Type) bool { return typ.ID == id }) {
					t.Fatal("lost nested details type", id)
				}
			}
			for _, id := range tc.drop {
				if slices.ContainsFunc(doc.Types, func(typ contract.Type) bool { return typ.ID == id }) {
					t.Fatal("unselected details leaked", id)
				}
			}
			expected := foundryhttp.ErrorDefinitions()
			for _, definition := range doc.Errors {
				if slices.Contains(tc.codes, definition.Code) {
					expected = append(expected, definition)
				}
			}
			slices.SortFunc(expected, func(a, b foundryhttp.ErrorDefinition) int { return strings.Compare(string(a.Code), string(b.Code)) })
			if len(doc.Errors) != len(foundryhttp.ErrorDefinitions())+len(tc.codes) || !slices.Equal(doc.Errors, expected) {
				t.Fatal("projection error catalog changed", doc.Errors)
			}
		})
	}
	full, err := source.Project(Selection{Routes: []foundryhttp.RouteID{"seat", "registry"}})
	if err != nil {
		t.Fatal(err)
	}
	all, _ := full.JSON()
	if !bytes.Equal(before, all) {
		t.Fatal("selecting all operations changed the manifest")
	}
	after, _ := source.JSON()
	if !bytes.Equal(before, after) {
		t.Fatal("projection mutated source")
	}
}

// The hand-kept v8 baseline uses only the built-in envelope graph and catalog
// retained in the current-contracts/client v8 manifest, with no operations.
func TestManifestDetailFreeBytesOnlyChangeVersion(t *testing.T) {
	old, err := os.ReadFile("testdata/detail_free_v8.json")
	if err != nil {
		t.Fatal(err)
	}
	source, err := Build(t.Context(), Sources{})
	if err != nil {
		t.Fatal(err)
	}
	current, _ := source.JSON()
	expected := bytes.Replace(old, []byte(`"version": 8`), []byte(`"version": `+strconv.Itoa(Version)), 1)
	if !bytes.Equal(current, expected) {
		t.Fatal("detail-free manifest changed beyond its version")
	}
	if _, err := Decode(old); err == nil {
		t.Fatal("v8 compatibility reader was added")
	}
	if bytes.Contains(current, []byte("details_type")) {
		t.Fatal("empty details metadata serialized")
	}
}

func TestManifestDetailsRootShapes(t *testing.T) {
	source := errorDetailsManifest(t, errorDetailsRoute(t, "registry.create", foundryhttp.POST, registryErrorDeclaration()))
	for _, root := range []contract.Type{
		{Kind: contract.StringKind},
		{Kind: contract.IntegerKind, Bits: 32, Signed: true},
		{Kind: contract.ArrayKind, Element: "shape.string"},
		{Kind: contract.MapKind, Element: "shape.string"},
		{Kind: contract.DynamicKind, Nullable: true},
		{Kind: contract.ObjectKind, Nullable: true},
	} {
		for _, alias := range []bool{false, true} {
			name := string(root.Kind)
			if alias {
				name = "alias/" + name
			}
			t.Run(name, func(t *testing.T) {
				root.ID = "shape.root"
				schema := contract.Schema{Root: root.ID, Types: []contract.Type{root, {ID: "shape.string", Kind: contract.StringKind}}}
				if alias {
					schema.Root = "shape.alias"
					schema.Types = append(schema.Types, contract.Type{ID: schema.Root, Kind: contract.AliasKind, Element: root.ID})
				}
				descriptor := contract.DefineJSONField[any](schema)
				if err := descriptor.Validate(); err != nil {
					t.Fatal("invalid test schema", err)
				}
				declaration := foundryhttp.DefineErrorDetails("bad_details", 409, "Bad details.", descriptor)
				if _, err := foundryhttp.NewRouter(errorDetailsRoute(t, "invalid", foundryhttp.POST, declaration)); err == nil {
					t.Fatal("non-object details assembled")
				}
				doc, _ := source.Snapshot()
				doc.Types = append(doc.Types, schema.Types...)
				for i := range doc.Errors {
					if doc.Errors[i].Code == "registry_exists" {
						doc.Errors[i].DetailsType = schema.Root
					}
				}
				raw, err := json.Marshal(doc)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := Decode(raw); err == nil || !strings.Contains(err.Error(), "non-nullable object") {
					t.Fatal("forged details root accepted", err)
				}
			})
		}
	}
	t.Run("error envelope", func(t *testing.T) {
		declaration := foundryhttp.DefineErrorDetails("bad_details", 409, "Bad details.", foundryhttp.ErrorResponseJSON())
		router, err := foundryhttp.NewRouter(errorDetailsRoute(t, "invalid", foundryhttp.POST, declaration))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := Build(t.Context(), Sources{HTTP: router}); err == nil {
			t.Fatal("error envelope became details")
		}
		doc, _ := source.Snapshot()
		for i := range doc.Errors {
			if doc.Errors[i].Code == "registry_exists" {
				doc.Errors[i].DetailsType = doc.ErrorType
			}
		}
		raw, _ := json.Marshal(doc)
		if _, err := Decode(raw); err == nil {
			t.Fatal("forged error envelope details accepted")
		}
	})
}

func TestManifestSharedDetailsReachability(t *testing.T) {
	descriptor := contract.DefineJSON[RegistryErrorDetails](registryErrorSchema())
	other := foundryhttp.DefineErrorDetails("registry_locked", 409, "Registry locked.", descriptor)
	response := foundryhttp.DefineEndpoint(foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "registry.show", Method: foundryhttp.GET, Access: foundryhttp.Public}, foundryhttp.StaticPath("/registry")), foundryhttp.EmptyQuery(), foundryhttp.EmptyBody(), foundryhttp.JSONResponse(200, descriptor)).Handle(func(context.Context, foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, foundryhttp.NoBody]) (RegistryErrorDetails, error) {
		return RegistryErrorDetails{}, nil
	})
	router, err := foundryhttp.NewRouter(response, errorDetailsRoute(t, "registry.create", foundryhttp.POST, registryErrorDeclaration()), errorDetailsRoute(t, "registry.lock", foundryhttp.POST, other), errorDetailsRoute(t, "seat.reserve", foundryhttp.POST, seatErrorDeclaration()))
	if err != nil {
		t.Fatal(err)
	}
	channel := websocket.Public[struct{}]("registry", websocket.DefineRooms(foundryhttp.StringPath[string]()))
	registry, err := websocket.NewRegistry(websocket.Register(channel, websocket.DefineOutgoing(channel, "updated", descriptor).Registration()))
	if err != nil {
		t.Fatal(err)
	}
	realtime, err := websocket.DescribeClient(registry, websocket.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	source, err := Build(t.Context(), Sources{HTTP: router, Realtime: &realtime})
	if err != nil {
		t.Fatal(err)
	}
	doc, _ := source.Snapshot()
	// A hand-made private channel document also exercises reader normalization.
	doc.Realtime.Channels[0].Private = true
	doc.Realtime.Channels[0].Guard = "registry.api"
	doc.Realtime.Channels[0].Provider = "registries"
	source, err = freeze(doc)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name      string
		selection Selection
		code      foundryhttp.ErrorCode
	}{
		{"response", Selection{Routes: []foundryhttp.RouteID{"registry.show"}}, ""},
		{"other code", Selection{Routes: []foundryhttp.RouteID{"registry.lock"}}, "registry_locked"},
		{"path", Selection{Paths: []string{"/registry.create"}}, "registry_exists"},
		{"channel", Selection{Channels: []websocket.ChannelID{"registry"}}, ""},
		{"guard", Selection{Guards: []auth.GuardName{"registry.api"}}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			projected, err := source.Project(tc.selection)
			if err != nil {
				t.Fatal(err)
			}
			data, _ := projected.JSON()
			projected, err = Decode(data)
			if err != nil {
				t.Fatal(err)
			}
			view, _ := projected.Snapshot()
			if len(view.Roots) != 0 {
				t.Fatal("details became public roots")
			}
			for _, id := range []contract.TypeID{registryErrorSchema().Root, "registry.reference", "registry.id"} {
				count := 0
				for _, typ := range view.Types {
					if typ.ID == id {
						count++
					}
				}
				if count != 1 {
					t.Fatal("shared schema missing or duplicated", id, count)
				}
			}
			if bytes.Contains(data, []byte("seat.reference")) {
				t.Fatal("unselected details leaked")
			}
			for _, definition := range view.Errors {
				if definition.DetailsType != "" && definition.Code != tc.code {
					t.Fatal("unselected error retained", definition.Code)
				}
			}
			if tc.code != "" && !slices.ContainsFunc(view.Errors, func(def foundryhttp.ErrorDefinition) bool {
				return def.Code == tc.code && def.DetailsType == registryErrorSchema().Root
			}) {
				t.Fatal("selected declaration missing")
			}
		})
	}
}
