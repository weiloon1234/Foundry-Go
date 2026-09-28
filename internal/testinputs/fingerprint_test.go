package testinputs

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func put(t *testing.T, root, name, data string) {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
}
func fingerprintOf(t *testing.T, root string, tools ...Tool) string {
	t.Helper()
	digest, err := Compute(t.Context(), root, []string{"compiler-identity"}, tools)
	if err != nil {
		t.Fatal(err)
	}
	return digest
}

func TestFingerprintTracksExternalSourceAndToolsButExcludesPrivateState(t *testing.T) {
	root := t.TempDir()
	put(t, root, "go.mod", "module example.test/consumer\n")
	put(t, root, "models.go", "package models\n")
	put(t, root, "testdata/compilefail/invalid.go", "package bad\n")
	put(t, root, ".env.test", "private=one")
	put(t, root, ".cache/private.go", "private=one")
	put(t, root, "tools/node_modules/ignored.js", "ignored")
	first := fingerprintOf(t, root)
	put(t, root, ".env.test", "private=two")
	put(t, root, ".cache/private.go", "private=two")
	put(t, root, "tools/node_modules/ignored.js", "changed")
	if fingerprintOf(t, root) != first {
		t.Fatal("private/cache state changed source fingerprint")
	}
	file := filepath.Join(root, "models.go")
	future := time.Now().Add(time.Hour)
	if err := os.Chtimes(file, future, future); err != nil {
		t.Fatal(err)
	}
	if fingerprintOf(t, root) != first {
		t.Fatal("timestamp-only change invalidated tests")
	}
	put(t, root, "testdata/compilefail/invalid.go", "package changed\n")
	if fingerprintOf(t, root) == first {
		t.Fatal("compiler-child source was omitted")
	}
	before := fingerprintOf(t, root)
	put(t, root, ".foundry-gen.json", `{"version":1}`)
	if fingerprintOf(t, root) == before {
		t.Fatal("generation ownership was omitted")
	}
	toolRoot := t.TempDir()
	put(t, toolRoot, "tool", "binary-one")
	tool := Tool{Name: "selected", Path: filepath.Join(toolRoot, "tool")}
	before = fingerprintOf(t, root, tool)
	put(t, toolRoot, "tool", "binary-two")
	if fingerprintOf(t, root, tool) == before {
		t.Fatal("selected external tool changed without invalidation")
	}
	put(t, toolRoot, "typescript/lib/tsc.js", "require('./_tsc.js')")
	put(t, toolRoot, "typescript/lib/_tsc.js", "compiler-one")
	tool = Tool{Name: "typescript", Path: filepath.Join(toolRoot, "typescript"), Tree: true}
	before = fingerprintOf(t, root, tool)
	put(t, toolRoot, "typescript/lib/_tsc.js", "compiler-two")
	if fingerprintOf(t, root, tool) == before {
		t.Fatal("TypeScript compiler dependency omitted")
	}
}

func TestFingerprintRejectsSourceSymlinksAndCancellation(t *testing.T) {
	for _, name := range []string{"linked.go", "linked-package"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			outside := t.TempDir()
			put(t, outside, "source.go", "package outside\n")
			target := outside
			if name == "linked.go" {
				target = filepath.Join(outside, "source.go")
			}
			if err := os.Symlink(target, filepath.Join(root, name)); err != nil {
				t.Fatal(err)
			}
			if _, err := Compute(t.Context(), root, nil, nil); err == nil {
				t.Fatal("outside source dependency was silently omitted")
			}
		})
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := Compute(ctx, t.TempDir(), nil, nil); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestFingerprintTracksModuleRuntimeContentWithoutTimestampChanges(t *testing.T) {
	root := t.TempDir()
	name := "consumer/testdata/runtime.mjs"
	put(t, root, name, "export const value = 1;\n")
	path := filepath.Join(root, name)
	original, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	before := fingerprintOf(t, root)
	if fingerprintOf(t, root) != before {
		t.Fatal("unchanged runtime fingerprint changed")
	}
	put(t, root, name, "export const value = 2;\n")
	if err := os.Chtimes(path, original.ModTime(), original.ModTime()); err != nil {
		t.Fatal(err)
	}
	after := fingerprintOf(t, root)
	if after == before {
		t.Fatal("same-sized module runtime content was omitted")
	}
	future := original.ModTime().Add(time.Hour)
	if err := os.Chtimes(path, future, future); err != nil {
		t.Fatal(err)
	}
	if fingerprintOf(t, root) != after {
		t.Fatal("runtime timestamp invalidated unchanged content")
	}
}
