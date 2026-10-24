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
	"slices"
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

// Existing ceilings are retained; sensitive probes were measured in Stage 3.1b.
// Direct relation probes were measured in Stage 4b with the same toolchain.
// Go 1.27.1 darwin/arm64: T/t symbols in each package archive.
// Budget is ceil(measured * 1.15), calculated exactly with integer arithmetic.
var footprintBudgets = map[string]footprintBudget{
	"prune_registry":              {"./testdata/footprint/prune_registry", 933},
	"transaction_joins":           {"./testdata/footprint/transaction_joins", 1835},
	"transaction_correlation":     {"./testdata/footprint/transaction_correlation", 2908},
	"result_cursor":               {"./testdata/footprint/result_cursor", 2229},
	"result_reads":                {"./testdata/footprint/result_reads", 1351},
	"role_time_read":              {"./testdata/footprint/role_time_read", 1394},
	"role_primary_number":         {"./testdata/footprint/role_primary_number", 1195},
	"role_primary_text":           {"./testdata/footprint/role_primary_text", 1216},
	"role_lookup_input":           {"./testdata/footprint/role_lookup_input", 1226},
	"role_soft_read":              {"./testdata/footprint/role_soft_read", 1548},
	"role_soft_write":             {"./testdata/footprint/role_soft_write", 620},
	"role_input_same":             {"./testdata/footprint/role_input_same", 1360},
	"role_source":                 {"./testdata/footprint/role_source", 905},
	"terminal_create_or_first":    {"./testdata/footprint/terminal_create_or_first", 880},
	"terminal_adjustment":         {"./testdata/footprint/terminal_adjustment", 1382},
	"terminal_explain":            {"./testdata/footprint/terminal_explain", 1318},
	"terminal_lookup":             {"./testdata/footprint/terminal_lookup", 893},
	"terminal_prune":              {"./testdata/footprint/terminal_prune", 930},
	"datatable_projection":        {"./testdata/footprint/datatable_projection", 2951},
	"report_reads":                {"./testdata/footprint/report_reads", 1063},
	"report_filters":              {"./testdata/footprint/report_filters", 2095},
	"validation_database":         {"./testdata/footprint/validation_database", 1441},
	"framework_webhookstore":      {"github.com/weiloon1234/Foundry-Go/internal/webhookstore", 4705},
	"framework_notificationstore": {"github.com/weiloon1234/Foundry-Go/internal/notificationstore", 5119},
	"framework_attachmentstore":   {"github.com/weiloon1234/Foundry-Go/internal/attachmentstore", 4729},
	"framework_extensionstore":    {"github.com/weiloon1234/Foundry-Go/internal/extensionstore", 5407},
	"framework_auditstore":        {"github.com/weiloon1234/Foundry-Go/internal/auditstore", 4753},
	"rows_each":                   {"./testdata/footprint/rows_each", 899},

	"cte_recursive": {"./testdata/footprint/cte_recursive", 1076},
	"record_sets":   {"./testdata/footprint/record_sets", 1288},
	"value_sets":    {"./testdata/footprint/value_sets", 2230},
	"uuid_seeks":    {"./testdata/footprint/uuid_seeks", 1023},
	"each_writes":   {"./testdata/footprint/each_writes", 888},

	"json_paths":     {"./testdata/footprint/json_paths", 6146},
	"json_functions": {"./testdata/footprint/json_functions", 2736},

	"scalar_text":           {"./testdata/footprint/scalar_text", 2312},
	"scalar_temporal":       {"./testdata/footprint/scalar_temporal", 2625},
	"extension_handles":     {"./testdata/footprint/extension_handles", 1481},
	"extension_declaration": {"foundry.test/goway/models/extensions", 30434},
	"extension_slots":       {"./testdata/footprint/extension_slots", 2154},
	"extension_slots_old":   {"./testdata/footprint/extension_slots_old", 30493},
	"orm_factory":           {"./testdata/footprint/orm_factory", 1093},
	"orm_binding":           {"./testdata/footprint/orm_binding", 1089},

	"framework_countries":        {"github.com/weiloon1234/Foundry-Go/countries", 4122},
	"framework_jobarchive":       {"github.com/weiloon1234/Foundry-Go/internal/jobarchive", 2280},
	"framework_idempotencystore": {"github.com/weiloon1234/Foundry-Go/internal/idempotencystore", 2249},
	"framework_mfastore":         {"github.com/weiloon1234/Foundry-Go/internal/mfastore", 2947},
	"framework_challengestore":   {"github.com/weiloon1234/Foundry-Go/internal/challengestore", 2558},
	"framework_sessionstore":     {"github.com/weiloon1234/Foundry-Go/internal/sessionstore", 4166},
	"framework_tokenstore":       {"github.com/weiloon1234/Foundry-Go/internal/tokenstore", 5315},
	"scoped_write":               {"./testdata/footprint/scoped_write", 1111},

	// Framework store migration: actual declaration/write and publisher consumers.
	"framework_outbox": {"github.com/weiloon1234/Foundry-Go/internal/outboxstore", 2896},
	"outbox_publisher": {"github.com/weiloon1234/Foundry-Go/outbox/publisher", 1324},

	"records":                    {"./testdata/footprint/records", 1313},
	"transaction_records":        {"./testdata/footprint/transaction_records", 1296},
	"source_write":               {"./testdata/footprint/source_write", 1311},
	"conflict_advanced":          {"./testdata/footprint/conflict_advanced", 1817},
	"conflict":                   {"./testdata/footprint/conflict", 944},
	"audit":                      {"./testdata/footprint/audit", 1313},
	"relation_write_declaration": {"./testdata/footprint/relation_write_declaration", 361},
	"relation_write_attach":      {"./testdata/footprint/relation_write_attach", 1110},
	"relation_write_sync":        {"./testdata/footprint/relation_write_sync", 1124},

	"aggregate_slot_declaration": {"./testdata/footprint/aggregate_slot_declaration", 1112},
	"aggregate_slot_direct":      {"./testdata/footprint/aggregate_slot_direct", 1490},
	"aggregate_slot_pivot":       {"./testdata/footprint/aggregate_slot_pivot", 2095},
	"aggregate_slot_nested":      {"./testdata/footprint/aggregate_slot_nested", 2048},

	"pivot":              {"./testdata/footprint/pivot", 1005},
	"through":            {"./testdata/footprint/through", 928},
	"morph":              {"./testdata/footprint/morph", 1088},
	"nested_with":        {"./testdata/footprint/nested_with", 1281},
	"has":                {"./testdata/footprint/has", 1302},
	"has_one":            {"./testdata/footprint/has_one", 1281},
	"raw":                {"./testdata/footprint/raw", 1045},
	"cursor":             {"./testdata/footprint/cursor", 1127},
	"key_chunk":          {"./testdata/footprint/key_chunk", 812},
	"input_key":          {"./testdata/footprint/input_key", 982},
	"mutators":           {"./testdata/footprint/mutators", 801},
	"mutator_explicit":   {"./testdata/footprint/mutator_explicit", 1605},
	"input_column_kinds": {"./testdata/footprint/input_column_kinds", 5088},
	"aggregate_group":    {"./testdata/footprint/aggregate_group", 2426},
	"aggregate_families": {"./testdata/footprint/aggregate_families", 3799},
	"distinct":           {"./testdata/footprint/distinct", 1135},

	"correlation":          {"./testdata/footprint/correlation", 2752},
	"lateral":              {"./testdata/footprint/lateral", 2852},
	"on_comparisons":       {"./testdata/footprint/on_comparisons", 1454},
	"subqueries":           {"./testdata/footprint/subqueries", 2837},
	"outer_join":           {"./testdata/footprint/outer_join", 1668},
	"nullable_join_chain":  {"./testdata/footprint/nullable_join_chain", 1695},
	"retrieval_read":       {"./testdata/footprint/retrieval_read", 826},
	"global_scopes":        {"./testdata/footprint/global_scopes", 1374},
	"conditional_row":      {"./testdata/footprint/conditional_row", 1681},
	"conditional_selected": {"./testdata/footprint/conditional_selected", 1511},
	"expr_arithmetic":      {"./testdata/footprint/expr_arithmetic", 1162},
	"expr_selected":        {"./testdata/footprint/expr_selected", 1551},
	"lifecycle_write":      {"./testdata/footprint/lifecycle_write", 930},
	"lifecycle_changes":    {"./testdata/footprint/lifecycle_changes", 1050},

	"raw_fragments": {"./testdata/footprint/raw_fragments", 1331},
	"window_rank":   {"./testdata/footprint/window_rank", 1345},
	"window_frames": {"./testdata/footprint/window_frames", 1767},

	"column_encrypted_text":          {"./testdata/footprint/column_encrypted_text", 1127},
	"column_nullable_encrypted_text": {"./testdata/footprint/column_nullable_encrypted_text", 1359},
	"column_encrypted_json":          {"./testdata/footprint/column_encrypted_json", 1217},
	"column_nullable_encrypted_json": {"./testdata/footprint/column_nullable_encrypted_json", 1449},
	"column_password":                {"./testdata/footprint/column_password", 1190},
	"column_nullable_password":       {"./testdata/footprint/column_nullable_password", 1417},
	"sensitive_models":               {"./models/sensitive", 2606},
	"column_json":                    {"./testdata/footprint/column_json", 1280},
	"column_nullable_json":           {"./testdata/footprint/column_nullable_json", 1522},
	"column_interval":                {"./testdata/footprint/column_interval", 1178},
	"column_nullable_interval":       {"./testdata/footprint/column_nullable_interval", 1406},

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

	probeOrder := make([]string, 0, len(footprintBudgets))
	for name := range footprintBudgets {
		probeOrder = append(probeOrder, name)
	}
	slices.Sort(probeOrder)

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

		measuredFunctions, _, auditFunctions := parseFootprintSymbols(t, nmStdout.Bytes())
		if (name == "onequery" || name == "q_with" || name == "aggregate_families") && auditFunctions != 0 {
			t.Fatalf("%s reaches %d audit functions; keep audit adapters behind package functions", name, auditFunctions)
		}
		functions[name] = measuredFunctions
		maxFunctions := (budget.measured*115 + 99) / 100
		t.Logf("probe %s: %d functions (baseline %d, max %d), %d archive bytes", name, measuredFunctions, budget.measured, maxFunctions, measuredBytes)
		t.Run(name, func(t *testing.T) {
			if measuredFunctions > maxFunctions {
				t.Errorf("%s exceeded function budget: measured %d > budget %d; %s", name, measuredFunctions, maxFunctions, footprintFailureAdvice)
			}
		})
	}
	// Identical four-slot LoadMissing workloads: importer cost must remain well
	// below the generated descriptor graph, not merely below its old ceiling.
	t.Run("extension_namespace_reduction", func(t *testing.T) {
		if functions["extension_slots"]*4 >= functions["extension_slots_old"] {
			t.Fatalf("thin extension namespace must remain below one quarter of the generated baseline: new %d, old %d", functions["extension_slots"], functions["extension_slots_old"])
		}
	})
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

func parseFootprintSymbols(t *testing.T, data []byte) (functions int, definedSymbols int, auditFunctions int) {
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
				if strings.Contains(scanner.Text(), "/audit/record.") || strings.Contains(scanner.Text(), "/database/orm.Audit") {
					auditFunctions++
				}
			}
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scanning nm output: %v", err)
	}
	return functions, definedSymbols, auditFunctions
}
