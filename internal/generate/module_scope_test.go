package generate

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadModuleScopeRelativeDirectories(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	write(t, root, "go.mod", "module example.test/application\n")
	subpackage := filepath.Join(root, "transport")
	if err := os.Mkdir(subpackage, 0755); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct{ name, cwd, dir string }{
		{"default in subpackage", subpackage, "."},
		{"relative subpackage", root, "transport/../transport"},
		{"relative module root", filepath.Dir(root), "./" + filepath.Base(root) + "/."},
		{"absolute", subpackage, root},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Chdir(test.cwd)
			scope, err := loadModuleScope(t.Context(), test.dir)
			if err != nil {
				t.Fatal(err)
			}
			if scope.root != root || scope.path != "example.test/application" {
				t.Fatalf("unexpected module scope: %+v", scope)
			}
		})
	}
}
