package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/internal/testinputs"
	"github.com/weiloon1234/Foundry-Go/testkit"
)

func TestPackageBatchesPreserveCoverageAndFailClosed(t *testing.T) {
	checkPackageBatches(t, "test -race -count=1", func(ctx context.Context, binary, dir string, environment []string) error {
		return testPackages(ctx, binary, dir, environment, []string{"test", "-race", "-count=1"}, 2)
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
	if err := os.MkdirAll(filepath.Join(dir, "tests/fixtures/consumer"), 0700); err != nil {
		t.Fatal(err)
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
	if err := run(t.Context(), binary, "unused", true, 2, 13*time.Minute); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	want := "test -mod=readonly -count=1 -timeout=13m0s -race pkg/a\n"
	if string(data) != want+want {
		t.Fatal("root and consumer commands lost the configured timeout", string(data))
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
