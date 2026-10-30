package devserver

import (
	"bytes"
	"context"
	"errors"
	"io"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/cli"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
)

func TestWatchedFilesTrackServerInputsOnly(t *testing.T) {
	// Glob metacharacters in the project path must not hide config/*.toml.
	root := filepath.Join(t.TempDir(), "app[1]")
	write := func(name, content string) {
		t.Helper()
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	files := []string{
		"portals.go",
		"portals_test.go",
		"cmd/artisan/main.go",
		"internal/portals/portals.go",
		"internal/portals/portals_test.go",
		"internal/.cache/skip.go",
		"internal/node_modules/skip.go",
		"internal/testdata/skip.go",
		"internal/vendor/skip.go",
		"web/portals/admin/portal.json",
		"web/portals/admin/src/main.go",
		"web/portals/admin/src/App.tsx",
		"go.mod",
		"go.sum",
		"config/app.toml",
		"local.env",
		"README.md",
	}
	for _, name := range files {
		write(name, "content-a")
	}

	envFile := filepath.Join(root, "local.env")
	extraWatched := []string{"config/*.toml", "web/portals/*/portal.json", envFile}
	w := newWatcher(root, extraWatched)
	before := w.snapshot()

	expectedWatched := []string{
		"portals.go",
		"cmd/artisan/main.go",
		"internal/portals/portals.go",
		"web/portals/admin/portal.json",
		"go.mod",
		"go.sum",
		"config/app.toml",
		"local.env",
	}

	for _, name := range expectedWatched {
		target := filepath.Join(root, filepath.FromSlash(name))
		if _, ok := before[target]; !ok {
			t.Fatalf("%s (%s) is not watched in snapshot: %v", name, target, before)
		}
	}
	if len(before) != len(expectedWatched) {
		t.Fatalf("watched %d files, want %d: %v", len(before), len(expectedWatched), before)
	}

	// Unwatched modifications must NOT trigger a change.
	write("web/portals/admin/src/main.go", "changed")
	write("web/portals/admin/src/App.tsx", "changed")
	write("portals_test.go", "changed")
	write("internal/portals/portals_test.go", "changed")
	write("internal/.cache/skip.go", "changed")
	write("internal/node_modules/skip.go", "changed")
	write("internal/testdata/skip.go", "changed")
	write("internal/vendor/skip.go", "changed")
	write("README.md", "changed")

	if !maps.Equal(before, w.snapshot()) {
		t.Fatal("changes outside the server inputs triggered a rebuild")
	}

	// Watched modifications MUST trigger a change.
	for _, input := range []string{"internal/portals/portals.go", "portals.go", "web/portals/admin/portal.json", "config/app.toml"} {
		snapshot := w.snapshot()
		write(input, "changed again")
		if maps.Equal(snapshot, w.snapshot()) {
			t.Fatalf("a change to %s was not detected", input)
		}
	}
}

func TestWatchedFilesSupportsGlobsAndDirectories(t *testing.T) {
	root := t.TempDir()
	write := func(name, content string) {
		t.Helper()
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	write("main.go", "package main")
	write("go.mod", "module test")
	write("templates/email.html", "<h1>Hello</h1>")
	write("templates/sub/nested.html", "<p>Nested</p>")
	write("assets/style.css", "body {}")

	// Extra includes a directory ("templates") and a relative glob ("assets/*.css").
	w := newWatcher(root, []string{"templates", "assets/*.css"})
	snapshot := w.snapshot()

	for _, name := range []string{"main.go", "go.mod", "templates/email.html", "templates/sub/nested.html", "assets/style.css"} {
		path := filepath.Join(root, filepath.FromSlash(name))
		if _, ok := snapshot[path]; !ok {
			t.Fatalf("file %s (%s) was not included in snapshot", name, path)
		}
	}
}

func TestPrefixWriterKeepsLinesWhole(t *testing.T) {
	var mu sync.Mutex
	var out bytes.Buffer
	vite := newPrefixWriter(&mu, &out, "[admin] ")
	server := newPrefixWriter(&mu, &out, "[server] ")

	writes := []struct {
		w    *prefixWriter
		text string
	}{
		{vite, "ready "},
		{server, "listening\n"},
		{vite, "in 90 ms\nhmr"},
		{vite, " update\n"},
	}

	for _, wr := range writes {
		if _, err := wr.w.Write([]byte(wr.text)); err != nil {
			t.Fatal(err)
		}
	}

	want := "[server] listening\n[admin] ready in 90 ms\n[admin] hmr update\n"
	if out.String() != want {
		t.Fatalf("output = %q, want %q", out.String(), want)
	}
}

func TestRebuildBuildCommandLdflags(t *testing.T) {
	root := t.TempDir()
	write := func(name, content string) {
		t.Helper()
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	write("go.mod", "module test-app\n\ngo 1.24\n")
	write("cmd/server/main.go", "package main\n\nfunc main() {}\n")

	var status bytes.Buffer
	var output bytes.Buffer

	server := devServer{
		root:    root,
		binary:  filepath.Join(root, "bin", "dev-server"),
		target:  "./cmd/server",
		status:  &status,
		output:  &output,
		watcher: newWatcher(root, nil),
		opts:    Options{Root: root},
	}

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	server.rebuild(ctx)
	defer server.stop()

	if !server.running() {
		t.Fatalf("rebuilt server is not running; status: %q; output: %q", status.String(), output.String())
	}
	if _, err := os.Stat(server.binary); err != nil {
		t.Fatalf("expected binary at %s, error: %v", server.binary, err)
	}
}

func TestRebuildPreservesPreviousServerOnFailure(t *testing.T) {
	root := t.TempDir()
	var status bytes.Buffer
	var output bytes.Buffer

	server := devServer{
		root:    root,
		binary:  filepath.Join(root, "bin", "dev-server"),
		target:  "./nonexistent-package",
		status:  &status,
		output:  &output,
		watcher: newWatcher(root, nil),
		opts:    Options{Root: root},
	}

	// Simulate an already-running server child.
	done := make(chan struct{})
	server.current = &runningServer{
		cancel: func() {},
		done:   done,
	}

	if !server.running() {
		t.Fatal("simulated server is not running")
	}

	server.rebuild(t.Context())

	// Because build failed, previous running server must be preserved.
	if !server.running() {
		t.Fatal("server stopped after failed rebuild; should keep running")
	}
	if !strings.Contains(status.String(), "the previous server keeps running") {
		t.Fatalf("status output %q did not report that the previous server keeps running", status.String())
	}
}

func TestServerExitReporting(t *testing.T) {
	var server devServer
	if server.running() {
		t.Fatal("no server started, yet one is reported running")
	}

	done := make(chan struct{})
	server.current = &runningServer{cancel: func() {}, done: done}
	if !server.running() {
		t.Fatal("started server is not reported running")
	}

	close(done)
	if server.running() {
		t.Fatal("exited server is still reported as running")
	}
}

func TestOnChangeFailureAndPanicIsolation(t *testing.T) {
	t.Run("returns ordinary error", func(t *testing.T) {
		customErr := errors.New("portal layout changed")
		fn := func() error {
			return customErr
		}
		err := callback.Isolated("devserver OnChange", fn)
		if !errors.Is(err, customErr) {
			t.Fatalf("got %v, want %v", err, customErr)
		}
	})

	t.Run("isolates panic", func(t *testing.T) {
		fn := func() error {
			panic("unexpected boom")
		}
		err := callback.Isolated("devserver OnChange", fn)
		if err == nil {
			t.Fatal("expected panic to be caught")
		}
		if !errors.Is(err, fault.Panicked) {
			t.Fatalf("expected fault.Panicked, got %v", err)
		}
	})

	t.Run("isolates goexit", func(t *testing.T) {
		fn := func() error {
			runtime.Goexit()
			return nil
		}
		err := callback.Isolated("devserver OnChange", fn)
		if err == nil {
			t.Fatal("expected Goexit to be caught")
		}
		if !errors.Is(err, fault.Panicked) {
			t.Fatalf("expected fault.Panicked, got %v", err)
		}
	})
}

func TestProcessGroupCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	// Launch a process that runs for some time (e.g. go version).
	wait, err := startChild(ctx, t.TempDir(), "go", []string{"version"}, nil, cli.Streams{
		In:  strings.NewReader(""),
		Out: io.Discard,
		Err: io.Discard,
	})
	if err != nil {
		t.Fatalf("cannot start child: %v", err)
	}

	if err := wait(); err != nil {
		t.Fatalf("wait failed: %v", err)
	}
}

func TestOptionsValidationAndSnapshots(t *testing.T) {
	t.Run("empty root", func(t *testing.T) {
		_, err := (Options{}).Validate()
		if err == nil || !errors.Is(err, fault.Invalid) {
			t.Fatalf("got %v, want fault.Invalid", err)
		}
	})

	t.Run("non-existent root", func(t *testing.T) {
		_, err := (Options{Root: "/nonexistent/path/for/sure"}).Validate()
		if err == nil || !errors.Is(err, fault.Invalid) {
			t.Fatalf("got %v, want fault.Invalid", err)
		}
	})

	t.Run("defaults and snapshots", func(t *testing.T) {
		root := t.TempDir()
		origArgs := []string{"serve", "--config", "dev.toml"}
		origEnv := []string{"ENV=1"}
		origWatched := []string{"local.env"}
		origHelpers := []Helper{{Name: "vite", Executable: "npm", Args: []string{"run", "dev"}}}

		opts := Options{
			Root:    root,
			Args:    origArgs,
			Env:     origEnv,
			Watched: origWatched,
			Helpers: origHelpers,
		}

		v, err := opts.Validate()
		if err != nil {
			t.Fatalf("validate failed: %v", err)
		}

		if v.Target != "./cmd/artisan" {
			t.Fatalf("Target = %q, want ./cmd/artisan", v.Target)
		}
		if v.Binary != filepath.Join(v.Root, "bin", "dev-server") {
			t.Fatalf("Binary = %q, want %q", v.Binary, filepath.Join(v.Root, "bin", "dev-server"))
		}
		if v.PollInterval != DefaultPollInterval {
			t.Fatalf("PollInterval = %v", v.PollInterval)
		}
		if v.SettleInterval != DefaultSettleInterval {
			t.Fatalf("SettleInterval = %v", v.SettleInterval)
		}

		// Mutate original slices to ensure snapshots are independent.
		origArgs[0] = "mutated"
		origEnv[0] = "MUTATED=1"
		origWatched[0] = "mutated.env"
		origHelpers[0].Name = "mutated"

		if v.Args[0] == "mutated" {
			t.Fatal("Args slice was not cloned")
		}
		if v.Env[0] == "MUTATED=1" {
			t.Fatal("Env slice was not cloned")
		}
		if v.Watched[0] == "mutated.env" {
			t.Fatal("Watched slice was not cloned")
		}
		if v.Helpers[0].Name == "mutated" {
			t.Fatal("Helpers slice was not cloned")
		}
	})

	t.Run("invalid helper", func(t *testing.T) {
		root := t.TempDir()
		_, err := (Options{
			Root:    root,
			Helpers: []Helper{{Name: ""}},
		}).Validate()
		if err == nil || !errors.Is(err, fault.Invalid) {
			t.Fatalf("got %v, want fault.Invalid", err)
		}
	})
}
