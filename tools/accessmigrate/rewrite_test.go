package main

import (
	"bytes"
	"context"
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite the golden files from the current output")

func copyTree(t *testing.T, from, to string) {
	t.Helper()
	err := filepath.WalkDir(from, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(from, path)
		if d.IsDir() {
			return os.MkdirAll(filepath.Join(to, rel), 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		// The inputs are stored as .go.txt so the repository's own checks do not
		// read their old-API spelling as a use of a retired name.
		return os.WriteFile(strings.TrimSuffix(filepath.Join(to, rel), ".txt"), data, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
}

// module copies the fixture application next to the chosen stub of the
// framework (old or new API) and returns the application directory.
func module(t *testing.T, stub string) string {
	t.Helper()
	root := t.TempDir()
	copyTree(t, filepath.Join("testdata", stub), filepath.Join(root, "framework"))
	copyTree(t, filepath.Join("testdata", "app"), filepath.Join(root, "app"))
	gomod := filepath.Join(root, "app", "go.mod")
	data, err := os.ReadFile(gomod)
	if err != nil {
		t.Fatal(err)
	}
	data = bytes.ReplaceAll(data, []byte("STUB_DIR"), []byte(filepath.Join(root, "framework")))
	if err := os.WriteFile(gomod, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(root, "app")
}

func migrate(t *testing.T, dir string, args ...string) string {
	t.Helper()
	var out, errs bytes.Buffer
	if err := run(context.Background(), append([]string{"--dir", dir}, args...), &out, &errs); err != nil {
		t.Fatalf("%v\n%s", err, errs.String())
	}
	return out.String()
}

func goCommand(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("go", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=mod", "GOWORK=off", "GOPROXY=off")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

func compare(t *testing.T, golden, got string) {
	t.Helper()
	if *update {
		if err := os.MkdirAll(filepath.Dir(golden), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(golden, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if string(want) != got {
		t.Errorf("%s differs from the current output:\n%s", golden, got)
	}
}

func TestMigratesEveryShapeAndListsWhatItLeaves(t *testing.T) {
	dir := module(t, "stubold")
	plan := migrate(t, dir)
	compare(t, filepath.Join("testdata", "golden", "plan.txt"), plan)
	for _, pkg := range []string{"migrated", "left"} {
		files, _ := filepath.Glob(filepath.Join(dir, pkg, "*.go"))
		for _, file := range files {
			got, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			compare(t, filepath.Join("testdata", "golden", pkg, filepath.Base(file)+".txt"), string(got))
		}
	}
}

func TestMigratedCodeBuildsAgainstTheNewAPIAndRunsAreIdempotent(t *testing.T) {
	dir := module(t, "stubold")
	migrate(t, dir)
	// The same application, now pinned to the release with the required rule.
	root := filepath.Dir(dir)
	if err := os.RemoveAll(filepath.Join(root, "framework")); err != nil {
		t.Fatal(err)
	}
	copyTree(t, filepath.Join("testdata", "stubnew"), filepath.Join(root, "framework"))
	goCommand(t, dir, "build", "./migrated/...")
	again := migrate(t, dir, "--dry-run", "./migrated/...")
	if !strings.Contains(again, "would rewrite 0 call(s) and 0 declaration(s) in 0 file(s); left 0 call(s) and 0 declaration(s)") {
		t.Fatalf("a migrated module changed again:\n%s", again)
	}
}

func TestDryRunWritesNothing(t *testing.T) {
	dir := module(t, "stubold")
	before, err := os.ReadFile(filepath.Join(dir, "migrated", "fold.go"))
	if err != nil {
		t.Fatal(err)
	}
	plan := migrate(t, dir, "--dry-run")
	if !strings.Contains(plan, "would rewrite") {
		t.Fatalf("dry run did not describe the rewrite:\n%s", plan)
	}
	after, _ := os.ReadFile(filepath.Join(dir, "migrated", "fold.go"))
	if !bytes.Equal(before, after) {
		t.Fatal("dry run wrote a file")
	}
}

func TestRefusesAFlagAsPackagePattern(t *testing.T) {
	var out, errs bytes.Buffer
	if err := run(context.Background(), []string{"--dir", t.TempDir(), "--", "-x"}, &out, &errs); err == nil {
		t.Fatal("accepted a flag as a package pattern")
	}
}
