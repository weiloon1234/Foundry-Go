package doctor

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// docs check proves Go membership with this listing, so the invocation must
// fill the file lists without resolving dependencies and must leave out files
// the default build does not select.
func TestModulePackagesFillsFileListsAndHonoursBuildTags(t *testing.T) {
	root := t.TempDir()
	write := func(name, text string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(filepath.Join(root, name)), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, name), []byte(text), 0644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module example.test/app\n\ngo 1.22\n")
	write("internal/pages/page.go", "package pages\n\nimport _ \"example.test/missing/dependency\"\n")
	write("internal/pages/page_test.go", "package pages\n")
	write("internal/pages/page_external_test.go", "package pages_test\n")
	write("internal/pages/tagged.go", "//go:build neverselected\n\npackage pages\n")
	write("nested/go.mod", "module example.test/nested\n\ngo 1.22\n")
	write("nested/nested.go", "package nested\n")
	data, err := ModulePackages(context.Background(), Options{Dir: root, Timeout: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	var found struct {
		Dir                                          string
		GoFiles, CgoFiles, TestGoFiles, XTestGoFiles []string
		Incomplete                                   bool
	}
	if err := json.NewDecoder(bytes.NewReader(data)).Decode(&found); err != nil {
		t.Fatal(err)
	}
	resolved, _ := filepath.EvalSymlinks(root)
	if found.Dir != filepath.Join(resolved, "internal", "pages") || len(found.GoFiles) != 1 || found.GoFiles[0] != "page.go" ||
		len(found.TestGoFiles) != 1 || found.TestGoFiles[0] != "page_test.go" || len(found.XTestGoFiles) != 1 || found.XTestGoFiles[0] != "page_external_test.go" {
		t.Fatalf("listing = %+v", found)
	}
	if bytes.Contains(data, []byte("tagged.go")) || bytes.Contains(data, []byte("nested.go")) {
		t.Fatalf("listing names a file the default build does not compile: %s", data)
	}
}

func TestModulePackagesReportsAnUnlistableModule(t *testing.T) {
	if _, err := ModulePackages(context.Background(), Options{Dir: t.TempDir(), Timeout: time.Minute}); err == nil {
		t.Fatal("listed a directory without a module")
	}
}
