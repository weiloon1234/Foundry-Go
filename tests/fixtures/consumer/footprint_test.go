//go:build !race

package consumer_test

import (
	"bufio"
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/testkit"
)

type footprintBudget struct {
	pkg          string
	maxFunctions int
	maxSymbols   int
	maxBytes     int64
}

const footprintFailureAdvice = "a method or field on a model or endpoint type probably reaches a framework generic (query engine, signed/idempotent endpoint) again; see docs/guides/query-build-resources.md#model-and-endpoint-edges"

// Measured values on this branch versus pre-change baseline (closure-free Tier A).
// Baseline measurements are taken from Foundry-Go commit dd188a7 with the same probe bodies.
//
// Probe case       Pre-change baseline         This branch (measured)      Committed budget (ceil 1.25x)
// ------------------------------------------------------------------------------------------------------
// model_type       14,910 fn, ~70.9 MB         1,159 fn,  7,267 syms, 4.55 MB   1,449 fn,  9,084 syms, 5,959,555 B (~5.68 MB)
// model_isolated    2,546 fn, ~12.2 MB           139 fn,  1,051 syms, 0.60 MB     174 fn,  1,314 syms,   786,148 B (~0.75 MB)
// model_draft       2,612 fn, ~12.4 MB           225 fn,  1,566 syms, 0.95 MB     282 fn,  1,958 syms, 1,239,298 B (~1.18 MB)
// endpoint_plain    3,734 fn, ~16.4 MB         1,065 fn,  7,314 syms, 4.62 MB   1,332 fn,  9,143 syms, 6,059,643 B (~5.78 MB)
var footprintBudgets = map[string]footprintBudget{
	"model_type": {
		pkg:          "./testdata/footprint/model_type",
		maxFunctions: 1449,
		maxSymbols:   9084,
		maxBytes:     5959555,
	},
	"model_isolated": {
		pkg:          "./testdata/footprint/model_isolated",
		maxFunctions: 174,
		maxSymbols:   1314,
		maxBytes:     786148,
	},
	"model_draft": {
		pkg:          "./testdata/footprint/model_draft",
		maxFunctions: 282,
		maxSymbols:   1958,
		maxBytes:     1239298,
	},
	"endpoint_plain": {
		pkg:          "./testdata/footprint/endpoint_plain",
		maxFunctions: 1332,
		maxSymbols:   9143,
		maxBytes:     6059643,
	},
}

func TestFootprintBudgets(t *testing.T) {
	testkit.TrackExternalInputs(t)

	probeOrder := []string{"model_type", "model_isolated", "model_draft", "endpoint_plain"}
	pkgArgs := make([]string, 0, len(probeOrder))
	for _, name := range probeOrder {
		pkgArgs = append(pkgArgs, footprintBudgets[name].pkg)
	}

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()

	listArgs := append([]string{"list", "-export", "-f", "{{.ImportPath}} {{.Export}}"}, pkgArgs...)
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	cmd := exec.CommandContext(ctx, goBin, listArgs...)
	configureCompilerCommand(cmd)
	cmd.Env = append(os.Environ(), "GOWORK=off", "GOPROXY=off", "GOSUMDB=off", "GOTOOLCHAIN=local")

	stdout := &compilerOutput{limit: 1 << 20, cancel: cancel}
	stderr := &compilerOutput{limit: 64 << 10, cancel: cancel}
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	err := cmd.Run()
	if stdout.exceeded || stderr.exceeded {
		t.Fatal("go list -export output exceeded limit")
	}
	if err != nil {
		t.Fatalf("go list -export failed: %v\nstderr: %s", err, stderr.String())
	}

	exports := make(map[string]string)
	scanner := bufio.NewScanner(bytes.NewReader(stdout.Bytes()))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) != 2 {
			t.Fatalf("unexpected go list output format: %q", line)
		}
		importPath, exportArchive := parts[0], parts[1]
		for name, budget := range footprintBudgets {
			expectedSuffix := strings.TrimPrefix(budget.pkg, ".")
			if strings.HasSuffix(importPath, expectedSuffix) {
				exports[name] = exportArchive
				break
			}
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("reading go list output: %v", err)
	}

	for _, name := range probeOrder {
		budget := footprintBudgets[name]
		archive, ok := exports[name]
		if !ok || archive == "" {
			t.Fatalf("no export archive found for probe %s (%s)", name, budget.pkg)
		}

		info, err := os.Stat(archive)
		if err != nil {
			t.Fatalf("stat export archive %s: %v", archive, err)
		}
		measuredBytes := info.Size()

		nmCmd := exec.CommandContext(ctx, goBin, "tool", "nm", archive)
		configureCompilerCommand(nmCmd)
		nmCmd.Env = append(os.Environ(), "GOWORK=off", "GOPROXY=off", "GOSUMDB=off", "GOTOOLCHAIN=local")
		nmStdout := &compilerOutput{limit: 64 << 20, cancel: cancel}
		nmStderr := &compilerOutput{limit: 64 << 10, cancel: cancel}
		nmCmd.Stdout = nmStdout
		nmCmd.Stderr = nmStderr
		err = nmCmd.Run()
		if nmStdout.exceeded || nmStderr.exceeded {
			t.Fatalf("go tool nm output exceeded limit for %s", archive)
		}
		if err != nil {
			t.Fatalf("go tool nm %s failed: %v\nstderr: %s", archive, err, nmStderr.String())
		}

		measuredFunctions, measuredSymbols := parseFootprintSymbols(t, nmStdout.Bytes())
		t.Logf("probe %s: %d functions (max %d), %d symbols (max %d), %d bytes (max %d)",
			name, measuredFunctions, budget.maxFunctions, measuredSymbols, budget.maxSymbols, measuredBytes, budget.maxBytes)

		t.Run(name, func(t *testing.T) {
			if measuredFunctions > budget.maxFunctions {
				t.Errorf("%s exceeded function budget: measured %d > budget %d; %s",
					name, measuredFunctions, budget.maxFunctions, footprintFailureAdvice)
			}
			if measuredSymbols > budget.maxSymbols {
				t.Errorf("%s exceeded total defined symbol budget: measured %d > budget %d; %s",
					name, measuredSymbols, budget.maxSymbols, footprintFailureAdvice)
			}
			if measuredBytes > budget.maxBytes {
				t.Errorf("%s exceeded archive size budget: measured %d bytes > budget %d bytes (%.2f MB > %.2f MB); %s",
					name, measuredBytes, budget.maxBytes,
					float64(measuredBytes)/(1024*1024), float64(budget.maxBytes)/(1024*1024), footprintFailureAdvice)
			}
		})
	}
}

func parseFootprintSymbols(t *testing.T, data []byte) (functions int, definedSymbols int) {
	t.Helper()
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) == 0 {
			continue
		}
		var symType string
		if len(fields) >= 2 && len(fields[1]) == 1 {
			symType = fields[1]
		} else if len(fields[0]) == 1 {
			symType = fields[0]
		} else {
			t.Fatalf("unrecognized nm symbol format: %q", scanner.Text())
		}
		if symType != "U" {
			definedSymbols++
			if symType == "T" || symType == "t" {
				functions++
			}
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scanning nm output: %v", err)
	}
	return functions, definedSymbols
}
