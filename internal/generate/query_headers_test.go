package generate

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

const headerQuerySource = `package sample
import "github.com/weiloon1234/Foundry-Go/value"
type Version string
//foundry:enum
type Platform string
const Mobile Platform = "mobile"
type Packed []string
func (Packed) MarshalText() ([]byte, error) { return nil, nil }
func (*Packed) UnmarshalText([]byte) error { return nil }
//foundry:query
type Input struct {
 Search value.Optional[string] ` + "`query:\"X-App-Version\"`" + `
 Version Version ` + "`header:\"x-app-VERSION\" client:\"label=app.version\"`" + `
 Platform value.Optional[Platform] ` + "`header:\"X-Platform\"`" + `
 Packed Packed ` + "`header:\"X-Packed\"`" + `
 OptionalPacked value.Optional[Packed] ` + "`header:\"X-Optional-Packed\"`" + `
 Page value.Optional[uint16]
 Ignored map[string]any ` + "`header:\"-\" form:\"ignored\"`" + `
}
//foundry:query
type HeadersOnly struct { Version string ` + "`header:\"X-Version\"`" + ` }
var Parameters = InputDescriptor()
`

func TestQueryHeaderGeneration(t *testing.T) {
	dir := fixture(t, headerQuerySource)
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	first := generatedSnapshot(t, dir)
	output := first["input_foundry.gen.go"]
	for _, want := range []string{
		"HeaderParam[Input, Version](\"X-App-Version\"", "StringQuery[Version]",
		"OptionalHeaderParam[Input, Platform]", "EnumQuery[Platform, *Platform]",
		"HeaderParam[Input, Packed]", "OptionalHeaderParam[Input, Packed]", "TextQuery[Packed, *Packed]",
		"OptionalQueryParam[Input, string](\"X-App-Version\"", "OptionalQueryParam[Input, uint16](\"page\"",
		"WithPresentation", "app.version", "sync.OnceValue",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("header output missing %q:\n%s", want, output)
		}
	}
	for _, unwanted := range []string{"Ignored", "RepeatedQueryParam", "ValidationFields"} {
		if strings.Contains(output, unwanted) {
			t.Fatalf("header output acquired %q", unwanted)
		}
	}
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, generatedSnapshot(t, dir)) {
		t.Fatal("header output or ownership manifest changed on repeat generation")
	}
	if _, err := Generate(t.Context(), Options{Dir: dir, Check: true}); err != nil {
		t.Fatal(err)
	}
	write(t, dir, "headers_test.go", `package sample_test
import (
 "testing"
 sample "foundry.test/generator"
 foundryhttp "github.com/weiloon1234/Foundry-Go/http"
)
func TestHeaderMetadata(t *testing.T) {
 headers,err:=foundryhttp.RequestHeaderParameters(sample.InputDescriptor())
 if err!=nil || len(headers)!=4 {t.Fatalf("headers: %+v, %v",headers,err)}
 if headers[0].Name!="X-App-Version" || !headers[0].Required || headers[0].Presentation.LabelKey!="app.version" {t.Fatal("required header metadata lost")}
 if headers[1].Required || headers[3].Required || headers[3].Scalar==nil || headers[3].Scalar.Syntax!=foundryhttp.EnumURLSyntax {t.Fatal("optional scalar metadata lost")}
 query,err:=foundryhttp.QueryParameters(sample.InputDescriptor())
 if err!=nil || len(query)!=2 || query[0].Name!="X-App-Version" || query[1].Name!="page" {t.Fatalf("query projection: %+v, %v",query,err)}
 only:=sample.HeadersOnlyDescriptor()
 query,err=foundryhttp.QueryParameters(only)
 if err!=nil || len(query)!=0 {t.Fatalf("header-only query projection: %+v, %v",query,err)}
 headers,err=foundryhttp.RequestHeaderParameters(only)
 if err!=nil || len(headers)!=1 || headers[0].Name!="X-Version" {t.Fatalf("header-only metadata: %+v, %v",headers,err)}
}
`)
	command := exec.CommandContext(t.Context(), "go", "test", ".")
	command.Dir = dir
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("generated header consumer failed: %v\n%s", err, output)
	}
}

func TestInvalidQueryHeaderDeclarationsDoNotPublish(t *testing.T) {
	for name, want := range map[string]string{
		"empty_name":          "HTTP header name is empty or too long",
		"invalid_name":        "HTTP header name contains an invalid byte",
		"reserved_name":       "request header is owned by the framework or transport",
		"duplicate_name":      "multiple fields bind the same header parameter",
		"conflicting_sources": "cannot declare both query and header tags",
		"query_skip":          "cannot declare both query and header tags",
		"header_skip":         "cannot declare both query and header tags",
		"both_skipped":        "cannot declare both query and header tags",
		"form_source":         "query fields cannot declare form tags",
		"pointer":             "header fields require concrete values, not pointers",
		"optional_pointer":    "header fields require concrete values, not pointers",
		"nullable":            "header text fields have no implicit null representation",
		"optional_nullable":   "header text fields have no implicit null representation",
		"slice":               "header fields require one scalar; slice repetition is unsupported",
		"optional_slice":      "header fields require one scalar; slice repetition is unsupported",
		"unsupported_codec":   "unsupported header field",
		"partial_codec":       "header text codec requires both MarshalText and UnmarshalText",
		"invalid_codec":       "header text methods must implement",
		"partial_slice_codec": "header text codec requires both MarshalText and UnmarshalText",
		"options":             "unknown or repeated header tag option",
		"unexported":          "header fields must be exported and non-embedded",
		"embedded":            "header fields must be exported and non-embedded",
		"form":                "form fields cannot declare header tags",
		"multipart":           "form fields cannot declare header tags",
		"path":                "path fields cannot declare header tags",
		"dto":                 "DTO fields cannot declare header tags",
		"nested_dto":          "DTO fields cannot declare header tags",
		"embedded_dto":        "DTO fields cannot declare header tags",
	} {
		t.Run(name, func(t *testing.T) {
			source, err := os.ReadFile(filepath.Join("testdata", "query_headers", name+".go"))
			if err != nil {
				t.Fatal(err)
			}
			dir := fixture(t, string(source))
			_, err = Generate(t.Context(), Options{Dir: dir})
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("expected %q, got %v", want, err)
			}
			line := strings.Count(string(source[:strings.LastIndex(string(source), "header:")]), "\n") + 1
			if !strings.Contains(err.Error(), fmt.Sprintf("models.go:%d:", line)) || !regexp.MustCompile(`models\.go:[0-9]+:[0-9]+:`).MatchString(err.Error()) {
				t.Fatalf("header diagnostic has no source position: %v", err)
			}
			if len(generatedSnapshot(t, dir)) != 0 {
				t.Fatal("invalid header declaration published output")
			}
		})
	}
}

func TestQueryHeaderSourceChangesInvalidateOutput(t *testing.T) {
	source := "package sample\n//foundry:query\ntype Input struct { Field string `query:\"X-Field\"` }\n"
	dir := fixture(t, source)
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	before := generatedSnapshot(t, dir)
	write(t, dir, "models.go", strings.Replace(source, "query:", "header:", 1))
	if _, err := Generate(t.Context(), Options{Dir: dir, Check: true}); err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("source change was not stale: %v", err)
	}
	if !reflect.DeepEqual(before, generatedSnapshot(t, dir)) {
		t.Fatal("stale check published output")
	}
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	after := generatedSnapshot(t, dir)
	if !strings.Contains(after["input_foundry.gen.go"], "HeaderParam[Input, string]") || after[manifestName] == before[manifestName] {
		t.Fatal("source change did not update the binding and ownership manifest")
	}
	write(t, dir, "models.go", source)
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, generatedSnapshot(t, dir)) {
		t.Fatal("returning to query source did not restore original output")
	}
}
