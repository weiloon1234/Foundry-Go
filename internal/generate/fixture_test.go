package generate

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/weiloon1234/Foundry-Go/testkit"
)

const fixtureModulePath = "foundry.test/generator"

type fixtureModule struct {
	mod, sum []byte
}

var fixtureModuleFiles = sync.OnceValues(func() (fixtureModule, error) {
	root, err := filepath.Abs("../..")
	if err != nil {
		return fixtureModule{}, err
	}
	mod, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return fixtureModule{}, err
	}
	moduleLine := "module " + framework + "\n"
	if !strings.HasPrefix(string(mod), moduleLine) {
		return fixtureModule{}, fmt.Errorf("root go.mod has an unexpected module declaration")
	}
	sum, err := os.ReadFile(filepath.Join(root, "go.sum"))
	if err != nil {
		return fixtureModule{}, err
	}
	// Retain the framework's approved requirements, toolchain and checksums.
	fixtureMod := strings.Replace(string(mod), moduleLine, "module "+fixtureModulePath+"\n", 1)
	fixtureMod += fmt.Sprintf("\nrequire %s v0.0.0\nreplace %s => %q\n", framework, framework, root)
	return fixtureModule{mod: []byte(fixtureMod), sum: sum}, nil
})

func fixture(t *testing.T, source string) string {
	t.Helper()
	testkit.TrackExternalInputs(t)
	files, err := fixtureModuleFiles()
	if err != nil {
		t.Fatal(err)
	}
	// Match Go's canonical /private/var paths on macOS.
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	write(t, dir, "go.mod", string(files.mod))
	write(t, dir, "go.sum", string(files.sum))
	write(t, dir, "models.go", source)
	return dir
}

// Rejection cases share only module inputs. Each case owns its source and any
// unexpected output, so its no-publication assertion still observes real files.
type rejectionFixtures struct {
	root string
}

func newRejectionFixtures(t *testing.T) rejectionFixtures {
	t.Helper()
	return rejectionFixtures{root: fixture(t, "package sample\n")}
}

func (fixtures rejectionFixtures) fixture(t *testing.T, source string) string {
	t.Helper()
	dir, err := os.MkdirTemp(fixtures.root, "case")
	if err != nil {
		t.Fatal(err)
	}
	fixtures.write(t, dir, "models.go", source)
	return dir
}

func (fixtures rejectionFixtures) write(t *testing.T, dir, name, source string) {
	t.Helper()
	relative, err := filepath.Rel(fixtures.root, dir)
	if err != nil || outsideRoot(relative) || relative == "." {
		t.Fatalf("rejection source is outside its case directory: %s", dir)
	}
	caseName := strings.Split(filepath.ToSlash(relative), "/")[0]
	source = strings.ReplaceAll(source, fixtureModulePath+"/", fixtureModulePath+"/"+caseName+"/")
	write(t, dir, name, source)
}

func TestRejectionFixturesKeepCasesIsolated(t *testing.T) {
	fixtures := newRejectionFixtures(t)
	source := "package sample\nimport \"foundry.test/generator/lib\"\n//foundry:dto\ntype Response struct{ Value lib.Value }\n"
	valid := fixtures.fixture(t, source)
	invalid := fixtures.fixture(t, source)
	for dir, field := range map[string]string{valid: "string", invalid: "func()"} {
		if err := os.Mkdir(filepath.Join(dir, "lib"), 0o755); err != nil {
			t.Fatal(err)
		}
		fixtures.write(t, filepath.Join(dir, "lib"), "value.go", "package lib\ntype Value struct{ Field "+field+" }\n")
	}
	if _, err := generateFixture(t.Context(), Options{Dir: valid, Recursive: true}); err != nil {
		t.Fatal(err)
	}
	before := generatedSnapshot(t, valid)
	if len(before) == 0 {
		t.Fatal("valid sibling did not exercise publication")
	}
	if _, err := generateFixture(t.Context(), Options{Dir: invalid, Recursive: true}); err == nil || !strings.Contains(err.Error(), "unsupported DTO field type") {
		t.Fatalf("invalid sibling did not retain its own declaration: %v", err)
	}
	if len(generatedSnapshot(t, invalid)) != 0 || !reflect.DeepEqual(before, generatedSnapshot(t, valid)) {
		t.Fatal("rejection published output or changed its sibling")
	}
}
