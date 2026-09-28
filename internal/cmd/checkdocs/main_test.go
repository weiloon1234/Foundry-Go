package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRepositoryChecks(t *testing.T) {
	tests := []struct {
		name      string
		change    func(t *testing.T, root string)
		wantError string
	}{
		{
			name:   "valid repository",
			change: func(t *testing.T, root string) {},
		},
		{
			name: "missing linked document",
			change: func(t *testing.T, root string) {
				writeTestFile(t, root, "README.md", "[Missing](missing.md)\n")
			},
			wantError: `local link "missing.md"`,
		},
		{
			name: "gap in blueprint sequence",
			change: func(t *testing.T, root string) {
				if err := os.Rename(filepath.Join(root, "blueprint/01-next.md"), filepath.Join(root, "blueprint/02-next.md")); err != nil {
					t.Fatal(err)
				}
			},
			wantError: "expected milestone 01",
		},
		{
			name: "duplicate milestone",
			change: func(t *testing.T, root string) {
				writeTestFile(t, root, "blueprint/00-other.md", "# Duplicate\n")
			},
			wantError: "missing or duplicate number",
		},
		{
			name: "fixture version drift",
			change: func(t *testing.T, root string) {
				writeTestFile(t, root, "tests/fixtures/consumer/go.mod", "module example.test/consumer\n\ngo 1.19.0\n")
			},
			wantError: "differs from root",
		},
		{
			name: "missing fixture Go requirement",
			change: func(t *testing.T, root string) {
				writeTestFile(t, root, "tests/fixtures/consumer/go.mod", "module example.test/consumer\n")
			},
			wantError: "missing Go requirement",
		},
		{
			name: "remote links fragments and fenced examples",
			change: func(t *testing.T, root string) {
				writeTestFile(t, root, "README.md", "[Go](https://go.dev/)\n[Section](#section)\n```md\n[Example](not-created.md)\n```\n~~~md\n[Example](also-not-created.md)\n~~~\n")
			},
		},
		{
			name: "inline generics are code rather than links",
			change: func(t *testing.T, root string) {
				writeTestFile(t, root, "README.md", "Use `NewIDAt[User](clock.Now())` and ``literal ` [not](a-link) ``. [Blueprint](blueprint/00-master.md)\n")
			},
		},
		{
			name: "inline code does not hide missing links",
			change: func(t *testing.T, root string) {
				writeTestFile(t, root, "README.md", "`New[T](arg)` [Missing](missing.md)\n")
			},
			wantError: `local link "missing.md"`,
		},
		{
			name: "relative encoded path and fragment",
			change: func(t *testing.T, root string) {
				writeTestFile(t, root, "docs/usage guide.md", "# Usage\n")
				writeTestFile(t, root, "blueprint/01-next.md", "[Usage](../docs/usage%20guide.md#usage)\n")
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			root := testRepository(t)
			test.change(t, root)
			err := checkRepository(root)
			if test.wantError == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("expected error containing %q, got %v", test.wantError, err)
			}
		})
	}
}

func testRepository(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	// Synthetic module files are read as data; they are never built or installed.
	writeTestFile(t, root, "go.mod", "module example.test/framework\n\ngo 1.20.0\n")
	writeTestFile(t, root, "tests/fixtures/consumer/go.mod", "module example.test/consumer\n\ngo 1.20.0\n")
	writeTestFile(t, root, "README.md", "[Blueprint](blueprint/00-master.md)\n")
	writeTestFile(t, root, "blueprint/00-master.md", "# Master\n[Next](01-next.md)\n")
	writeTestFile(t, root, "blueprint/01-next.md", "# Next\n")
	return root
}

func writeTestFile(t *testing.T, root, name, content string) {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
