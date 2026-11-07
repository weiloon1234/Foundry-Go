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

func TestRegionBoundaryPaddingUsesSameRenderAndCheck(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "tests/fixtures/consumer/example/example.go")
	guide := filepath.Join(root, "docs/guides/example.md")
	for _, file := range []string{source, guide} {
		if err := os.MkdirAll(filepath.Dir(file), 0755); err != nil {
			t.Fatal(err)
		}
	}
	body := "//foundry:dto\ntype Value struct{}\n\nvar Other = 2\n"
	original := "<!-- compiled-source: tests/fixtures/consumer/example/example.go#value -->\n```go\n" + body + "```\n"
	if err := os.WriteFile(guide, []byte(original), 0644); err != nil {
		t.Fatal(err)
	}
	for _, padding := range []string{"\n", "\n \t\n\n"} {
		input := "package example\n// doc-example:begin value\n" + padding + body + padding + "// doc-example:end value\n"
		if err := os.WriteFile(source, []byte(input), 0644); err != nil {
			t.Fatal(err)
		}
		for _, check := range []bool{true, false, true} {
			if err := run(root, check); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(guide)
			if err != nil || string(data) != original {
				t.Fatalf("boundary padding changed fence: %q, %v", data, err)
			}
			data, err = os.ReadFile(source)
			if err != nil || string(data) != input {
				t.Fatal("renderer modified source", err)
			}
		}
	}
	stale := strings.Replace(original, "struct{}\n\nvar", "struct{}\nvar", 1)
	if err := os.WriteFile(guide, []byte(stale), 0644); err != nil {
		t.Fatal(err)
	}
	if err := run(root, true); err == nil {
		t.Fatal("check ignored an interior blank-line difference")
	}
	data, err := os.ReadFile(guide)
	if err != nil || string(data) != stale {
		t.Fatal("check modified stale guide", err)
	}
	if err := run(root, false); err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile(guide)
	if err != nil || string(data) != original {
		t.Fatal("render did not preserve the interior blank line", err)
	}
}
