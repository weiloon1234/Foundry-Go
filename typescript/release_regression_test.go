package typescript

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/contract/manifest"
	"github.com/weiloon1234/Foundry-Go/datatable"
	"github.com/weiloon1234/Foundry-Go/fault"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/i18n"
	"github.com/weiloon1234/Foundry-Go/validation"
)

func TestTableOperationsDoNotBorrowAnotherTableEndpoint(t *testing.T) {
	r := renderer{document: manifest.Document{HTTP: []manifest.Operation{
		{Name: "reportsMembersQuery", Route: foundryhttp.RouteInfo{ID: "reports.members.query"}, Body: &manifest.Payload{Type: "request"}},
		{Name: "otherReportsOrdersQuery", Route: foundryhttp.RouteInfo{ID: "other.reports.orders.query"}, Body: &manifest.Payload{Type: "request"}},
	}}}
	query, export, err := r.tableOperations(manifest.Table{ID: datatable.TableID("reports.orders"), Request: "request"})
	if err != nil {
		t.Fatal(err)
	}
	if query != "" || export != "" {
		t.Fatalf("orders borrowed another table's endpoint: %q %q", query, export)
	}
}

func TestPublicConfigRejectsInvalidUnionFallback(t *testing.T) {
	for _, fallback := range []string{"", "other"} {
		t.Run("fallback="+fallback, func(t *testing.T) {
			source, err := manifest.Build(t.Context(), manifest.Sources{PublicConfig: []manifest.PublicSetting{{Name: "mode", Env: "VITE_MODE", Kind: manifest.PublicEnum, EnumCases: []string{"live"}, Fallback: fallback}}})
			if err != nil {
				t.Fatal(err)
			}
			if _, err = Render(source); err == nil {
				t.Fatal("invalid enum fallback emitted under a literal union")
			}
		})
	}
}

func TestPublicConfigNumericFallbackParity(t *testing.T) {
	cases := []struct {
		Value  string  `json:"value"`
		Valid  bool    `json:"valid"`
		Number float64 `json:"number"`
	}{
		{Value: "0x10", Valid: true, Number: 16},
		{Value: "0x1p2"},
		{Value: "0Xff", Valid: true, Number: 255},
		{Value: "0b101", Valid: true, Number: 5},
		{Value: "0B11", Valid: true, Number: 3},
		{Value: "0o10", Valid: true, Number: 8},
		{Value: "0O17", Valid: true, Number: 15},
		{Value: "0x10000000000000000", Valid: true, Number: 18446744073709551616},
		{Value: "0", Valid: true},
		{Value: "-0", Valid: true},
		{Value: "-12.5", Valid: true, Number: -12.5},
		{Value: "+.5", Valid: true, Number: .5},
		{Value: "1.", Valid: true, Number: 1},
		{Value: "01", Valid: true, Number: 1},
		{Value: "1e2", Valid: true, Number: 100},
		{Value: "1E-2", Valid: true, Number: .01},
		{Value: "1e-999", Valid: true},
		{Value: "1_000"},
		{Value: "0x1_0"},
		{Value: "+0x10"},
		{Value: "-0x10"},
		{Value: "0x"},
		{Value: "0b2"},
		{Value: "0o8"},
		{Value: "1e"},
		{Value: "NaN"},
		{Value: "Infinity"},
		{Value: "1e999"},
		{Value: "0x" + strings.Repeat("f", 256)},
	}
	settings := []manifest.PublicSetting{{Name: "number", Env: "VITE_NUMBER", Kind: manifest.PublicNumber, Fallback: "1"}}
	for i, test := range cases {
		t.Run(test.Value, func(t *testing.T) {
			setting := manifest.PublicSetting{Name: "number", Env: "VITE_NUMBER", Kind: manifest.PublicNumber, Fallback: test.Value}
			source, err := manifest.Build(t.Context(), manifest.Sources{PublicConfig: []manifest.PublicSetting{setting}})
			if err != nil {
				t.Fatal(err)
			}
			_, err = Render(source)
			if (err == nil) != test.Valid {
				t.Fatalf("fallback %q: Render error = %v, want valid = %v", test.Value, err, test.Valid)
			}
		})
		if test.Valid {
			settings = append(settings, manifest.PublicSetting{Name: fmt.Sprintf("number%d", i), Env: fmt.Sprintf("VITE_NUMBER_%d", i), Kind: manifest.PublicNumber, Fallback: test.Value})
		}
	}
	source, err := manifest.Build(t.Context(), manifest.Sources{PublicConfig: settings})
	if err != nil {
		t.Fatal(err)
	}
	output, err := Render(source)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(cases)
	if err != nil {
		t.Fatal(err)
	}
	runReleaseTypeScript(t, output, `
const cases = `+string(data)+`;
const defaults = sdk.decodePublicEnv({});
for (const [i, test] of cases.entries()) {
  assert.equal(sdk.publicEnvSchema.VITE_NUMBER.isValid(test.value), test.valid, test.value);
  assert.equal(sdk.isValidPublicValue("VITE_NUMBER", test.value), test.valid, test.value);
  assert.equal(sdk.findPublicEnvProblems({VITE_NUMBER: test.value}, "VITE_").length, test.valid ? 0 : 1, test.value);
  if (test.valid) {
    const expected = test.value === "-0" ? -0 : test.number;
    assert.equal(defaults["number" + i], expected, test.value);
    assert.equal(sdk.decodePublicEnv({VITE_NUMBER: test.value}).number, expected, test.value);
  } else {
    assert.throws(() => sdk.decodePublicEnv({VITE_NUMBER: test.value}), /VITE_NUMBER is invalid/, test.value);
  }
}
assert.equal(sdk.decodePublicEnv({VITE_NUMBER: ""}).number, 1);
assert.equal(sdk.decodePublicEnv({VITE_NUMBER: "  "}).number, 1);
assert.equal(sdk.decodePublicEnv({VITE_NUMBER: " 0x10 "}).number, 16);
assert.equal(Object.is(sdk.decodePublicEnv({VITE_NUMBER: "-0"}).number, -0), true);
`)
}

func TestPublicConfigLocaleMatchesCatalog(t *testing.T) {
	locales, err := i18n.NewLocaleSet("en", "en", "ms")
	if err != nil {
		t.Fatal(err)
	}
	source, err := manifest.Build(t.Context(), manifest.Sources{Locales: locales, PublicConfig: []manifest.PublicSetting{{Name: "locale", Env: "VITE_LOCALE", Kind: manifest.PublicLocale, Fallback: "en"}}})
	if err != nil {
		t.Fatal(err)
	}
	document, err := source.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	document.Locales = &manifest.Locales{Default: locales.Default(), Supported: locales.Locales()}
	data, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	source, err = manifest.Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	output, err := Render(source)
	if err != nil {
		t.Fatal(err)
	}
	runReleaseTypeScript(t, output, `assert.equal(sdk.isValidPublicValue("VITE_LOCALE", "de"), false); assert.throws(() => sdk.decodePublicEnv({VITE_LOCALE:"de"})); assert.equal(sdk.decodePublicEnv({}).locale,"en"); assert.equal(sdk.decodePublicEnv({VITE_LOCALE:"ms"}).locale,"ms");`)
}

func TestPublicConfigOptionalValues(t *testing.T) {
	locales, err := i18n.NewLocaleSet("en", "en", "ms")
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := i18n.NewCatalog(t.Context(), locales, i18n.CatalogOptions{}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	settings := []manifest.PublicSetting{
		{Name: "text", Env: "VITE_TEXT", Kind: manifest.PublicString, Optional: true, MaxLength: 4},
		{Name: "enabled", Env: "VITE_ENABLED", Kind: manifest.PublicBoolean, Optional: true},
		{Name: "number", Env: "VITE_NUMBER", Kind: manifest.PublicNumber, Optional: true},
		{Name: "mode", Env: "VITE_MODE", Kind: manifest.PublicEnum, Optional: true, EnumCases: []string{"live", "test"}},
		{Name: "locale", Env: "VITE_LOCALE", Kind: manifest.PublicLocale, Optional: true},
		{Name: "currency", Env: "VITE_CURRENCY", Kind: manifest.PublicCurrency, Optional: true},
		{Name: "timeZone", Env: "VITE_TIME_ZONE", Kind: manifest.PublicTimeZone, Optional: true},
		{Name: "required", Env: "VITE_REQUIRED", Kind: manifest.PublicString, Fallback: "default"},
	}
	source, err := manifest.Build(t.Context(), manifest.Sources{Catalog: catalog, PublicConfig: settings})
	if err != nil {
		t.Fatal(err)
	}
	output, err := Render(source)
	if err != nil {
		t.Fatal(err)
	}
	output = append(output, []byte(`
type Equal<A, B> = (<T>() => T extends A ? 1 : 2) extends (<T>() => T extends B ? 1 : 2) ? true : false;
type Assert<T extends true> = T;
type OptionalPublicConfigTypes = [
  Assert<Equal<PublicEnv["text"], string | undefined>>,
  Assert<Equal<PublicEnv["enabled"], boolean | undefined>>,
  Assert<Equal<PublicEnv["number"], number | undefined>>,
  Assert<Equal<PublicEnv["mode"], "live" | "test" | undefined>>,
  Assert<Equal<PublicEnv["locale"], Locale | undefined>>,
  Assert<Equal<PublicEnv["currency"], string | undefined>>,
  Assert<Equal<PublicEnv["timeZone"], string | undefined>>,
  Assert<Equal<PublicEnv["required"], string>>
];
`)...)
	runReleaseTypeScript(t, output, `
const cases = [
  ["VITE_TEXT", "text", " ok ", "ok", "private-invalid-text"],
  ["VITE_ENABLED", "enabled", " false ", false, "private-invalid-boolean"],
  ["VITE_NUMBER", "number", " 0 ", 0, "private-invalid-number"],
  ["VITE_MODE", "mode", " live ", "live", "private-invalid-mode"],
  ["VITE_LOCALE", "locale", " ms ", "ms", "de"],
  ["VITE_CURRENCY", "currency", " USD ", "USD", "private-invalid-currency"],
  ["VITE_TIME_ZONE", "timeZone", " UTC ", "UTC", "private-invalid-zone"],
];
for (const [key, name, raw, expected, invalid] of cases) {
  for (const env of [{}, {[key]: undefined}, {[key]: ""}, {[key]: " \t\n"}]) {
    const decoded = sdk.decodePublicEnv(env);
    assert.equal(decoded[name], undefined, key);
    assert.equal(Object.hasOwn(decoded, name), true);
    assert.equal(Object.isFrozen(decoded), true);
    assert.equal(decoded.required, "default");
  }
  assert.equal(sdk.decodePublicEnv({[key]: raw})[name], expected, key);
  assert.deepEqual(sdk.findPublicEnvProblems({[key]: ""}, "VITE_"), []);
  assert.equal(sdk.findPublicEnvProblems({[key]: undefined}, "VITE_").length, 1);
  assert.equal(sdk.findPublicEnvProblems({[key]: invalid}, "VITE_").length, 1);
  assert.throws(() => sdk.decodePublicEnv({[key]: invalid}), error => {
    assert.match(error.message, new RegExp(key + " is invalid"));
    assert.equal(error.message.includes(invalid), false);
    return true;
  });
}
assert.equal(sdk.decodePublicEnv({VITE_ENABLED: "0"}).enabled, false);
assert.equal(sdk.decodePublicEnv({VITE_ENABLED: "1"}).enabled, true);
assert.equal(sdk.decodePublicEnv({VITE_ENABLED: "true"}).enabled, true);
assert.equal(sdk.decodePublicEnv({VITE_NUMBER: "0x10"}).number, 16);
assert.equal(sdk.decodePublicEnv({VITE_REQUIRED: " "}).required, "default");
assert.equal(sdk.decodePublicEnv({VITE_REQUIRED: " supplied "}).required, "supplied");
`)
}

func TestPublicConfigOptionalEnumRequiresCases(t *testing.T) {
	source, err := manifest.Build(t.Context(), manifest.Sources{PublicConfig: []manifest.PublicSetting{{Name: "mode", Env: "VITE_MODE", Kind: manifest.PublicEnum, Optional: true}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Render(source); !errors.Is(err, fault.Invalid) {
		t.Fatalf("optional enum without cases emitted: %v", err)
	}
}

func TestExtensionOnlyUploadAcceptHint(t *testing.T) {
	type form struct{ Document foundryhttp.UploadedFile }
	field := validation.DefineField("document", func(f form) foundryhttp.UploadedFile { return f.Document })
	body := foundryhttp.MultipartDirectory(foundryhttp.DefineMultipart(foundryhttp.FilePart("document", func(f *form) *foundryhttp.UploadedFile { return &f.Document })), t.TempDir())
	endpoint := foundryhttp.ValidateBody(foundryhttp.DefineEndpoint(foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "upload", Method: foundryhttp.POST, Access: foundryhttp.Public}, foundryhttp.StaticPath("/upload")), foundryhttp.EmptyQuery(), foundryhttp.MultipartBody(body), foundryhttp.JSONResponse(200, contract.StringJSON[string]())), validation.Rules(field, validation.FileExtensions[foundryhttp.UploadedFile]("csv")))
	router, err := foundryhttp.NewRouter(endpoint.Handle(func(context.Context, foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, form]) (string, error) {
		return "", nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	source, err := manifest.Build(t.Context(), manifest.Sources{HTTP: router})
	if err != nil {
		t.Fatal(err)
	}
	output, err := Render(source)
	if err != nil {
		t.Fatal(err)
	}
	runReleaseTypeScript(t, output, `assert.equal(sdk.operation("upload").field("body", "document").accept,".csv");`)
}

func runReleaseTypeScript(t *testing.T, source []byte, assertions string) {
	t.Helper()
	node, compiler := os.Getenv("FOUNDRY_TEST_NODE"), os.Getenv("FOUNDRY_TEST_TYPESCRIPT")
	if node == "" || compiler == "" {
		t.Skip("set FOUNDRY_TEST_NODE and FOUNDRY_TEST_TYPESCRIPT")
	}
	dir := t.TempDir()
	input := filepath.Join(dir, "input.ts")
	if err := os.WriteFile(input, source, 0600); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"type":"module"}`), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, node, compiler, "--target", "ES2022", "--module", "NodeNext", "--moduleResolution", "NodeNext", "--lib", "ES2022,DOM,DOM.Iterable", "--strict", "--exactOptionalPropertyTypes", "--noUncheckedIndexedAccess", "--outDir", filepath.Join(dir, "dist"), input)
	command.WaitDelay = time.Second
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("strict generated TypeScript: %v\n%s", err, output)
	}
	script := `import assert from "node:assert/strict"; import {pathToFileURL} from "node:url"; const sdk=await import(pathToFileURL(process.argv[1])); ` + assertions
	command = exec.CommandContext(ctx, node, "--input-type=module", "-e", script, filepath.Join(dir, "dist", "input.js"))
	command.WaitDelay = time.Second
	if output, err := command.CombinedOutput(); err != nil {
		text := string(output)
		if len(text) > 5000 {
			text = text[len(text)-5000:]
		}
		t.Fatalf("generated runtime: %v\n%s", err, text)
	}
}

func TestTableOperationBindingsValidateContracts(t *testing.T) {
	table := manifest.Table{ID: "reports.members", Request: "request", Row: "member", Exports: true}
	query := manifest.Operation{Name: "membersQuery", Route: foundryhttp.RouteInfo{ID: "reports.members.query"}, Body: &manifest.Payload{MediaType: "application/json", Type: "request"}, Response: &manifest.Payload{MediaType: "application/json", Type: "page"}}
	types := []contract.Type{{ID: "page", Kind: contract.ObjectKind, Properties: []contract.Property{{Name: "data", Type: "rows", Required: true}}}, {ID: "rows", Kind: contract.ArrayKind, Element: "member"}}
	for _, scenario := range []string{"valid", "wrong request", "wrong row", "ambiguous query", "ambiguous export", "export only"} {
		t.Run(scenario, func(t *testing.T) {
			table := table
			op := query
			r := renderer{document: manifest.Document{Types: types, HTTP: []manifest.Operation{op}}}
			switch scenario {
			case "wrong request":
				op.Body = &manifest.Payload{MediaType: "application/json", Type: "other"}
				r.document.HTTP[0] = op
			case "wrong row":
				table.Row = "other"
			case "ambiguous query":
				op.Route.ID = "reports.members:query"
				r.document.HTTP = append(r.document.HTTP, op)
			case "ambiguous export", "export only":
				op = manifest.Operation{Name: "membersExport", Route: foundryhttp.RouteInfo{ID: "reports.members.csv"}, Response: &manifest.Payload{File: &foundryhttp.FileResponseInfo{}}}
				r.document.HTTP = []manifest.Operation{op}
				if scenario == "ambiguous export" {
					op.Route.ID = "reports.members.export"
					r.document.HTTP = append(r.document.HTTP, op)
				}
			}
			queryName, exportName, err := r.tableOperations(table)
			switch scenario {
			case "valid":
				if err != nil || queryName != "membersQuery" || exportName != "" {
					t.Fatalf("valid binding: %q %q %v", queryName, exportName, err)
				}
			case "export only":
				if err != nil || queryName != "" || exportName != "membersExport" {
					t.Fatalf("export binding: %q %q %v", queryName, exportName, err)
				}
				r.renderTableEntry(table, queryName, exportName, "Member")
				if !strings.Contains(r.out.String(), `createTableDescriptor<Member, never, "membersExport">`) {
					t.Fatal("export-only descriptor lost its operation type")
				}
			default:
				if !errors.Is(err, fault.Invalid) {
					t.Fatalf("invalid table binding accepted: %v", err)
				}
			}
		})
	}
}
