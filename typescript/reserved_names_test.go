package typescript

import (
	"go/ast"
	"go/parser"
	"go/token"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/contract/manifest"
)

// TestReservedNamesPreserveConsumerTypeNames pins the sorted reserved names.
// FOUNDRY_UPDATE_RESERVED_NAMES=1 rewrites the baseline from reservedNames.
func TestReservedNamesPreserveConsumerTypeNames(t *testing.T) {
	const path = "testdata/reserved-names.txt"
	names, err := reservedNames()
	if err != nil {
		t.Fatal(err)
	}
	got := slices.Sorted(maps.Keys(names))
	if os.Getenv("FOUNDRY_UPDATE_RESERVED_NAMES") == "1" {
		if err := os.WriteFile(path, []byte(strings.Join(got, "\n")+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	baseline, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
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

// TestReservedNamesComeFromCode reserves what the generated module's code
// declares or refers to, and nothing that only comments, strings, property
// names or type parameters spell.
func TestReservedNamesComeFromCode(t *testing.T) {
	names, err := reservedNames()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"FieldBinding", "FormFieldState", "WireCodec", "Promise", "Record", "Response", "ReadonlyArray", "FieldMessageKey"} {
		if !names[name] {
			t.Errorf("%s is declared or referred to by generated code but not reserved", name)
		}
	}
	for _, name := range standardGlobalNames {
		if !names[name] {
			t.Errorf("standard global type %s is not reserved", name)
		}
	}
	if len(slices.Compact(slices.Sorted(slices.Values(standardGlobalNames)))) != len(standardGlobalNames) {
		t.Error("standardGlobalNames lists a name twice")
	}
	// Comment words, a timeout's string name, request limit keys and type
	// parameters.
	for _, name := range []string{"Nothing", "TimeoutError", "Bytes", "Whole", "LabelPrefix", "TClient", "T", "Name"} {
		if names[name] {
			t.Errorf("%s is reserved although no generated code declares or refers to it", name)
		}
	}
	code := string(codeText([]byte("const a = \"Quoted\" + `Text ${Inside} More` // Comment\n/* Block */ const b = /[A-Z]Pattern/g; const c = x / Divided / 2;\n")))
	for _, gone := range []string{"Quoted", "Text", "More", "Comment", "Block", "Pattern"} {
		if strings.Contains(code, gone) {
			t.Errorf("codeText kept %q: %q", gone, code)
		}
	}
	for _, kept := range []string{"Inside", "Divided"} {
		if !strings.Contains(code, kept) {
			t.Errorf("codeText dropped code %q: %q", kept, code)
		}
	}
}

// TestRenderedNamesListTheRendererDeclarations fails when the renderer emits a
// module-scope name that renderedNames lacks: a capitalized declaration spelled
// in the renderer's own string literals (a name completed by a format verb is
// checked in rendered output instead), or one in the module rendered from each
// consumer fixture manifest (tables, realtime and public configuration
// included) that neither the runtime, a consumer schema nor a realtime
// channel declares. Every listed name must also appear in one of them.
func TestRenderedNamesListTheRendererDeclarations(t *testing.T) {
	listed := make(map[string]bool)
	for _, name := range renderedNames {
		listed[name] = true
	}
	runtimeDeclared := make(map[string]bool)
	entries, err := runtimeSources.ReadDir("runtime")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		data, err := runtimeSources.ReadFile("runtime/" + entry.Name())
		if err != nil {
			t.Fatal(err)
		}
		for _, match := range declaration.FindAllSubmatch(codeText(data), -1) {
			runtimeDeclared[string(match[4])] = true
		}
	}
	emitted := make(map[string]bool)
	check := func(name, where string) {
		emitted[name] = true
		if capitalized([]byte(name)) && !listed[name] && !runtimeDeclared[name] {
			t.Errorf("%s declares %s at module scope, which renderedNames does not list", where, name)
		}
	}
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), file, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(parsed, func(node ast.Node) bool {
			literal, ok := node.(*ast.BasicLit)
			if !ok || literal.Kind != token.STRING {
				return true
			}
			text, err := strconv.Unquote(literal.Value)
			if err != nil {
				t.Fatal(err)
			}
			for _, bounds := range declaration.FindAllStringSubmatchIndex(text, -1) {
				if end := bounds[9]; end < len(text) && text[end] == '%' {
					continue
				}
				check(text[bounds[8]:bounds[9]], file)
			}
			return true
		})
	}
	for _, path := range []string{
		"../tests/fixtures/consumer/clientcontracts/testdata/current-contracts/client/contracts_manifest_foundry.gen.json",
		"../tests/fixtures/consumer/clientcontracts/testdata/current-contracts/reporting/contracts_manifest_foundry.gen.json",
	} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		source, err := manifest.Decode(data)
		if err != nil {
			t.Fatal(err)
		}
		document, err := source.Snapshot()
		if err != nil {
			t.Fatal(err)
		}
		names, err := newNaming(document)
		if err != nil {
			t.Fatal(err)
		}
		consumer := make(map[string]bool)
		for _, name := range names.names {
			consumer[name] = true
		}
		for _, name := range names.received {
			consumer[name] = true
		}
		output, err := Render(source)
		if err != nil {
			t.Fatal(err)
		}
		for _, match := range declaration.FindAllSubmatch(codeText(output), -1) {
			if name := string(match[4]); !consumer[name] && !strings.HasPrefix(name, "Channel_") {
				check(name, filepath.Base(filepath.Dir(path))+" module")
			}
		}
	}
	for _, name := range renderedNames {
		if !emitted[name] && !runtimeDeclared[name] {
			t.Errorf("renderedNames lists %s, which the renderer does not declare", name)
		}
	}
}

// TestRenderNamesSchemasAgainstReservedNames qualifies a schema that would
// shadow a global the module uses or a standard global type, and keeps a name
// that no rule reserves.
func TestRenderNamesSchemasAgainstReservedNames(t *testing.T) {
	schema := contract.Schema{Root: "consumer/api.Envelope", Types: []contract.Type{
		{ID: "consumer/api.Envelope", Kind: contract.ObjectKind, Properties: []contract.Property{
			{Name: "promise", Type: "consumer/api.Promise", Required: true},
			{Name: "nothing", Type: "consumer/api.Nothing", Required: true},
			{Name: "request", Type: "github.com/weiloon1234/Foundry-Go/datatable.Request", Required: true},
			{Name: "name", Type: "github.com/weiloon1234/Foundry-Go/httpclient.Name", Required: true},
		}},
		{ID: "consumer/api.Promise", Kind: contract.StringKind},
		{ID: "consumer/api.Nothing", Kind: contract.StringKind},
		{ID: "github.com/weiloon1234/Foundry-Go/datatable.Request", Kind: contract.StringKind},
		{ID: "github.com/weiloon1234/Foundry-Go/httpclient.Name", Kind: contract.StringKind},
	}}
	source, err := manifest.Build(t.Context(), manifest.Sources{Schemas: []contract.Schema{schema}})
	if err != nil {
		t.Fatal(err)
	}
	output, err := Render(source)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`readonly "consumer/api.Promise": Api_Promise;`, `readonly "consumer/api.Nothing": Nothing;`, `readonly "github.com/weiloon1234/Foundry-Go/datatable.Request": Datatable_Request;`, `readonly "github.com/weiloon1234/Foundry-Go/httpclient.Name": Name;`} {
		if !strings.Contains(string(output), want) {
			t.Fatal("schema name does not follow the reserved names", want)
		}
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
