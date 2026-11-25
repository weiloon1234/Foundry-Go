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

const emptyLedger = "{}\n"

func newRoot(t *testing.T, source string) string {
	t.Helper()
	root := t.TempDir()
	writeSource(t, root, "go.mod", "module github.com/weiloon1234/Foundry-Go\n\ngo 1.22\n")
	writeSource(t, root, "kit/kit.go", source)
	writeSource(t, root, retirement.LedgerFile, emptyLedger)
	writeSource(t, root, retirement.ReleaseSurfaceFile, "# Recorded at release v1.0.0.\n")
	writeSource(t, root, retirement.ReleaseLedgerFile, "\n")
	return root
}

func TestGenerateCheckAndReleaseCycle(t *testing.T) {
	root := newRoot(t, "package kit\n\nfunc Keep() {}\nfunc Drop() {}\n")

	if err := run(root, "", true, ""); err == nil || !strings.Contains(err.Error(), "internal/retirement/exported-surface.txt: [surface-stale] the recorded exported API is stale") {
		t.Fatalf("a missing record must fail the check: %v", err)
	}
	// Record a release, then remove.
	if err := run(root, "", false, "v1.1.0"); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{retirement.SurfaceFile, retirement.ReleaseSurfaceFile, retirement.ReleaseLedgerFile} {
		if _, err := os.Stat(filepath.Join(root, name)); err != nil {
			t.Fatalf("release did not record %s: %v", name, err)
		}
	}
	release, _ := os.ReadFile(filepath.Join(root, retirement.ReleaseSurfaceFile))
	if !strings.HasPrefix(string(release), "# Recorded at release v1.1.0.\n") {
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
	// The ledger of the root decides, not the one built into the binary.
	writeSource(t, root, retirement.LedgerFile, `{"kit":[{"name":"Drop","noReplacement":true}]}`)
	if err := run(root, "", true, ""); err != nil {
		t.Fatalf("an entry in the root's own ledger covers the removal: %v", err)
	}
}

func TestReleaseRefusesWhileARemovalHasNoEntry(t *testing.T) {
	root := newRoot(t, "package kit\n\nfunc Keep() {}\nfunc Drop() {}\n")
	if err := run(root, "", false, "v1.1.0"); err != nil {
		t.Fatal(err)
	}
	writeSource(t, root, "kit/kit.go", "package kit\n\nfunc Keep() {}\n")
	baseline, _ := os.ReadFile(filepath.Join(root, retirement.ReleaseSurfaceFile))
	err := run(root, "", false, "v1.2.0")
	if err == nil || !strings.Contains(err.Error(), "kit.Drop (func) was removed since the previous release") {
		t.Fatalf("a release must run the removal check first: %v", err)
	}
	after, _ := os.ReadFile(filepath.Join(root, retirement.ReleaseSurfaceFile))
	if string(after) != string(baseline) {
		t.Fatal("a refused release must not replace the baseline")
	}
	writeSource(t, root, retirement.LedgerFile, `{"kit":[{"name":"Drop","noReplacement":true}]}`)
	if err := run(root, "", false, "v1.2.0"); err != nil {
		t.Fatalf("with the entry the release records: %v", err)
	}
	after, _ = os.ReadFile(filepath.Join(root, retirement.ReleaseSurfaceFile))
	if !strings.HasPrefix(string(after), "# Recorded at release v1.2.0.\n") {
		t.Fatalf("baseline header: %q", after)
	}
	names, _ := os.ReadFile(filepath.Join(root, retirement.ReleaseLedgerFile))
	if string(names) != "kit\tDrop\n" {
		t.Fatalf("the release records the root's ledger names: %q", names)
	}
}

func TestFlagsAreExclusiveAndTagIsValidated(t *testing.T) {
	root := newRoot(t, "package kit\n\nfunc Keep() {}\n")
	for name, call := range map[string]func() error{
		"check and out":     func() error { return run(root, filepath.Join(root, "x"), true, "") },
		"check and release": func() error { return run(root, "", true, "v1.1.0") },
		"out and release":   func() error { return run(root, filepath.Join(root, "x"), false, "v1.1.0") },
	} {
		if err := call(); err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
			t.Errorf("%s: %v", name, err)
		}
	}
	for _, tag := range []string{"1.1.0", "v1.1", "v1.1.0-rc1", "v1.1.0\n# injected"} {
		if err := run(root, "", false, tag); err == nil || !strings.Contains(err.Error(), "vMAJOR.MINOR.PATCH") {
			t.Errorf("tag %q: %v", tag, err)
		}
	}
	if _, err := os.Stat(filepath.Join(root, retirement.SurfaceFile)); err == nil {
		t.Fatal("a refused invocation must write nothing")
	}
}
