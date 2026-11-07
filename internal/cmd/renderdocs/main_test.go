package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCompareIsReadOnlyAndRenderChangesOnlyFence(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "tests/fixtures/consumer/example/example.go")
	guide := filepath.Join(root, "docs/guides/example.md")
	for _, file := range []string{source, guide} {
		if err := os.MkdirAll(filepath.Dir(file), 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(source, []byte("package example\n// doc-example:begin value\nvar Value = 1\n// doc-example:end value\n"), 0644); err != nil {
		t.Fatal(err)
	}
	original := "# Example\n\n<!-- compiled-source: tests/fixtures/consumer/example/example.go#value -->\n```go\nstale\n```\n\nAuthored prose.\n"
	if err := os.WriteFile(guide, []byte(original), 0644); err != nil {
		t.Fatal(err)
	}
	if err := run(root, true); err == nil {
		t.Fatal("compare accepted stale fence")
	}
	data, err := os.ReadFile(guide)
	if err != nil || string(data) != original {
		t.Fatal("compare wrote guide", err)
	}
	if err := run(root, false); err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile(guide)
	if err != nil || string(data) != strings.Replace(original, "stale", "var Value = 1", 1) {
		t.Fatal("render changed authored content", err)
	}
	if err := run(root, true); err != nil {
		t.Fatal(err)
	}
	if err := run(root, false, "absent.md"); err == nil {
		t.Fatal("accepted missing guide")
	}
}
