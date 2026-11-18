package generate

import (
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestHTTPHelperSelection(t *testing.T) {
	p := &packageInput{fset: token.NewFileSet()}
	for _, role := range []string{"", dtoResponseRole} {
		for _, explicit := range []string{"", "body", "response", "both"} {
			want := explicit
			if want == "" {
				want = "none"
			}
			got, err := p.httpMode(token.NoPos, role, explicit, false)
			if err != nil || got != want {
				t.Fatalf("role=%q http=%q: %s %v", role, explicit, got, err)
			}
		}
	}
	for _, blocked := range []bool{false, true} {
		p.httpDependency = blocked
		if got, err := p.httpMode(token.NoPos, "", "", !blocked); err != nil || got != "none" {
			t.Fatal(got, err)
		}
		if _, err := p.httpMode(token.NoPos, "", "body", !blocked); err == nil {
			t.Fatal("unsupported helper was accepted")
		}
	}
	set := httpDependencySet([]string{framework + "/auth", framework + "/internal/authtransport", "example/application/requests", "context"})
	if !set[framework+"/http"] || !set[framework+"/internal/authtransport"] || set["example/application/requests"] {
		t.Fatal(set)
	}
}

func TestHTTPNoneIsNotADirectiveValue(t *testing.T) {
	for _, directive := range []string{"dto", "multipart"} {
		dir := fixture(t, "package sample\n//foundry:"+directive+" http=none\ntype SaveRequest struct{}\n")
		if _, err := generateFixture(t.Context(), Options{Dir: dir}); err == nil || !strings.Contains(err.Error(), "http") {
			t.Fatalf("%s accepted http=none: %v", directive, err)
		}
	}
}

func TestConcreteHTTPHelpersPreserveDeclarations(t *testing.T) {
	dir := fixture(t, `package sample
//foundry:dto http=both
type SaveRequest struct { Name string `+"`json:\"name\"`"+` }
//foundry:dto role=response http=body
type ReplyResource struct { Name string `+"`json:\"name\"`"+` }
//foundry:dto
type Nested struct { Count int `+"`json:\"count\"`"+` }
`)
	if _, err := generateFixture(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	first := generatedSnapshot(t, dir)
	for _, want := range []string{"func SaveBody()", "func SaveResponse(status int)", "//go:noinline", "SaveRequestJSON()", "SaveRequestValidationFields()"} {
		if !strings.Contains(first["save_request_foundry.gen.go"], want) {
			t.Fatal("missing", want)
		}
	}
	if strings.Contains(first["reply_resource_foundry.gen.go"], "ValidationFields") || !strings.Contains(first["reply_resource_foundry.gen.go"], "func ReplyBody()") {
		t.Fatal("http direction changed response-role semantics")
	}
	if strings.Contains(first["nested_foundry.gen.go"], "/http") {
		t.Fatal("default imported HTTP")
	}
	if _, err := generateFixture(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, generatedSnapshot(t, dir)) {
		t.Fatal("helper generation was not deterministic")
	}
}

func TestHTTPHelperNameCollisions(t *testing.T) {
	for _, source := range []string{
		"//foundry:dto http=body\ntype SaveRequest struct{}\nfunc SaveBody(){}",
		"//foundry:dto http=body\ntype SaveRequest struct{}\n//foundry:dto http=body\ntype SaveResource struct{}",
	} {
		dir := fixture(t, "package sample\n"+source)
		if _, err := generateFixture(t.Context(), Options{Dir: dir}); err == nil || !strings.Contains(err.Error(), "SaveBody") || !strings.Contains(err.Error(), "declaration") {
			t.Fatalf("collision: %v", err)
		}
		if len(generatedSnapshot(t, dir)) != 0 {
			t.Fatal("collision published output")
		}
	}
}

func TestExplicitOnlyDefaultAndMultipartDirectoryHelper(t *testing.T) {
	dir := fixture(t, `package sample
import h "`+framework+`/http"
//foundry:dto
type UnusedRequest struct { Name string `+"`json:\"name\"`"+` }
//foundry:dto role=response
type UnusedResource struct { Name string `+"`json:\"name\"`"+` }
//foundry:multipart http=body
type UploadRequest struct { File h.UploadedFile `+"`form:\"file\"`"+` }
`)
	if _, err := generateFixture(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	files := generatedSnapshot(t, dir)
	for _, name := range []string{"unused_request_foundry.gen.go", "unused_resource_foundry.gen.go"} {
		if strings.Contains(files[name], framework+"/http") {
			t.Fatalf("default emitted an unused helper in %s", name)
		}
	}
	for _, want := range []string{"func UploadBody(directory string)", "MultipartDirectory(UploadRequestDescriptor(), directory)", "//go:noinline"} {
		if !strings.Contains(files["upload_request_foundry.gen.go"], want) {
			t.Fatal("missing", want)
		}
	}
}

func TestHTTPHelperTestSymbolCollisions(t *testing.T) {
	for _, test := range []struct {
		name, source string
		collision    bool
	}{
		{"same package", "package sample\nfunc SaveBody(){}", true},
		{"build constrained", "//go:build never\n\npackage sample\nvar SaveBody int", true},
		{"import alias", "package sample\nimport SaveBody \"fmt\"", true},
		{"default import name", "package sample\nimport \"foundry.test/generator/transport\"", true},
		{"external namespace", "package sample_test\nfunc SaveBody(){}", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := fixture(t, "package sample\n//foundry:dto http=body\ntype SaveRequest struct{}\n")
			write(t, dir, "helper_test.go", test.source)
			if err := os.Mkdir(filepath.Join(dir, "transport"), 0755); err != nil {
				t.Fatal(err)
			}
			write(t, dir, "transport/name.go", "package SaveBody\n")
			_, err := generateFixture(t.Context(), Options{Dir: dir})
			if test.collision {
				if err == nil || !strings.Contains(err.Error(), "SaveBody") || !strings.Contains(err.Error(), "helper_test.go") || !strings.Contains(err.Error(), "models.go") {
					t.Fatalf("collision diagnostic: %v", err)
				}
				if len(generatedSnapshot(t, dir)) != 0 {
					t.Fatal("collision published output")
				}
			} else if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestFrameworkPackagesDoNotEmitHTTPHelpers(t *testing.T) {
	p := &packageInput{fset: token.NewFileSet(), frameworkPackage: true}
	if mode, err := p.httpMode(token.NoPos, "", "", false); err != nil || mode != "none" {
		t.Fatal(mode, err)
	}
	if _, err := p.httpMode(token.NoPos, "", "body", false); err == nil || !strings.Contains(err.Error(), "application modules") {
		t.Fatal(err)
	}
}

func TestTestCollisionInputsAreGuarded(t *testing.T) {
	dir := fixture(t, "package sample\n//foundry:dto http=body\ntype SaveRequest struct{}\n")
	write(t, dir, "helper_test.go", "package sample\nfunc unrelated(){}\n")
	graph, err := listGraph(t.Context(), dir, false)
	if err != nil {
		t.Fatal(err)
	}
	writes, err := graph.prepare(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	write(t, dir, "helper_test.go", "package sample\nfunc SaveBody(){}\n")
	if err := publishBatch(t.Context(), dir, writes, graph.checkSourceTree, nil); err == nil || !strings.Contains(err.Error(), "helper_test.go changed") {
		t.Fatalf("changed test source was not guarded: %v", err)
	}
	if len(generatedSnapshot(t, dir)) != 0 {
		t.Fatal("published after test source changed")
	}
}
