package generate

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// Real Go discovery supplies the metadata under test, but a guarded Go wrapper
// refuses every export-data request before any compiler or tool hook can run.
func guardGraphExports(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the guarded Go wrapper requires a Unix shell")
	}
	tool, err := exec.LookPath("go")
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	marker := filepath.Join(bin, "export-arguments")
	wrapper := `#!/bin/sh
for argument do
    if [ "$argument" = "-deps" ]; then
        printf '%s\n' "$@" > "$FOUNDRY_GRAPH_TEST_EXPORT_CALL"
        exit 86
    fi
done
exec "$FOUNDRY_GRAPH_TEST_GO" "$@"
`
	if err := os.WriteFile(filepath.Join(bin, "go"), []byte(wrapper), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FOUNDRY_GRAPH_TEST_GO", tool)
	t.Setenv("FOUNDRY_GRAPH_TEST_EXPORT_CALL", marker)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return marker
}

func TestGraphRejectsImportOptionsBeforeExportSubprocess(t *testing.T) {
	for _, path := range []string{"-a", "-toolexec", "-toolexec=foundry-test-forbidden", "--"} {
		for _, origin := range []string{"source", "extra", "generated", "recursive"} {
			t.Run(origin+"/"+path, func(t *testing.T) {
				source := "package sample\n"
				var extra []string
				if origin == "source" {
					source += "import " + strconv.Quote(path) + "\n"
				} else if origin == "extra" {
					extra = []string{path}
				}
				dir := fixture(t, source)
				name := "models.go"
				switch origin {
				case "generated":
					name = "old_foundry.gen.go"
					write(t, dir, name, generatedHeader+"\npackage sample\nimport "+strconv.Quote(path)+"\n")
				case "recursive":
					nested := filepath.Join(dir, "models")
					if err := os.Mkdir(nested, 0o755); err != nil {
						t.Fatal(err)
					}
					name = "models/invalid.go"
					write(t, nested, "invalid.go", "package models\nimport "+strconv.Quote(path)+"\n")
				}
				before := graphSnapshot(t, dir)
				marker := guardGraphExports(t)
				_, err := listGraph(t.Context(), dir, origin == "recursive", extra...)
				if err == nil || !strings.Contains(err.Error(), "invalid Go import path "+strconv.Quote(path)) || !strings.Contains(err.Error(), "leading dash") {
					t.Fatalf("flag-shaped import was not rejected: %v", err)
				}
				if origin != "extra" && !strings.Contains(err.Error(), name+":") {
					t.Fatalf("malformed import has no source location: %v", err)
				}
				if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("malformed import reached the export subprocess: %v", err)
				}
				if !reflect.DeepEqual(before, graphSnapshot(t, dir)) {
					t.Fatal("malformed import changed generated output")
				}
			})
		}
	}
}

func TestGraphImportPreflightKeepsCgoAndPlatformPackagesExcluded(t *testing.T) {
	dir := fixture(t, "package sample\n")
	for name, source := range map[string]string{
		"native":       "package native\nimport \"C\"\nfunc Version() int { return 1 }\n",
		"platformonly": "//go:build foundry_never_selected\n\npackage platformonly\nimport \"-toolexec=foundry-test-forbidden\"\n",
	} {
		packageDir := filepath.Join(dir, name)
		if err := os.Mkdir(packageDir, 0o755); err != nil {
			t.Fatal(err)
		}
		write(t, packageDir, "source.go", source)
	}
	g, err := listGraph(t.Context(), dir, true)
	if err != nil {
		t.Fatal("import preflight rejected an excluded package", err)
	}
	if len(g.packages) != 1 {
		t.Fatalf("excluded packages entered the graph: %v", sortedNames(g.packages))
	}
}

func TestGraphListsValidImportsWithMissingGeneratedTypes(t *testing.T) {
	source := strings.Replace(sampleSource, "package sample", "package sample\nimport \"bytes\"\nvar buffer bytes.Buffer", 1)
	dir := fixture(t, source)
	g, err := listGraph(t.Context(), dir, false, "strings")
	if err != nil {
		t.Fatal("valid dependency operands were rejected", err)
	}
	for _, path := range []string{"bytes", "strings"} {
		if _, err := g.Import(path); err != nil {
			t.Fatal("valid import has no compiled export data", err)
		}
	}
	// The handwritten Draft method and initializer refer to UserDraft before
	// it exists on disk; the complete generated overlay resolves both.
	if _, err := g.prepare(t.Context()); err != nil {
		t.Fatal("missing generated types prevented graph preparation", err)
	}
}
