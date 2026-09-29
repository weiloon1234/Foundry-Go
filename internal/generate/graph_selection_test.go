package generate

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestGraphSkipsCgoAndPlatformPackagesWithoutDeclarations(t *testing.T) {
	dir := graphFixture(t)
	for name, files := range map[string]map[string]string{
		"native":               {"native.go": "package native\n\nimport \"C\"\n\nfunc Version() int { return 1 }\n"},
		"platformonly":         {"linux.go": "//go:build foundry_never_selected\n\npackage platformonly\n"},
		"node_modules/tooling": {"tool.go": "not valid Go"},
		"ignored":              {"broken.go": "not valid Go"},
	} {
		for file, data := range files {
			if err := os.MkdirAll(filepath.Join(dir, name), 0o755); err != nil {
				t.Fatal(err)
			}
			write(t, filepath.Join(dir, name), file, data)
		}
	}
	module, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	write(t, dir, "go.mod", string(module)+"\nignore ./ignored\n")
	report, err := Generate(t.Context(), Options{Dir: dir, Recursive: true})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(report.Written, []string{"domain/user_foundry.gen.go", "state/state_foundry.gen.go"}) {
		t.Fatalf("outputs = %+v", report.Written)
	}
	if _, err := Generate(t.Context(), Options{Dir: dir, Recursive: true, Check: true}); err != nil {
		t.Fatal(err)
	}
}

func TestGraphRejectsDeclarationsInCgoAndConstrainedFiles(t *testing.T) {
	for name, test := range map[string]struct{ file, source string }{
		"cgo":         {"native.go", "package native\n\nimport \"C\"\n\n//foundry:enum\ntype Mode string\n\nconst Fast Mode = \"fast\"\n"},
		"constrained": {"native_linux.go", "//go:build foundry_never_selected\n\npackage native\n\n//foundry:enum\ntype Mode string\n\nconst Fast Mode = \"fast\"\n"},
	} {
		t.Run(name, func(t *testing.T) {
			dir := graphFixture(t)
			native := filepath.Join(dir, "native")
			if err := os.Mkdir(native, 0o755); err != nil {
				t.Fatal(err)
			}
			write(t, native, "doc.go", "package native\n")
			write(t, native, test.file, test.source)
			_, err := Generate(t.Context(), Options{Dir: dir, Recursive: true})
			if err == nil || !strings.Contains(err.Error(), "native/"+test.file+":") {
				t.Fatalf("declaration location not reported: %v", err)
			}
			if len(graphSnapshot(t, dir)) != 0 {
				t.Fatal("rejected graph published output")
			}
		})
	}
}

// Moving a declaration behind a build constraint must not look like deletion:
// its owned output stays and generation explains the platform exclusion.
func TestConstrainedDeclarationKeepsOwnedOutput(t *testing.T) {
	dir := fixture(t, "package sample\n\n//foundry:enum\ntype Mode string\n\nconst Fast Mode = \"fast\"\n")
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	before := generatedSnapshot(t, dir)
	write(t, dir, "models.go", "package sample\n")
	write(t, dir, "mode_platform.go", "//go:build foundry_never_selected\n\npackage sample\n\n//foundry:enum\ntype Mode string\n\nconst Fast Mode = \"fast\"\n")
	if _, err := Generate(t.Context(), Options{Dir: dir}); err == nil || !strings.Contains(err.Error(), "mode_platform.go") || !strings.Contains(err.Error(), "build constraints") {
		t.Fatalf("constrained declaration accepted: %v", err)
	}
	if !reflect.DeepEqual(before, generatedSnapshot(t, dir)) {
		t.Fatal("constrained declaration removed its owned output")
	}
}

func TestModuleScopeIgnorePatternsMatchGo(t *testing.T) {
	scope := moduleScope{root: "/module", ignore: []string{"./web", "assets/cache"}}
	for dir, want := range map[string]bool{
		"/module/web":                  true,
		"/module/web/app":              true,
		"/module/api/web":              false,
		"/module/assets/cache":         true,
		"/module/api/assets/cache/sub": true,
		"/module/assets":               false,
		"/module":                      false,
		"/elsewhere/web":               false,
	} {
		if got := scope.ignored(filepath.FromSlash(dir)); got != want {
			t.Errorf("ignored(%s) = %t, want %t", dir, got, want)
		}
	}
	if !excludedDirectory("/module", filepath.FromSlash("/module/web/node_modules/pkg")) || excludedDirectory("/module", filepath.FromSlash("/module/modules")) {
		t.Fatal("node_modules exclusion is wrong")
	}
}
