//go:build !race

package goway_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/testkit"
	"github.com/weiloon1234/Foundry-Go/tests/oracle"
)

type footprintBudget struct {
	pkg      string
	measured int
}

const footprintFailureAdvice = "a generic method, field or codec helper has expanded the reachable surface; inspect database/orm surface guards and blueprint/goway-data-layer/README.md, Goals and rules (R1–R9)"

// Budgets remeasured for v0.2 Compile Footprint on 2026-10-30 (Go 1.27.3 darwin/arm64).
// T/t symbols in each package archive.
// Budget is ceil(measured * 1.15), calculated exactly with integer arithmetic.
var footprintBudgets = map[string]footprintBudget{
	"pilot_expressions":         {"./testdata/footprint/pilot_expressions", 1222},
	"load_query":                {"./testdata/footprint/load_query", 599},
	"related_value":             {"./testdata/footprint/related_value", 1173},
	"relation_expression_order": {"./testdata/footprint/relation_expression_order", 1943},
	"cursor_terminal":           {"./testdata/footprint/cursor_terminal", 1198},

	"derived_aggregates": {"./testdata/footprint/derived_aggregates", 2172},
	"record_distinct_on": {"./testdata/footprint/record_distinct_on", 1089},

	"relation_filter":               {"./testdata/footprint/relation_filter", 822},
	"read_scalar":                   {"./testdata/footprint/read_scalar", 761},
	"read_record":                   {"./testdata/footprint/read_record", 562},
	"read_set":                      {"./testdata/footprint/read_set", 648},
	"key_clauses":                   {"./testdata/footprint/key_clauses", 1700},
	"result_clauses":                {"./testdata/footprint/result_clauses", 1534},
	"prune_registry":                {"./testdata/footprint/prune_registry", 320},
	"transaction_joins":             {"./testdata/footprint/transaction_joins", 1555},
	"transaction_correlation":       {"./testdata/footprint/transaction_correlation", 1922},
	"result_cursor":                 {"./testdata/footprint/result_cursor", 1258},
	"result_reads":                  {"./testdata/footprint/result_reads", 1021},
	"role_time_read":                {"./testdata/footprint/role_time_read", 742},
	"role_primary_number":           {"./testdata/footprint/role_primary_number", 590},
	"role_primary_text":             {"./testdata/footprint/role_primary_text", 591},
	"role_lookup_input":             {"./testdata/footprint/role_lookup_input", 599},
	"role_soft_read":                {"./testdata/footprint/role_soft_read", 953},
	"role_soft_write":               {"./testdata/footprint/role_soft_write", 262},
	"role_input_same":               {"./testdata/footprint/role_input_same", 742},
	"role_source":                   {"./testdata/footprint/role_source", 569},
	"terminal_create_or_first":      {"./testdata/footprint/terminal_create_or_first", 279},
	"terminal_adjustment":           {"./testdata/footprint/terminal_adjustment", 592},
	"terminal_explain":              {"./testdata/footprint/terminal_explain", 652},
	"terminal_lookup":               {"./testdata/footprint/terminal_lookup", 308},
	"terminal_prune":                {"./testdata/footprint/terminal_prune", 317},
	"datatable_projection":          {"./testdata/footprint/datatable_projection", 2236},
	"report_reads":                  {"./testdata/footprint/report_reads", 701},
	"report_filters":                {"./testdata/footprint/report_filters", 1204},
	"validation_database":           {"./testdata/footprint/validation_database", 746},
	"framework_webhookstore":        {"github.com/weiloon1234/Foundry-Go/internal/webhookstore", 2539},
	"framework_notificationstore":   {"github.com/weiloon1234/Foundry-Go/internal/notificationstore", 2780},
	"framework_attachmentstore":     {"github.com/weiloon1234/Foundry-Go/internal/attachmentstore", 3032},
	"framework_extensionstore":      {"github.com/weiloon1234/Foundry-Go/internal/extensionstore", 3209},
	"framework_auditstore":          {"github.com/weiloon1234/Foundry-Go/internal/auditstore", 4077},
	"rows_each":                     {"./testdata/footprint/rows_each", 626},
	"framework_registration_option": {"./integrations/registrationoption", 475},
	"framework_application_control": {"./integrations/applicationcontrol", 3003},
	"framework_registration":        {"./integrations/registration", 3135},
	"framework_pagination":          {"./integrations", 2144},
	"framework_seeder":              {"./integrations/seeding", 266},
	"each_soft_writes":              {"./testdata/footprint/each_soft_writes", 987},
	"write_unset":                   {"./testdata/footprint/write_unset", 171},
	"observer_counts":               {"./testdata/footprint/observer_counts", 63},
	"source_clauses":                {"./testdata/footprint/source_clauses", 1848},
	"extension_files":               {"./testdata/footprint/extension_files", 1479},

	"cte_recursive": {"./testdata/footprint/cte_recursive", 885},
	"record_sets":   {"./testdata/footprint/record_sets", 1134},
	"value_sets":    {"./testdata/footprint/value_sets", 1501},
	"uuid_seeks":    {"./testdata/footprint/uuid_seeks", 653},
	"each_writes":   {"./testdata/footprint/each_writes", 283},

	"json_paths":     {"./testdata/footprint/json_paths", 5344},
	"json_functions": {"./testdata/footprint/json_functions", 2424},

	"scalar_text":           {"./testdata/footprint/scalar_text", 1824},
	"scalar_temporal":       {"./testdata/footprint/scalar_temporal", 2348},
	"extension_matching":    {"./testdata/footprint/extension_matching", 3553},
	"extension_handles":     {"./testdata/footprint/extension_handles", 968},
	"extension_declaration": {"foundry.test/goway/models/extensions", 5795},
	"extension_slots":       {"./testdata/footprint/extension_slots", 2109},
	"orm_factory":           {"./testdata/footprint/orm_factory", 650},
	"orm_binding":           {"./testdata/footprint/orm_binding", 640},

	"framework_countries":        {"github.com/weiloon1234/Foundry-Go/countries", 3380},
	"framework_jobarchive":       {"github.com/weiloon1234/Foundry-Go/internal/jobarchive", 1514},
	"framework_idempotencystore": {"github.com/weiloon1234/Foundry-Go/internal/idempotencystore", 1698},
	"framework_mfastore":         {"github.com/weiloon1234/Foundry-Go/internal/mfastore", 2072},
	"framework_challengestore":   {"github.com/weiloon1234/Foundry-Go/internal/challengestore", 1394},
	"framework_sessionstore":     {"github.com/weiloon1234/Foundry-Go/internal/sessionstore", 2523},
	"framework_tokenstore":       {"github.com/weiloon1234/Foundry-Go/internal/tokenstore", 2849},
	"scoped_write":               {"./testdata/footprint/scoped_write", 481},

	// Framework store migration: actual declaration/write and publisher consumers.
	"framework_outbox": {"github.com/weiloon1234/Foundry-Go/internal/outboxstore", 2055},
	"outbox_publisher": {"github.com/weiloon1234/Foundry-Go/outbox/publisher", 756},

	"records":                    {"./testdata/footprint/records", 1106},
	"transaction_records":        {"./testdata/footprint/transaction_records", 1094},
	"source_write":               {"./testdata/footprint/source_write", 790},
	"conflict_advanced":          {"./testdata/footprint/conflict_advanced", 1327},
	"conflict":                   {"./testdata/footprint/conflict", 474},
	"audit":                      {"./testdata/footprint/audit", 793},
	"relation_write_declaration": {"./testdata/footprint/relation_write_declaration", 361},
	"relation_write_attach":      {"./testdata/footprint/relation_write_attach", 620},
	"relation_write_sync":        {"./testdata/footprint/relation_write_sync", 625},

	"aggregate_slot_declaration": {"./testdata/footprint/aggregate_slot_declaration", 608},
	"aggregate_slot_direct":      {"./testdata/footprint/aggregate_slot_direct", 1074},
	"aggregate_slot_pivot":       {"./testdata/footprint/aggregate_slot_pivot", 1220},
	"aggregate_slot_nested":      {"./testdata/footprint/aggregate_slot_nested", 1187},

	"pivot":              {"./testdata/footprint/pivot", 652},
	"through":            {"./testdata/footprint/through", 571},
	"morph":              {"./testdata/footprint/morph", 638},
	"nested_with":        {"./testdata/footprint/nested_with", 892},
	"has":                {"./testdata/footprint/has", 870},
	"has_one":            {"./testdata/footprint/has_one", 887},
	"raw":                {"./testdata/footprint/raw", 693},
	"cursor":             {"./testdata/footprint/cursor", 533},
	"key_chunk":          {"./testdata/footprint/key_chunk", 489},
	"input_key":          {"./testdata/footprint/input_key", 600},
	"mutators":           {"./testdata/footprint/mutators", 245},
	"mutator_explicit":   {"./testdata/footprint/mutator_explicit", 989},
	"input_column_kinds": {"./testdata/footprint/input_column_kinds", 3795},
	"aggregate_group":    {"./testdata/footprint/aggregate_group", 2168},
	"aggregate_families": {"./testdata/footprint/aggregate_families", 3623},
	"distinct":           {"./testdata/footprint/distinct", 823},

	"correlation":          {"./testdata/footprint/correlation", 1993},
	"lateral":              {"./testdata/footprint/lateral", 2043},
	"on_comparisons":       {"./testdata/footprint/on_comparisons", 966},
	"subqueries":           {"./testdata/footprint/subqueries", 2110},
	"outer_join":           {"./testdata/footprint/outer_join", 1213},
	"nullable_join_chain":  {"./testdata/footprint/nullable_join_chain", 1242},
	"retrieval_read":       {"./testdata/footprint/retrieval_read", 364},
	"global_scopes":        {"./testdata/footprint/global_scopes", 491},
	"conditional_row":      {"./testdata/footprint/conditional_row", 1216},
	"conditional_selected": {"./testdata/footprint/conditional_selected", 1237},
	"expr_arithmetic":      {"./testdata/footprint/expr_arithmetic", 882},
	"expr_selected":        {"./testdata/footprint/expr_selected", 1284},
	"lifecycle_write":      {"./testdata/footprint/lifecycle_write", 391},
	"lifecycle_changes":    {"./testdata/footprint/lifecycle_changes", 558},

	"raw_fragments": {"./testdata/footprint/raw_fragments", 1066},
	"window_rank":   {"./testdata/footprint/window_rank", 1061},
	"window_frames": {"./testdata/footprint/window_frames", 1488},

	"column_encrypted_text":          {"./testdata/footprint/column_encrypted_text", 705},
	"column_nullable_encrypted_text": {"./testdata/footprint/column_nullable_encrypted_text", 931},
	"column_encrypted_json":          {"./testdata/footprint/column_encrypted_json", 795},
	"column_nullable_encrypted_json": {"./testdata/footprint/column_nullable_encrypted_json", 1022},
	"column_password":                {"./testdata/footprint/column_password", 737},
	"column_nullable_password":       {"./testdata/footprint/column_nullable_password", 962},
	"sensitive_models":               {"./models/sensitive", 1884},
	"column_json":                    {"./testdata/footprint/column_json", 821},
	"column_nullable_json":           {"./testdata/footprint/column_nullable_json", 1061},
	"column_interval":                {"./testdata/footprint/column_interval", 706},
	"column_nullable_interval":       {"./testdata/footprint/column_nullable_interval", 931},

	"column_time":   {"./testdata/footprint/column_time", 723},
	"column_enum":   {"./testdata/footprint/column_enum", 785},
	"relation_with": {"./testdata/footprint/relation_with", 1038},

	"model_type":           {"./testdata/footprint/model_type", 299},
	"table_ref":            {"./testdata/footprint/table_ref", 449},
	"onequery":             {"./testdata/footprint/onequery", 457},
	"q_with":               {"./testdata/footprint/q_with", 551},
	"q_select":             {"./testdata/footprint/q_select", 500},
	"q_join":               {"./testdata/footprint/q_join", 774},
	"report_pipeline":      {"./testdata/footprint/report_pipeline", 1077},
	"models":               {"./models", 4098},
	"column_number":        {"./testdata/footprint/column_number", 673},
	"column_nullable_text": {"./testdata/footprint/column_nullable_text", 955},
	"models_1":             {"./testdata/footprint/models_1", 1048},
	"models_3":             {"./testdata/footprint/models_3", 1178},
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
	goBin := "go"
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
	// The retired generated workload is compared with its provenance-checked freeze.
	bundle, err := oracle.Load(oracle.Directory())
	if err != nil {
		t.Fatal(err)
	}
	record, exists := bundle.Records["footprint/./testdata/footprint/extension_slots_old"]
	if !exists || record.Kind != "footprint" {
		t.Fatal("missing historical extension footprint")
	}
	var baseline struct {
		Functions int `json:"functions"`
	}
	if err := json.Unmarshal([]byte(record.Transcript), &baseline); err != nil || baseline.Functions <= 0 {
		t.Fatal("invalid historical extension footprint", err)
	}
	functions["extension_slots_old"] = baseline.Functions
	// Identical four-slot LoadMissing workloads: importer cost must remain well
	// below the generated descriptor graph, not merely below its old ceiling.
	t.Run("extension_namespace_reduction", func(t *testing.T) {
		if functions["extension_slots"]*4 >= functions["extension_slots_old"] {
			t.Fatalf("thin extension namespace must remain below one quarter of the generated baseline: new %d, old %d", functions["extension_slots"], functions["extension_slots_old"])
		}
	})
	// The two packages have identical model shapes. Subtracting their totals
	// removes shared value-type and package overhead; the delta covers two models.
	const measuredGrowth = 130
	maxGrowth := (measuredGrowth*115 + 99) / 100
	growth := functions["models_3"] - functions["models_1"]
	t.Logf("model slope: %d functions for two added models (max %d), %.1f per model", growth, maxGrowth, float64(growth)/2)
	t.Run("model_slope", func(t *testing.T) {
		if growth > maxGrowth {
			t.Errorf("model growth exceeded function budget: measured %d > budget %d; %s", growth, maxGrowth, footprintFailureAdvice)
		}
	})
}

func parseFootprintSymbols(t *testing.T, data []byte) (int, int, int) {
	t.Helper()
	functions, symbols, audit, err := oracle.CountSymbols(data)
	if err != nil {
		t.Fatal(err)
	}
	return functions, symbols, audit
}
