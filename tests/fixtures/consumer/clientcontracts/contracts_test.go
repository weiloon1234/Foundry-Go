package clientcontracts_test

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/weiloon1234/Foundry-Go/testkit"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"foundry.test/consumer/clientcontracts"
	"github.com/weiloon1234/Foundry-Go/contract/manifest"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/openapi"
	"github.com/weiloon1234/Foundry-Go/typescript"
	"github.com/weiloon1234/Foundry-Go/websocket"
)

func fixture(t *testing.T) clientcontracts.Fixture {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "sample.txt"), []byte("client download"), 0600); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.Close() })
	result, err := clientcontracts.NewFixture(t.Context(), root, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestConsumerClientManifestKeepsRealFeatureBoundaries(t *testing.T) {
	f := fixture(t)
	source, err := f.Manifest(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	data, err := source.JSON()
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte("must-not-be-exported")) || bytes.Contains(data, []byte(`"Private"`)) {
		t.Fatal("authentication model became public schema")
	}
	document, err := source.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if document.Version != manifest.Version || len(document.HTTP) != 23 || document.Realtime == nil || len(document.Realtime.Channels) != 2 || document.Locales == nil || len(document.Enums) != 1 || len(document.Permissions) != 1 {
		t.Fatal("registered metadata missing")
	}
	if len(document.Tables) != 1 || document.Tables[0].ID != "reports.members" || !document.Tables[0].Exports || len(document.Notifications) != 1 || len(document.Notifications[0].Channels) != 1 || document.Notifications[0].Channels[0].Payload != "foundry.test/consumer/clientcontracts.Member" {
		t.Fatal("table or rendered notification metadata missing")
	}
	var account, page bool
	securedPages := 0
	for _, op := range document.HTTP {
		switch op.Name {
		case "accountShow":
			account = true
			if op.Route.Authentication == nil || op.Route.Authentication.Credential.Name != "fixture_session" || len(op.Route.Authentication.RequiredPermissions) != 1 {
				t.Fatal("credential/permission metadata missing")
			}
		case "membersSecure", "membersSecureSimple", "membersSecureCursor":
			securedPages++
			if op.Route.Authentication == nil || op.Route.Authentication.Credential.Name != "fixture_session" || len(op.Route.Authentication.RequiredPermissions) != 1 {
				t.Fatal("pagination did not export its actual guard binding")
			}
		case "membersIndex":
			page = true
			defaults := map[string]string{}
			for _, p := range op.Query {
				if text, set := p.DefaultURL.Get(); set {
					defaults[p.Name] = text
				}
			}
			if defaults["page"] != "1" || defaults["per_page"] != "20" || op.Validation == nil {
				t.Fatal("pagination metadata changed")
			}
		}
	}
	if !account || !page || securedPages != 3 {
		t.Fatal("missing actual endpoints")
	}
	openAPI, err := openapi.Render(source, openapi.Options{Title: "Consumer", APIVersion: "1"})
	if err != nil {
		t.Fatal(err)
	}
	var exported struct {
		Paths map[string]struct {
			Get struct {
				Security []map[string][]string `json:"security"`
			} `json:"get"`
		} `json:"paths"`
	}
	if err := json.Unmarshal(openAPI, &exported); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/members/secure", "/members/secure-simple", "/members/secure-cursor"} {
		if len(exported.Paths[path].Get.Security) != 1 {
			t.Fatal("missing OpenAPI page security", path)
		}
	}
	if _, err := typescript.Render(source); err != nil {
		t.Fatal(err)
	}
	copy, err := manifest.Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	again, err := copy.JSON()
	if err != nil || !bytes.Equal(data, again) {
		t.Fatal("serialized manifest changed", err)
	}
}

func TestTypeScriptClientAgainstRealHTTPAndWebSocket(t *testing.T) {
	testkit.TrackExternalInputs(t)
	node, compiler := os.Getenv("FOUNDRY_TEST_NODE"), os.Getenv("FOUNDRY_TEST_TYPESCRIPT")
	if node == "" || compiler == "" {
		if os.Getenv("FOUNDRY_TEST_TYPESCRIPT_REQUIRED") == "1" {
			t.Fatal("set FOUNDRY_TEST_NODE and FOUNDRY_TEST_TYPESCRIPT to existing native executables/compiler JS")
		}
		t.Skip("TypeScript acceptance requires the selected native Node and compiler")
	}
	for _, path := range []string{node, compiler} {
		if !filepath.IsAbs(path) {
			t.Fatal("TypeScript tool paths must be absolute")
		}
		if info, err := os.Stat(path); err != nil || !info.Mode().IsRegular() {
			t.Fatal("TypeScript tool is unavailable", err)
		}
	}
	f := fixture(t)
	source, err := f.Manifest(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	options := typescript.Options{Dir: dir, OpenAPI: openapi.Options{Title: "Consumer", APIVersion: "1"}}
	if _, err := clientcontracts.Export(t.Context(), source, options); err != nil {
		t.Fatal(err)
	}
	options.Check = true
	if _, err := clientcontracts.Export(t.Context(), source, options); err != nil {
		t.Fatal(err)
	}
	// A prior contract omits the newer empty endpoint and wrapped union variant.
	// Existing operations remain usable; an unknown union variant is rejected.
	previous, err := source.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	kept := previous.HTTP[:0]
	for _, operation := range previous.HTTP {
		if operation.Name != "empty" {
			kept = append(kept, operation)
		}
	}
	previous.HTTP = kept
	for i := range previous.Types {
		typ := &previous.Types[i]
		if typ.ID != "foundry.test/consumer/unions.PaymentMethod" {
			continue
		}
		variants := typ.Variants[:0]
		for _, variant := range typ.Variants {
			if variant.Tag != "wrapped" {
				variants = append(variants, variant)
			}
		}
		typ.Variants = variants
	}
	previousJSON, err := json.Marshal(previous)
	if err != nil {
		t.Fatal(err)
	}
	previousManifest, err := manifest.Decode(previousJSON)
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := typescript.Render(previousManifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "legacy.ts"), legacy, 0600); err != nil {
		t.Fatal(err)
	}
	// The runtime must also compile for applications without streaming routes;
	// their generated response union has no FileResult/EventStreamResult member.
	kept = previous.HTTP[:0]
	for _, operation := range previous.HTTP {
		if operation.Response == nil || operation.Response.File == nil && operation.Response.MediaType != "text/event-stream" {
			kept = append(kept, operation)
		}
	}
	previous.HTTP = kept
	nonStreamingJSON, err := json.Marshal(previous)
	if err != nil {
		t.Fatal(err)
	}
	nonStreamingManifest, err := manifest.Decode(nonStreamingJSON)
	if err != nil {
		t.Fatal(err)
	}
	nonStreaming, err := typescript.Render(nonStreamingManifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "non_streaming.ts"), nonStreaming, 0600); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"types.ts", "runtime.mjs", "forms.mjs"} {
		data, err := os.ReadFile(filepath.Join("testdata", name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"type":"module"}`), 0600); err != nil {
		t.Fatal(err)
	}
	config := map[string]any{"compilerOptions": map[string]any{"target": "ES2022", "module": "NodeNext", "moduleResolution": "NodeNext", "lib": []string{"ES2022", "DOM", "DOM.Iterable"}, "strict": true, "exactOptionalPropertyTypes": true, "noUncheckedIndexedAccess": true, "noEmitOnError": true, "outDir": "dist"}, "include": []string{"*.ts"}}
	settings, _ := json.Marshal(config)
	if err := os.WriteFile(filepath.Join(dir, "tsconfig.json"), settings, 0600); err != nil {
		t.Fatal(err)
	}
	run := func(name string, args ...string) {
		t.Helper()
		ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
		defer cancel()
		command := exec.CommandContext(ctx, node, args...)
		command.Dir = dir
		command.WaitDelay = time.Second
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("%s: %v\n%s", name, err, output)
		}
		t.Logf("%s: %s", name, strings.TrimSpace(string(output)))
	}
	run("strict TypeScript and negative contracts", compiler, "--project", filepath.Join(dir, "tsconfig.json"))
	hub, err := websocket.New(f.Registry, f.Authentication, f.Config)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	server := httptest.NewUnstartedServer(mux)
	origin := foundryhttp.Origin("http://" + server.Listener.Addr().String())
	// Signed routes verify against the admitted public origin.
	routes, err := foundryhttp.ApplyMiddleware(f.Router, foundryhttp.PublicURLs(foundryhttp.PublicURLConfig{AllowedOrigins: []foundryhttp.Origin{origin}}))
	if err != nil {
		t.Fatal(err)
	}
	mux.Handle("/", routes)
	mux.Handle("/ws", hub)
	// Native Node WebSocket has no browser cookie jar. This loopback-only test
	// endpoint supplies a fixed fixture credential before the real auth adapter.
	mux.Handle("/ws-auth", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.AddCookie(&http.Cookie{Name: "fixture_session", Value: "fixture-only"})
		hub.ServeHTTP(w, r)
	}))
	server.Start()
	links, err := f.Links(t.Context(), origin)
	if err != nil {
		t.Fatal(err)
	}
	signed, err := json.Marshal(links)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := hub.Stop(ctx); err != nil {
			t.Error(err)
		}
		server.Close()
	})
	run("real HTTP/WebSocket and adversarial codec contracts", filepath.Join(dir, "runtime.mjs"), filepath.Join(dir, "dist", "contracts_foundry.gen.js"), server.URL, filepath.Join(dir, "dist", "legacy.js"), string(signed))
}
