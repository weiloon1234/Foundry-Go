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

// Budgets remeasured for v0.2 Compile Footprint on 2026-10-30 (Go 1.27.3 darwin/arm64).
// Ceilings use ceil(measured * 1.15) calculated with integer arithmetic.
// Pre-change baselines reflect commit dd188a7 where recorded.
//
// Probe case              Pre-change baseline         v0.2 (measured)              Committed budget (ceil 1.15x)
// ------------------------------------------------------------------------------------------------------------------
// model_type              14,910 fn, ~70.9 MB           915 fn,  5,846 syms, 3.55 MB    1,053 fn,  6,723 syms, 4,279,107 B (~4.08 MB)
// model_isolated           2,546 fn, ~12.2 MB           106 fn,    799 syms, 0.43 MB      122 fn,    919 syms,   522,974 B (~0.50 MB)
// model_assignments        2,612 fn, ~12.4 MB           200 fn,  1,398 syms, 0.84 MB      230 fn,  1,608 syms, 1,015,395 B (~0.97 MB)
// endpoint_plain           3,734 fn, ~16.4 MB           454 fn,  3,669 syms, 2.02 MB      523 fn,  4,220 syms, 2,440,908 B (~2.33 MB)
// endpoint_ordinary        —                            695 fn,  5,321 syms, 3.11 MB      800 fn,  6,120 syms, 3,755,093 B (~3.58 MB)
// endpoint_authenticated   —                            982 fn,  7,198 syms, 4.36 MB    1,130 fn,  8,278 syms, 5,254,902 B (~5.01 MB)
// dto_validated            —                            376 fn,  2,744 syms, 1.80 MB      433 fn,  3,156 syms, 2,172,424 B (~2.07 MB)
var footprintBudgets = map[string]footprintBudget{
	"model_type": {
		pkg:          "./testdata/footprint/model_type",
		maxFunctions: 1053,
		maxSymbols:   6723,
		maxBytes:     4279107,
	},
	"model_isolated": {
		pkg:          "./testdata/footprint/model_isolated",
		maxFunctions: 122,
		maxSymbols:   919,
		maxBytes:     522974,
	},
	"model_assignments": {
		pkg:          "./testdata/footprint/model_draft",
		maxFunctions: 230,
		maxSymbols:   1608,
		maxBytes:     1015395,
	},
	"endpoint_plain": {
		pkg:          "./testdata/footprint/endpoint_plain",
		maxFunctions: 523,
		maxSymbols:   4220,
		maxBytes:     2440908,
	},
	"endpoint_ordinary": {
		pkg:          "./testdata/footprint/endpoint_ordinary",
		maxFunctions: 800,
		maxSymbols:   6120,
		maxBytes:     3755093,
	},
	"endpoint_authenticated": {
		pkg:          "./testdata/footprint/endpoint_authenticated",
		maxFunctions: 1130,
		maxSymbols:   8278,
		maxBytes:     5254902,
	},
	"dto_validated": {
		pkg:          "./testdata/footprint/dto_validated",
		maxFunctions: 433,
		maxSymbols:   3156,
		maxBytes:     2172424,
	},
}

func TestFootprintBudgets(t *testing.T) {
	testkit.TrackExternalInputs(t)

	probeOrder := []string{"model_type", "model_isolated", "model_assignments", "endpoint_plain", "endpoint_ordinary", "endpoint_authenticated", "dto_validated"}
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
