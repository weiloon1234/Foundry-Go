package manifest_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/contract/manifest"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/websocket"
)

func endpoint(t *testing.T, id foundryhttp.RouteID, path string) foundryhttp.RouteRegistration {
	t.Helper()
	d := foundryhttp.DefineEndpoint(foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: id, Method: foundryhttp.GET, Access: foundryhttp.Public}, foundryhttp.StaticPath(path)), foundryhttp.EmptyQuery(), foundryhttp.EmptyBody(), foundryhttp.JSONResponse(201, contract.StringJSON[string]()))
	return d.Handle(func(context.Context, foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, foundryhttp.NoBody]) (string, error) {
		t.Fatal("metadata invoked handler")
		return "", nil
	})
}

func TestManifestUsesRegisteredMetadataAndSurvivesSerialization(t *testing.T) {
	router, err := foundryhttp.NewRouter(endpoint(t, "profile.show", "/profile"), endpoint(t, "health", "/health"))
	if err != nil {
		t.Fatal(err)
	}
	channel := websocket.Public[struct{}]("orders", websocket.DefineRooms(foundryhttp.StringPath[string]()))
	event := websocket.DefineOutgoing(channel, "updated", contract.StringJSON[string]())
	registry, err := websocket.NewRegistry(websocket.Register(channel, event.Registration()))
	if err != nil {
		t.Fatal(err)
	}
	realtime, err := websocket.DescribeClient(registry, websocket.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	frozen, err := manifest.Build(t.Context(), manifest.Sources{HTTP: router, Realtime: &realtime})
	if err != nil {
		t.Fatal(err)
	}
	data, err := frozen.JSON()
	if err != nil {
		t.Fatal(err)
	}
	restored, err := manifest.Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	again, err := restored.JSON()
	if err != nil || !bytes.Equal(data, again) {
		t.Fatal("serialized manifest is not canonical", err)
	}
	document, err := restored.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if len(document.HTTP) != 2 || document.HTTP[1].Name != "profileShow" || document.HTTP[1].Status != 201 || document.HTTP[1].Response.Type != "string" {
		t.Fatal("route metadata changed")
	}
	if document.HTTP[0].Path == nil || document.HTTP[0].Query == nil {
		t.Fatal("empty parameter lists are not JSON arrays")
	}
	if document.Realtime.Channels[0].Events[0].Direction != websocket.ServerToClient || document.Realtime.Channels[0].Events[0].Payload != "string" {
		t.Fatal("realtime direction or schema changed")
	}
	document.HTTP[0].Route.ID = "changed"
	data[0] = '!'
	stable, _ := frozen.JSON()
	if !bytes.Equal(stable, again) {
		t.Fatal("snapshot or JSON shares manifest storage")
	}
}

func TestManifestRejectsConflictsReferencesVersionsAndAmbiguousNames(t *testing.T) {
	conflict := contract.Schema{Root: "string", Types: []contract.Type{{ID: "string", Kind: contract.IntegerKind, Bits: 32, Signed: true}}}
	if _, err := manifest.Build(t.Context(), manifest.Sources{Schemas: []contract.Schema{conflict}}); err == nil {
		t.Fatal("schema conflict accepted")
	}
	router, err := foundryhttp.NewRouter(endpoint(t, "users.show", "/a"), endpoint(t, "users-show", "/b"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manifest.Build(t.Context(), manifest.Sources{HTTP: router}); err == nil {
		t.Fatal("camel-case collision accepted")
	}
	base, err := manifest.Build(t.Context(), manifest.Sources{})
	if err != nil {
		t.Fatal(err)
	}
	empty, err := base.Snapshot()
	if err != nil || empty.HTTP == nil || len(empty.HTTP) != 0 {
		t.Fatal("empty operation list is not a JSON array", err)
	}
	for _, change := range []func(*manifest.Document){
		func(d *manifest.Document) { d.Version++ },
		func(d *manifest.Document) { d.Roots = []contract.TypeID{"missing"} },
		func(d *manifest.Document) { d.ErrorType = "string" },
		func(d *manifest.Document) { d.Errors[0].Status = 299 },
		func(d *manifest.Document) { d.Types = append(d.Types, d.Types[0]) },
	} {
		document, _ := base.Snapshot()
		change(&document)
		data, err := json.Marshal(document)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := manifest.Decode(data); err == nil {
			t.Fatal("invalid manifest accepted")
		}
	}
	for _, data := range []string{`{"version":1,"version":1}`, `{"version":1,"unknown":true}`, `{"version":1,"types":"\ud800"}`} {
		if _, err := manifest.Decode([]byte(data)); err == nil {
			t.Fatal("ambiguous JSON accepted")
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := manifest.Build(ctx, manifest.Sources{}); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation ignored", err)
	}
	if _, err := (*manifest.Manifest)(nil).JSON(); err == nil {
		t.Fatal("nil manifest accepted")
	}
}

func TestManifestRejectsForgedTransportContracts(t *testing.T) {
	router, err := foundryhttp.NewRouter(endpoint(t, "sample", "/sample"))
	if err != nil {
		t.Fatal(err)
	}
	source, err := manifest.Build(t.Context(), manifest.Sources{HTTP: router})
	if err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func(*manifest.Operation){
		"get body": func(op *manifest.Operation) {
			op.Body = &manifest.Payload{MediaType: "application/json", Type: "string"}
		},
		"trace":           func(op *manifest.Operation) { op.Route.Method = foundryhttp.TRACE },
		"file budget":     func(op *manifest.Operation) { op.FileTransferBytes = 1024 },
		"signed version":  func(op *manifest.Operation) { op.Route.SignedURL = &foundryhttp.SignedURLInfo{Version: "future"} },
		"query name":      func(op *manifest.Operation) { op.Query = []manifest.Parameter{{Name: "bad&key", Type: "string"}} },
		"bodyless status": func(op *manifest.Operation) { op.Status = 204 },
		"documentation": func(op *manifest.Operation) {
			op.Route.Documentation = &foundryhttp.RouteDocumentation{Summary: "two\nlines"}
		},
		"example media": func(op *manifest.Operation) {
			op.Response = &manifest.Payload{File: &foundryhttp.FileResponseInfo{MediaTypes: []foundryhttp.MediaType{"text/plain"}}, Example: json.RawMessage(`"text"`)}
		},
	} {
		t.Run(name, func(t *testing.T) {
			document, err := source.Snapshot()
			if err != nil {
				t.Fatal(err)
			}
			change(&document.HTTP[0])
			data, err := json.Marshal(document)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := manifest.Decode(data); err == nil {
				t.Fatal("forged transport contract accepted")
			}
		})
	}
}

func FuzzManifestDecoder(f *testing.F) {
	valid, err := manifest.Build(context.Background(), manifest.Sources{})
	if err != nil {
		f.Fatal(err)
	}
	seed, err := valid.JSON()
	if err != nil {
		f.Fatal(err)
	}
	f.Add(seed)
	presentation, err := credentialManifest(f).JSON()
	if err != nil {
		f.Fatal(err)
	}
	f.Add(presentation)
	f.Add([]byte(`{"version":1}`))
	f.Add([]byte(`{"version":2,"types":[]}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		m, err := manifest.Decode(data)
		if err != nil {
			return
		}
		canonical, err := m.JSON()
		if err != nil {
			t.Fatal(err)
		}
		next, err := manifest.Decode(canonical)
		if err != nil {
			t.Fatal(err)
		}
		again, _ := next.JSON()
		if !bytes.Equal(canonical, again) {
			t.Fatal("decoder is not idempotent")
		}
	})
}
