//go:build !race

package goway_test

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
	pkg      string
	measured int
}

const footprintFailureAdvice = "a generic method, field or codec helper has expanded the reachable surface; inspect database/orm surface guards and blueprint/goway-data-layer/README.md, Goals and rules (R1–R9)"

// Stage 3.0 ceilings are retained; JSON/interval probes were measured in 3.1a.
// Go 1.27.1 darwin/arm64: T/t symbols in each package archive.
// Budget is ceil(measured * 1.15), calculated exactly with integer arithmetic.
var footprintBudgets = map[string]footprintBudget{
	"column_json":              {"./testdata/footprint/column_json", 1280},
	"column_nullable_json":     {"./testdata/footprint/column_nullable_json", 1522},
	"column_interval":          {"./testdata/footprint/column_interval", 1178},
	"column_nullable_interval": {"./testdata/footprint/column_nullable_interval", 1406},

	"column_time":   {"./testdata/footprint/column_time", 1194},
	"column_enum":   {"./testdata/footprint/column_enum", 1232},
	"relation_with": {"./testdata/footprint/relation_with", 1847},

	"model_type":           {"./testdata/footprint/model_type", 299},
	"table_ref":            {"./testdata/footprint/table_ref", 803},
	"onequery":             {"./testdata/footprint/onequery", 803},
	"q_with":               {"./testdata/footprint/q_with", 1053},
	"q_select":             {"./testdata/footprint/q_select", 836},
	"q_join":               {"./testdata/footprint/q_join", 1598},
	"report_pipeline":      {"./testdata/footprint/report_pipeline", 1905},
	"models":               {"./models", 6147},
	"column_number":        {"./testdata/footprint/column_number", 1172},
	"column_nullable_text": {"./testdata/footprint/column_nullable_text", 1456},
	"models_1":             {"./testdata/footprint/models_1", 1619},
	"models_3":             {"./testdata/footprint/models_3", 2435},
}

func TestFootprintBudgets(t *testing.T) {
	testkit.TrackExternalInputs(t)

	probeOrder := []string{"model_type", "table_ref", "onequery", "q_with", "q_select", "q_join", "report_pipeline", "models", "column_number", "column_nullable_text", "models_1", "models_3", "column_time", "column_enum", "relation_with", "column_json", "column_nullable_json", "column_interval", "column_nullable_interval"}
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

	functions := make(map[string]int)
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

		measuredFunctions, _ := parseFootprintSymbols(t, nmStdout.Bytes())
		functions[name] = measuredFunctions
		maxFunctions := (budget.measured*115 + 99) / 100
		t.Logf("probe %s: %d functions (baseline %d, max %d), %d archive bytes", name, measuredFunctions, budget.measured, maxFunctions, measuredBytes)
		t.Run(name, func(t *testing.T) {
			if measuredFunctions > maxFunctions {
				t.Errorf("%s exceeded function budget: measured %d > budget %d; %s", name, measuredFunctions, maxFunctions, footprintFailureAdvice)
			}
		})
	}
	// The two packages have identical model shapes. Subtracting their totals
	// removes shared value-type and package overhead; the delta covers two models.
	const measuredGrowth = 816
	maxGrowth := (measuredGrowth*115 + 99) / 100
	growth := functions["models_3"] - functions["models_1"]
	t.Logf("model slope: %d functions for two added models (max %d), %.1f per model", growth, maxGrowth, float64(growth)/2)
	t.Run("model_slope", func(t *testing.T) {
		if growth > maxGrowth {
			t.Errorf("model growth exceeded function budget: measured %d > budget %d; %s", growth, maxGrowth, footprintFailureAdvice)
		}
	})
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
