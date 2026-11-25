package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/internal/retirement"
)

func writeSource(t *testing.T, root, name, text string) {
	t.Helper()
	if err := write(root, name, []byte(text)); err != nil {
		t.Fatal(err)
	}
}

func TestGenerateCheckAndReleaseCycle(t *testing.T) {
	root := t.TempDir()
	writeSource(t, root, "go.mod", "module github.com/weiloon1234/Foundry-Go\n\ngo 1.22\n")
	writeSource(t, root, "kit/kit.go", "package kit\n\nfunc Keep() {}\nfunc Drop() {}\n")

	if err := run(root, "", true, ""); err == nil || !strings.Contains(err.Error(), "the recorded exported API is stale") {
		t.Fatalf("a missing record must fail the check: %v", err)
	}
	// The temporary root has no ledger of its own, so removals are judged by
	// the embedded one; nothing is removed yet. Record a release, then remove.
	if err := run(root, "", false, "v1.0.0"); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{retirement.SurfaceFile, retirement.ReleaseSurfaceFile, retirement.ReleaseLedgerFile} {
		if _, err := os.Stat(filepath.Join(root, name)); err != nil {
			t.Fatalf("release did not record %s: %v", name, err)
		}
	}
	release, _ := os.ReadFile(filepath.Join(root, retirement.ReleaseSurfaceFile))
	if !strings.HasPrefix(string(release), "# Recorded at release v1.0.0.\n") {
		t.Fatalf("release header: %q", release)
	}
	if err := run(root, "", true, ""); err != nil {
		t.Fatalf("a fresh record must pass: %v", err)
	}
	writeSource(t, root, "kit/kit.go", "package kit\n\nfunc Keep() {}\n")
	if err := run(root, "", true, ""); err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("a changed API must make the record stale: %v", err)
	}
	err := run(root, "", false, "")
	if err == nil || !strings.Contains(err.Error(), "kit.Drop (func) was removed since the previous release; add it to internal/retirement/denied.json") {
		t.Fatalf("generating must name the unledgered removal: %v", err)
	}
	if err := run(root, "", true, ""); err == nil || strings.Contains(err.Error(), "stale") {
		t.Fatalf("the regenerated record is fresh but the removal is still unledgered: %v", err)
	}
}
