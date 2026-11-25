package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/internal/apicomment"
)

func write(t *testing.T, root, name, text string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestShrinkFlagRemovesOnlyAndTheCheckThenPasses(t *testing.T) {
	root := t.TempDir()
	write(t, root, "go.mod", "module github.com/weiloon1234/Foundry-Go\n\ngo 1.22\n")
	write(t, root, "kit/kit.go", "package kit\n\ntype Spec struct {\n\t// Name labels the spec in listings.\n\tName string\n\tSize int\n}\n\nfunc Define(Spec) {}\n")
	write(t, root, apicomment.StagedFile, apicomment.StagedHeader+"kit\tfield\tSpec.Name\nkit\tfield\tSpec.Size\n")
	if err := run(root, false); err == nil || !strings.Contains(err.Error(), "[api-comment]") || !strings.Contains(err.Error(), "Spec.Name") {
		t.Fatalf("a stale line fails the check: %v", err)
	}
	if err := run(root, true); err != nil {
		t.Fatalf("after shrinking: %v", err)
	}
	data, _ := os.ReadFile(filepath.Join(root, apicomment.StagedFile))
	if string(data) != apicomment.StagedHeader+"kit\tfield\tSpec.Size\n" {
		t.Fatalf("shrunk file: %q", data)
	}
	write(t, root, "kit/kit.go", "package kit\n\ntype Spec struct {\n\tName string\n\tSize int\n\tMore int\n}\n\nfunc Define(Spec) {}\n")
	if err := run(root, true); err == nil || !strings.Contains(err.Error(), "Spec.More") {
		t.Fatalf("shrinking does not list a new field; it still fails: %v", err)
	}
	data, _ = os.ReadFile(filepath.Join(root, apicomment.StagedFile))
	if strings.Contains(string(data), "More") {
		t.Fatalf("the flag added a line: %q", data)
	}
}
