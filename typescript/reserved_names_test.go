package typescript

import (
	"maps"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/contract/manifest"
)

func TestReservedNamesPreserveConsumerTypeNames(t *testing.T) {
	// Frozen from renderedNames and runtime sources at fefe9551, before fetch.ts.
	baseline, err := os.ReadFile("testdata/reserved-names.txt")
	if err != nil {
		t.Fatal(err)
	}
	names, err := reservedNames()
	if err != nil {
		t.Fatal(err)
	}
	got := slices.Sorted(maps.Keys(names))
	if strings.Join(got, "\n")+"\n" != string(baseline) {
		want := strings.Fields(string(baseline))
		var added, removed []string
		for _, name := range got {
			if !slices.Contains(want, name) {
				added = append(added, name)
			}
		}
		for _, name := range want {
			if !names[name] {
				removed = append(removed, name)
			}
		}
		t.Fatalf("reservedNames changed: added %v, removed %v; changing reserved names renames consumer types and needs a compatibility note before updating the sorted baseline", added, removed)
	}
}

func TestRenderPreservesFetchSchemaName(t *testing.T) {
	schema := contract.Schema{Root: "consumer/api.Fetch", Types: []contract.Type{
		{ID: "consumer/api.Fetch", Kind: contract.StringKind},
	}}
	source, err := manifest.Build(t.Context(), manifest.Sources{Schemas: []contract.Schema{schema}})
	if err != nil {
		t.Fatal(err)
	}
	output, err := Render(source)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"export type Fetch = string;", `readonly "consumer/api.Fetch": Fetch;`} {
		if !strings.Contains(string(output), want) {
			t.Fatal("consumer schema Fetch lost its exported name", want)
		}
	}
}
