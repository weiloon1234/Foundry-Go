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
			name: "completed milestone future work",
			change: func(t *testing.T, root string) {
				writeTestFile(t, root, "docs/guides/lifecycle.md", "Events, outbox and audit remain milestone 01 work.\n")
			},
			wantError: "future-work claim names completed milestone 01",
		},
		{
			name: "wrapped planned milestone link",
			change: func(t *testing.T, root string) {
				writeTestFile(t, root, "docs/guides/pagination.md", "Automatic HTTP parsing is planned in\n[milestone 01](../../blueprint/01-next.md).\n")
			},
			wantError: "future-work claim names completed milestone 01",
		},
		{
			name: "completed milestone remains open",
			change: func(t *testing.T, root string) {
				writeTestFile(t, root, "README.md", "Milestone 01 remains open for additional source-parity work.\n")
			},
			wantError: "future-work claim names completed milestone 01",
		},
		{
			name: "missing authoritative status table",
			change: func(t *testing.T, root string) {
				writeTestFile(t, root, "blueprint/00-master-architecture-and-parity.md", "# Master\n")
			},
			wantError: "missing completed milestone status rows",
		},
		{
			name: "history deferred work and code keep their meaning",
			change: func(t *testing.T, root string) {
				writeTestFile(t, root, "docs/guides/current.md", "Milestone 01 passed acceptance. This API remains milestone 25 work.\n\nUse `remain milestone 01 work` as a historical search example.\n\n```md\nEvents remain milestone 01 work.\n```\n")
				writeTestFile(t, root, "docs/guides/review-20261019.md", "Events remain milestone 01 work.\n")
				writeTestFile(t, root, "docs/guides/review-2026-10-19.md", "Events remain milestone 01 work.\n")
				writeTestFile(t, root, "blueprint/01-next.md", "Events remain milestone 01 work.\n")
			},
		},
		{
			name: "missing linked document",
			change: func(t *testing.T, root string) {
				writeTestFile(t, root, "README.md", "[Missing](missing.md)\n")
			},
			wantError: `local link "missing.md"`,
		},
		{
			name: "existing link outside repository",
			change: func(t *testing.T, root string) {
				writeTestFile(t, filepath.Dir(root), "outside.md", "# Outside\n")
				writeTestFile(t, root, "README.md", "[Inside](blueprint/01-next.md)\n[Outside](../outside.md)\n")
			},
			wantError: `local link "../outside.md": target is outside repository root`,
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
				writeTestFile(t, root, "README.md", "Use `NewIDAt[User](clock.Now())` and ``literal ` [not](a-link) ``. [Blueprint](blueprint/00-master-architecture-and-parity.md)\n")
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
	writeTestFile(t, root, "README.md", "[Blueprint](blueprint/00-master-architecture-and-parity.md)\n")
	writeTestFile(t, root, "blueprint/00-master-architecture-and-parity.md", "# Master\n[Next](01-next.md)\n\n| Milestone | Blueprint | Depends on | Status |\n| 01 | [Next](01-next.md) | 00 | Complete |\n| 25 | Deferred extensions | Future work | Deferred |\n")
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
