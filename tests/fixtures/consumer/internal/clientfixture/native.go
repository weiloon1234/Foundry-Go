// Package clientfixture owns native strict TypeScript HTTP checks for consumers.
package clientfixture

import (
	"context"
	"encoding/json"
	"github.com/weiloon1234/Foundry-Go/contract/manifest"
	"github.com/weiloon1234/Foundry-Go/openapi"
	"github.com/weiloon1234/Foundry-Go/testkit"
	"github.com/weiloon1234/Foundry-Go/typescript"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

type Toolchain struct{ node, compiler string }

func Load(t testing.TB) Toolchain {
	t.Helper()
	testkit.TrackExternalInputs(t)
	tools := Toolchain{os.Getenv("FOUNDRY_TEST_NODE"), os.Getenv("FOUNDRY_TEST_TYPESCRIPT")}
	if tools.node == "" || tools.compiler == "" {
		if os.Getenv("FOUNDRY_TEST_TYPESCRIPT_REQUIRED") == "1" {
			t.Fatal("native TypeScript tools required")
		}
		t.Skip("native TypeScript tools not configured")
	}
	return tools
}
func (tools Toolchain) Check(t testing.TB, source *manifest.Manifest, baseURL, fixtures string) {
	tools.check(t, source, baseURL, fixtures, false)
}

// CheckFormAdapters compiles and runs the actual optional React/Vue modules.
// Their development dependencies live beside the selected TypeScript compiler.
func (tools Toolchain) CheckFormAdapters(t testing.TB, source *manifest.Manifest, fixtures string) {
	tools.check(t, source, "", fixtures, true)
}

func (tools Toolchain) check(t testing.TB, source *manifest.Manifest, baseURL, fixtures string, adapters bool) {
	t.Helper()
	dir := t.TempDir()
	options := typescript.Options{Dir: dir, OpenAPI: openapi.Options{Title: "Consumer API", APIVersion: "1"}, React: adapters, Vue: adapters}
	if adapters {
		modules := filepath.Dir(filepath.Dir(filepath.Dir(tools.compiler)))
		for _, name := range []string{"react", "react-dom", "vue", "jsdom", "@types/react", "@types/react-dom"} {
			if _, err := os.Stat(filepath.Join(modules, name, "package.json")); err != nil {
				if os.Getenv("FOUNDRY_TEST_TYPESCRIPT_REQUIRED") == "1" {
					t.Fatalf("optional adapter acceptance requires installed development package %s", name)
				}
				t.Skipf("optional adapter development package %s is not installed", name)
			}
		}
		if err := os.Symlink(modules, filepath.Join(dir, "node_modules")); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := typescript.Generate(t.Context(), source, options); err != nil {
		t.Fatal(err)
	}
	options.Check = true
	if _, err := typescript.Generate(t.Context(), source, options); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"types.ts", "runtime.mjs"} {
		data, err := os.ReadFile(filepath.Join(fixtures, name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"type":"module"}`), 0600); err != nil {
		t.Fatal(err)
	}
	config := map[string]any{"compilerOptions": map[string]any{"target": "ES2022", "module": "NodeNext", "moduleResolution": "NodeNext", "lib": []string{"ES2022", "DOM", "DOM.Iterable"}, "strict": true, "exactOptionalPropertyTypes": true, "noUncheckedIndexedAccess": true, "noEmitOnError": true, "outDir": "dist"}, "include": []string{"*.ts"}}
	data, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tsconfig.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) {
		t.Helper()
		ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
		defer cancel()
		command := exec.CommandContext(ctx, tools.node, args...)
		command.Dir = dir
		command.WaitDelay = time.Second
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("native client check: %v\n%s", err, output)
		}
	}
	run(tools.compiler, "--project", filepath.Join(dir, "tsconfig.json"))
	run(filepath.Join(dir, "runtime.mjs"), filepath.Join(dir, "dist", "contracts_foundry.gen.js"), baseURL, strconv.Itoa(manifest.Version))
}
