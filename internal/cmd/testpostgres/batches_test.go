package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/internal/testinputs"
	"github.com/weiloon1234/Foundry-Go/testkit"
)

func TestPackageBatchesPreserveCoverageAndFailClosed(t *testing.T) {
	checkPackageBatches(t, "test -race -count=1", func(ctx context.Context, binary, dir string, environment []string) error {
		return testPackages(ctx, binary, dir, environment, []string{"test", "-race", "-count=1"}, 2, packageShard{index: 1, total: 1}, nil, nil)
	})
}

func TestMakePackageBatchesStopAfterFailure(t *testing.T) {
	testkit.TrackExternalInputs(t)
	makeBinary, err := exec.LookPath("make")
	if err != nil {
		t.Skip("Makefile acceptance requires an existing make command")
	}
	makefile, err := filepath.Abs("../../../Makefile")
	if err != nil {
		t.Fatal(err)
	}
	checkPackageBatches(t, "test -timeout=13m", func(ctx context.Context, binary, dir string, environment []string) error {
		cmd := exec.CommandContext(ctx, makeBinary, "--no-print-directory", "-f", makefile, "GO="+binary, "TEST_PACKAGE_BATCH_SIZE=2", "TEST_TIMEOUT=13m", "test")
		cmd.Dir, cmd.Env = dir, environment
		_, err := cmd.CombinedOutput()
		return err
	})
}

func TestRequiredRunnerPropagatesSelectedTimeout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test executable uses POSIX shell")
	}
	dir := t.TempDir()
	for _, fixture := range []string{"consumer", "goway"} {
		if err := os.MkdirAll(filepath.Join(dir, "tests/fixtures", fixture), 0700); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(dir)
	t.Setenv("FOUNDRY_TEST_POSTGRES_URL", "fixture-only-not-a-connection")
	log := filepath.Join(dir, "calls")
	t.Setenv("BATCH_LOG", log)
	binary := filepath.Join(dir, "go-probe")
	script := `#!/bin/sh
set -eu
if test "$1" = list; then
    printf '%s\n' pkg/a
else
    printf '%s\n' "$*" >> "$BATCH_LOG"
fi
`
	if err := os.WriteFile(binary, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	if err := run(t.Context(), binary, "unused", true, 2, 13*time.Minute, "1/1", nil, nil); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	want := "test -mod=readonly -count=1 -timeout=13m0s -race pkg/a\n"
	if string(data) != want+want+want {
		t.Fatal("root and fixture commands lost the configured timeout", string(data))
	}
}

func TestRequiredRunnerShardsEveryModuleAndExemptsOnlyRace(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test executable uses POSIX shell")
	}
	dir := t.TempDir()
	for _, fixture := range []string{"consumer", "goway"} {
		if err := os.MkdirAll(filepath.Join(dir, "tests/fixtures", fixture), 0700); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(dir)
	t.Setenv("FOUNDRY_TEST_POSTGRES_URL", "fixture-only-not-a-connection")
	log := filepath.Join(dir, "calls")
	t.Setenv("BATCH_LOG", log)
	binary := filepath.Join(dir, "go-probe")
	script := `#!/bin/sh
set -eu
if test "$1" = list; then
    printf '%s\n' pkg/d pkg/b pkg/a pkg/c
else
    printf '%s|%s\n' "$PWD" "$*" >> "$BATCH_LOG"
fi
`
	if err := os.WriteFile(binary, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, shard string
		race        bool
		want        []string
		heavy       []string
	}{
		{"ordinary", "1/1", false, []string{"pkg/a", "pkg/b", "pkg/c", "pkg/d"}, nil},
		{"race-first", "1/2", true, []string{"pkg/a", "pkg/d"}, nil},
		{"race-second", "2/2", true, []string{"pkg/c"}, nil},
		{"empty-shard", "5/5", true, nil, nil},
		{"heavy-first", "1/2", true, []string{"pkg/d", "pkg/a"}, []string{"pkg/d", "pkg/c", "pkg/b"}},
		{"heavy-second", "2/2", true, []string{"pkg/c"}, []string{"pkg/d", "pkg/c", "pkg/b"}},
		{"heavy-single", "1/1", true, []string{"pkg/a", "pkg/c", "pkg/d"}, []string{"pkg/d", "pkg/c", "pkg/b"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := os.WriteFile(log, nil, 0600); err != nil {
				t.Fatal(err)
			}
			if err := run(t.Context(), binary, "unused", tc.race, 6, time.Minute, tc.shard, []string{"pkg/b"}, tc.heavy); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(log)
			if err != nil {
				t.Fatal(err)
			}
			if len(tc.want) == 0 {
				if len(data) != 0 {
					t.Fatal("empty shard invoked go test", string(data))
				}
				return
			}
			lines := strings.Split(strings.TrimSpace(string(data)), "\n")
			if len(lines) != 3 {
				t.Fatal("not every module was tested", string(data))
			}
			for index, module := range []string{".", "tests/fixtures/consumer", "tests/fixtures/goway"} {
				path, args, _ := strings.Cut(lines[index], "|")
				path, err = filepath.EvalSymlinks(path)
				if err != nil {
					t.Fatal(err)
				}
				actual, err := filepath.EvalSymlinks(filepath.Join(dir, module))
				if err != nil || path != actual {
					t.Fatal("wrong module", path, actual, err)
				}
				fields := strings.Fields(args)
				if slices.Contains(fields, "-race") != tc.race || !slices.Equal(fields[len(fields)-len(tc.want):], tc.want) {
					t.Fatal("wrong packages or race mode", args)
				}
			}
		})
	}
}

func TestMakeRaceExemptionsRemainCoveredByTest(t *testing.T) {
	testkit.TrackExternalInputs(t)
	if runtime.GOOS == "windows" {
		t.Skip("test executable uses POSIX shell")
	}
	makeBinary, err := exec.LookPath("make")
	if err != nil {
		t.Skip("Makefile acceptance requires an existing make command")
	}
	makefile, err := filepath.Abs("../../../Makefile")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.test/root\n"), 0600); err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(dir, "calls")
	binary := filepath.Join(dir, "go-probe")
	script := `#!/bin/sh
set -eu
if test "$1" = run; then
    printf '%s\n' "$BATCH_INPUTS"
elif test "$1" = list; then
    printf '%s\n' example.test/root/exempt example.test/root/exempt/child example.test/root/ordinary
else
    printf '%s\n' "$*" >> "$BATCH_LOG"
fi
`
	if err := os.WriteFile(binary, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{"test", "race"} {
		if err := os.WriteFile(log, nil, 0600); err != nil {
			t.Fatal(err)
		}
		cmd := exec.CommandContext(t.Context(), makeBinary, "--no-print-directory", "-f", makefile, "GO="+binary, "RACE_EXEMPT_PACKAGES=./exempt", "FIXTURE_DIRS=", target)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "BATCH_LOG="+log, "BATCH_INPUTS="+testinputs.Variable+"="+strings.Repeat("a", 64))
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatal(string(output), err)
		}
		data, err := os.ReadFile(log)
		if err != nil {
			t.Fatal(err)
		}
		fields := strings.Fields(string(data))
		if slices.Contains(fields, "example.test/root/exempt") != (target == "test") || slices.Contains(fields, "-race") != (target == "race") {
			t.Fatal("race exemption changed ordinary coverage or race mode", string(data))
		}
		for _, pkg := range []string{"example.test/root/exempt/child", "example.test/root/ordinary"} {
			if !slices.Contains(fields, pkg) {
				t.Fatal("non-exempt package was skipped", string(data))
			}
		}
		if target == "race" && strings.Count(string(output), "Race-exempt (covered by make test): ./exempt") != 1 {
			t.Fatal("race exemption notice missing or repeated", string(output))
		}
	}
}

func TestMakeCompilerFlagsPreserveCallerSelection(t *testing.T) {
	testkit.TrackExternalInputs(t)
	makeBinary, err := exec.LookPath("make")
	if err != nil {
		t.Skip("Makefile acceptance requires an existing make command")
	}
	for _, tc := range []struct{ flags, want string }{
		{"", "-trimpath -gcflags=all=-dwarf=false"},
		{"-trimpath", "-trimpath -gcflags=all=-dwarf=false"},
		{"-gcflags=all=-dwarf=true", "-gcflags=all=-dwarf=true -trimpath"},
		{"-trimpath -gcflags=example.test/app=-N", "-trimpath -gcflags=example.test/app=-N"},
	} {
		cmd := exec.CommandContext(t.Context(), makeBinary, "--no-print-directory", "-f", "Makefile", "-f", "-", "GOFLAGS="+tc.flags, "print-flags")
		cmd.Stdin = strings.NewReader("print-flags:\n\t@printf '%s\\n' \"$$GOFLAGS\"\n")
		cmd.Dir = "../../.."
		output, err := cmd.CombinedOutput()
		if err != nil || strings.TrimSpace(string(output)) != tc.want {
			t.Fatalf("GOFLAGS=%q: got %q, want %q: %v", tc.flags, output, tc.want, err)
		}
	}
}

// Both runners must preserve the same package discovery and failure contract.
func checkPackageBatches(t *testing.T, prefix string, run func(context.Context, string, string, []string) error) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("test executable uses POSIX shell")
	}
	for _, mode := range []string{"success", "list-failure", "empty-list", "test-failure"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			log := filepath.Join(dir, "calls")
			binary := filepath.Join(dir, "go-probe")
			script := `#!/bin/sh
set -eu
if test "$1" = run && test "$2" = ./internal/cmd/testinputs; then
    printf '%s\n' "$BATCH_INPUTS"
elif test "$1" = list; then
    if test "$BATCH_MODE" = empty-list; then exit 0; fi
    printf '%s\n' pkg/a pkg/b pkg/c pkg/d pkg/e
    if test "$BATCH_MODE" = list-failure; then exit 1; fi
else
    printf '%s\n' "$*" >> "$BATCH_LOG"
    if test "$BATCH_MODE" = test-failure; then exit 1; fi
fi
`
			if err := os.WriteFile(binary, []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			environment := append(os.Environ(), "BATCH_MODE="+mode, "BATCH_LOG="+log, "BATCH_INPUTS="+testinputs.Variable+"="+strings.Repeat("a", 64))
			err := run(t.Context(), binary, dir, environment)
			data, _ := os.ReadFile(log)
			if mode == "success" {
				want := []string{prefix + " pkg/a pkg/b", prefix + " pkg/c pkg/d", prefix + " pkg/e"}
				if err != nil || !reflect.DeepEqual(strings.Split(strings.TrimSpace(string(data)), "\n"), want) {
					t.Fatal(string(data), err)
				}
			} else {
				if err == nil {
					t.Fatal("failed discovery/execution passed")
				}
				if mode == "test-failure" {
					if strings.Count(string(data), "\n") != 1 {
						t.Fatal("testing continued after failure", string(data))
					}
				} else if len(data) != 0 {
					t.Fatal("incomplete package discovery executed tests", string(data))
				}
			}
		})
	}
}
