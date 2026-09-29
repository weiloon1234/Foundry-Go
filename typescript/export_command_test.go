package typescript_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/cli"
	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/contract/manifest"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/foundation"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/openapi"
	"github.com/weiloon1234/Foundry-Go/typescript"
)

func TestExportCommandPublishesFromApplicationSources(t *testing.T) {
	route := foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "notes.read", Method: foundryhttp.GET, Access: foundryhttp.Public}, foundryhttp.StaticPath("/notes"))
	endpoint := foundryhttp.DefineEndpoint(route, foundryhttp.EmptyQuery(), foundryhttp.EmptyBody(), foundryhttp.JSONResponse(200, contract.StringJSON[string]())).
		WithDocumentation(foundryhttp.RouteDocumentation{Summary: "Read notes */ safely", Deprecated: true})
	router, err := foundryhttp.NewRouter(endpoint.Handle(func(context.Context, foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, foundryhttp.NoBody]) (string, error) {
		t.Fatal("export ran a handler")
		return "", nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	sources := func(context.Context, foundation.Resolver) (manifest.Sources, error) {
		return manifest.Sources{HTTP: router}, nil
	}
	if _, err := typescript.ExportCommand("contracts.export", openapi.Options{Title: "Notes"}, sources); !errors.Is(err, fault.Invalid) {
		t.Fatal("missing API version accepted", err)
	}
	declaration, err := typescript.ExportCommand("contracts.export", openapi.Options{Title: "Notes", APIVersion: "1"}, sources)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := cli.New(declaration)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	run := func(args ...string) (string, error) {
		invocation, err := registry.Parse(append([]string{"contracts.export"}, args...), &bytes.Buffer{})
		if err != nil {
			return "", err
		}
		var out bytes.Buffer
		err = invocation.Run(t.Context(), nil, cli.Streams{In: strings.NewReader(""), Out: &out, Err: &bytes.Buffer{}})
		return out.String(), err
	}
	if _, err := run(); cli.Status(err) != cli.InvalidUsage {
		t.Fatal("missing --dir accepted", err)
	}
	if output, err := run("--dir", dir); err != nil || !strings.Contains(output, "Generated 3 client artifact(s)") {
		t.Fatalf("export = %q, %v", output, err)
	}
	if output, err := run("--dir", dir, "--check"); err != nil || !strings.Contains(output, "current") {
		t.Fatalf("check = %q, %v", output, err)
	}
	sdk, err := os.ReadFile(filepath.Join(dir, "contracts_foundry.gen.ts"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(sdk), "   * Read notes *\\/ safely\n   * @deprecated\n") {
		t.Fatal("operation documentation is missing or can close its comment")
	}
}
