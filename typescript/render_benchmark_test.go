package typescript

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"slices"
	"testing"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/contract/manifest"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/openapi"
)

// These fixtures measure rendering of a frozen, validated manifest. Discovery,
// manifest construction, OpenAPI and filesystem publication are outside timing.
var renderWorkloads = []struct {
	name     string
	types    int
	routes   int
	surfaces int
	chain    bool
}{
	{name: "Small", types: 8, routes: 16},
	{name: "Large", types: 128, routes: 256},
	{name: "MultiSurface", types: 128, routes: 256, surfaces: 8},
	{name: "Deep", types: 512, routes: 1, chain: true},
}

func renderBenchmarkManifest(t testing.TB, count, routes int, chain bool) *manifest.Manifest {
	t.Helper()
	node := func(i int) contract.TypeID { return contract.TypeID(fmt.Sprintf("benchmark.Node%04d", i)) }
	types := []contract.Type{
		{ID: "string", Kind: contract.StringKind},
		{ID: "benchmark.Amount", Kind: contract.IntegerKind, Bits: 64, Signed: true},
		{ID: "benchmark.Nullable", Kind: contract.AliasKind, Element: "string", Nullable: true},
		{ID: "benchmark.State", Kind: contract.StringKind, Cases: []json.RawMessage{json.RawMessage(`"active"`), json.RawMessage(`"archived"`)}},
	}
	for i := range count {
		properties := []contract.Property{
			{Name: "name", Type: "string", Required: true},
			{Name: "amount", Type: "benchmark.Amount", Required: true},
			{Name: "note", Type: "benchmark.Nullable"},
		}
		if chain && i+1 < count {
			properties = append(properties, contract.Property{Name: "next", Type: node(i + 1)})
		} else {
			properties = append(properties, contract.Property{Name: "state", Type: "benchmark.State", Required: true})
		}
		types = append(types, contract.Type{ID: node(i), Kind: contract.ObjectKind, Properties: properties})
	}
	registrations := make([]foundryhttp.RouteRegistration, routes)
	for i := range routes {
		id := foundryhttp.RouteID(fmt.Sprintf("portal%d.read%04d", i%8, i))
		path := fmt.Sprintf("/portal%d/items/%04d", i%8, i)
		endpoint := foundryhttp.DefineEndpoint(foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: id, Method: foundryhttp.GET, Access: foundryhttp.Public}, foundryhttp.StaticPath(path)), foundryhttp.EmptyQuery(), foundryhttp.EmptyBody(), foundryhttp.JSONResponse(200, contract.StringJSON[string]()))
		registrations[i] = endpoint.Handle(func(context.Context, foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, foundryhttp.NoBody]) (string, error) {
			t.Fatal("rendering ran a handler")
			return "", nil
		})
	}
	router, err := foundryhttp.NewRouter(registrations...)
	if err != nil {
		t.Fatal(err)
	}
	source, err := manifest.Build(t.Context(), manifest.Sources{HTTP: router, Schemas: []contract.Schema{{Root: node(0), Types: types}}})
	if err != nil {
		t.Fatal(err)
	}
	document, err := source.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	for i := range document.HTTP {
		document.HTTP[i].Response.Type = node(i % count)
	}
	data, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	source, err = manifest.Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	return source
}

func renderBenchmarkSurfaces(count int) []Surface {
	result := make([]Surface, count)
	for i := range result {
		name := fmt.Sprintf("portal%d", i)
		result[i] = Surface{Name: name, Routes: []foundryhttp.RouteID{foundryhttp.RouteID(name)}}
	}
	return result
}

func BenchmarkRender(b *testing.B) {
	for _, workload := range renderWorkloads {
		if workload.surfaces != 0 {
			continue
		}
		b.Run(workload.name, func(b *testing.B) {
			source := renderBenchmarkManifest(b, workload.types, workload.routes, workload.chain)
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				if _, err := Render(source); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkRenderModules(b *testing.B) {
	for _, workload := range renderWorkloads {
		if workload.chain {
			continue
		}
		b.Run(workload.name, func(b *testing.B) {
			source := renderBenchmarkManifest(b, workload.types, workload.routes, false)
			surfaces := renderBenchmarkSurfaces(workload.surfaces)
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				if _, err := renderModules(source, "api", surfaces); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkReceivedNaming(b *testing.B) {
	source := renderBenchmarkManifest(b, 512, 1, true)
	document, err := source.Snapshot()
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, err := newNaming(document); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkGenerateCheck(b *testing.B) {
	source := renderBenchmarkManifest(b, 128, 256, false)
	options := Options{Dir: b.TempDir(), Prefix: "api", Surfaces: renderBenchmarkSurfaces(8), OpenAPI: openapi.Options{Title: "Benchmark", APIVersion: "1"}}
	if _, err := Generate(b.Context(), source, options); err != nil {
		b.Fatal(err)
	}
	options.Check = true
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, err := Generate(b.Context(), source, options); err != nil {
			b.Fatal(err)
		}
	}
}

// The hashes let performance comparisons also prove complete output identity,
// including every entry's imports, manifest, declarations and shared runtime.
func TestRenderPerformanceFixtures(t *testing.T) {
	for _, workload := range renderWorkloads {
		t.Run(workload.name, func(t *testing.T) {
			source := renderBenchmarkManifest(t, workload.types, workload.routes, workload.chain)
			data, err := source.JSON()
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("manifest bytes=%d sha256=%x", len(data), sha256.Sum256(data))
			first, err := Render(source)
			if err != nil {
				t.Fatal(err)
			}
			second, err := Render(source)
			if err != nil || !bytes.Equal(first, second) {
				t.Fatal("unstable inlined client", err)
			}
			t.Logf("inlined bytes=%d sha256=%x", len(first), sha256.Sum256(first))
			surfaces := renderBenchmarkSurfaces(workload.surfaces)
			files, err := renderModules(source, "api", surfaces)
			if err != nil {
				t.Fatal(err)
			}
			again, err := renderModules(source, "api", surfaces)
			if err != nil {
				t.Fatal(err)
			}
			names := make([]string, 0, len(files))
			for name := range files {
				names = append(names, name)
			}
			slices.Sort(names)
			for _, name := range names {
				if !bytes.Equal(files[name], again[name]) {
					t.Fatal("unstable module", name)
				}
				t.Logf("%s bytes=%d sha256=%x", name, len(files[name]), sha256.Sum256(files[name]))
			}
		})
	}
}
