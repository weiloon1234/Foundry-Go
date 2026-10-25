package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func put(t *testing.T, root, name, data string) {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
}

func sourceFixture(t *testing.T) (string, sourcePolicy) {
	t.Helper()
	root := t.TempDir()
	module, err := readModule(".")
	if err != nil {
		t.Fatal(err)
	}
	policy, err := policyFor("../..")
	if err != nil {
		t.Fatal(err)
	}
	put(t, root, "internal/frameworkinfo/module.go", "package frameworkinfo\nconst GenerationManifest = "+strconv.Quote(policy.generationManifest)+"\n")
	put(t, root, "go.mod", "module example.test/Framework\n\ngo "+module.Go.Version+"\n")
	put(t, root, "go.sum", "")
	put(t, root, "LICENSE", "fixture license")
	put(t, root, "record.go", "package framework\n")
	put(t, root, policy.generationManifest, `{"version":1,"files":{}}`)
	put(t, root, ".env.test", "PRIVATE-SENTINEL")
	put(t, root, ".cache/private.json", "PRIVATE-SENTINEL")
	put(t, root, "node_modules/private.json", "PRIVATE-SENTINEL")
	for _, item := range []struct{ dir, name string }{
		{"tests/fixtures/plugin_base", "pluginbase"}, {"tests/fixtures/plugin_dep", "plugindep"}, {"tests/fixtures/consumer", "consumer"}, {"tests/fixtures/goway", "goway"},
	} {
		put(t, root, item.dir+"/go.mod", "module foundry.test/"+item.name+"\n\ngo "+module.Go.Version+"\n\nrequire example.test/Framework v0.0.0\nreplace example.test/Framework => ../../..\n")
		put(t, root, item.dir+"/go.sum", "")
		put(t, root, item.dir+"/fixture.go", "package "+item.name+"\n")
	}
	put(t, root, "tests/fixtures/consumer/productionprofile/record.go", "package productionprofile\n")
	put(t, root, "tests/fixtures/consumer/configuredprofile/routes.go", "package configuredprofile\n")
	put(t, root, "tests/fixtures/consumer/localization/messages.go", "package localization\n")
	put(t, root, "tests/fixtures/consumer/localization/locales/ms/messages.json", "{\"welcome\":\"Selamat datang\"}\n")
	return root, policy
}

func TestPrivateCandidateHasCanonicalIndependentModulesAndPreservesOwnership(t *testing.T) {
	root, policy := sourceFixture(t)
	var first manifest
	for round := range 2 {
		out := filepath.Join(t.TempDir(), "candidate")
		if err := prepare(options{root: root, out: out, version: "v0.0.0-candidate.24"}); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(filepath.Join(out, "manifest.json"))
		if err != nil {
			t.Fatal(err)
		}
		var report manifest
		if err := json.Unmarshal(data, &report); err != nil {
			t.Fatal(err)
		}
		if len(report.Artifacts) != 3 || len(report.Consumers) != 4 || report.Format != 3 {
			t.Fatal("incomplete release manifest")
		}
		for i, item := range report.Artifacts {
			archive, err := zip.OpenReader(filepath.Join(out, filepath.FromSlash(item.Zip)))
			if err != nil {
				t.Fatal(err)
			}
			var ownership, license bool
			for _, file := range archive.File {
				if strings.Contains(file.Name, ".env") || strings.Contains(file.Name, ".cache/") || strings.Contains(file.Name, "node_modules/") || strings.Contains(file.Name, "tests/fixtures/") {
					t.Fatal("private or nested source leaked", file.Name)
				}
				ownership = ownership || strings.HasSuffix(file.Name, "/"+policy.generationManifest)
				license = license || strings.HasSuffix(file.Name, "/LICENSE")
			}
			if err := archive.Close(); err != nil {
				t.Fatal(err)
			}
			if !license || (i == 0 && !ownership) || !strings.HasPrefix(item.Sum, "h1:") {
				t.Fatal("artifact lost license, ownership, or hash")
			}
			if round == 1 && (item.SHA256 != first.Artifacts[i].SHA256 || item.Sum != first.Artifacts[i].Sum) {
				t.Fatal("identical source did not produce identical module bytes")
			}
		}
		for _, name := range []string{"full", "ordinary", "configured", "goway"} {
			file, err := readModule(filepath.Join(out, "consumers", name))
			if err != nil {
				t.Fatal(err)
			}
			if len(file.Replace) != 0 || file.Require[0].Mod.Version != "v0.0.0-candidate.24" {
				t.Fatal("consumer still depends on workspace replacement")
			}
		}
		for _, name := range []string{"productionprofile/record.go", "configuredprofile/routes.go", "localization/messages.go", "localization/locales/ms/messages.json"} {
			if _, err := os.Stat(filepath.Join(out, "consumers/configured", name)); err != nil {
				t.Fatal("configured profile lost required source", err)
			}
		}
		for _, name := range []string{"configuredprofile", "localization"} {
			if _, err := os.Stat(filepath.Join(out, "consumers/ordinary", name)); !os.IsNotExist(err) {
				t.Fatal("ordinary profile acquired configured fixture source", name)
			}
		}
		if err := prepare(options{root: root, out: out, version: "v0.0.0-candidate.24"}); err == nil {
			t.Fatal("existing candidate overwritten")
		}
		current, _ := os.ReadFile(filepath.Join(out, "manifest.json"))
		if !bytes.Equal(current, data) {
			t.Fatal("failed retry mutated accepted artifact")
		}
		first = report
	}
	data, _ := os.ReadFile(filepath.Join(root, "tests/fixtures/consumer/go.mod"))
	if !bytes.Contains(data, []byte("replace example.test/Framework => ../../..")) {
		t.Fatal("source consumer was modified")
	}
}

func TestReleaseRejectsUnknownAssetsLinksAndReplacements(t *testing.T) {
	for _, mode := range []string{"link", "asset", "replace"} {
		t.Run(mode, func(t *testing.T) {
			root, _ := sourceFixture(t)
			switch mode {
			case "link":
				if err := os.Symlink(filepath.Join(root, ".env.test"), filepath.Join(root, "linked.go")); err != nil {
					t.Fatal(err)
				}
			case "asset":
				put(t, root, "unknown.pem", "PRIVATE-SENTINEL")
			case "replace":
				file, err := os.OpenFile(filepath.Join(root, "tests/fixtures/consumer/go.mod"), os.O_APPEND|os.O_WRONLY, 0600)
				if err != nil {
					t.Fatal(err)
				}
				_, err = file.WriteString("replace example.test/unexpected => /private/source\n")
				closeErr := file.Close()
				if err != nil || closeErr != nil {
					t.Fatal(err, closeErr)
				}
			}
			if err := prepare(options{root: root, out: filepath.Join(t.TempDir(), "candidate"), version: "v0.0.0-candidate.24"}); err == nil {
				t.Fatal("unsafe or incoherent source accepted")
			}
		})
	}
}

func TestSourcePolicyIncludesOnlyReviewedImagingAssets(t *testing.T) {
	_, policy := sourceFixture(t)
	for _, name := range []string{"imaging/testdata/avif/anim.avif", "imaging/testdata/avif/PATENTS", "imaging/testdata/webp/frame.png", "imaging/testdata/webp/animation.webp", "imaging/testdata/native/profile.icc", "imaging/testdata/native/oriented.jpg", "imaging/testdata/native/animated.jxl"} {
		if !policy.sourceName(filepath.FromSlash(name)) {
			t.Fatal("reviewed fixture excluded", name)
		}
	}
	for _, name := range []string{"private.jpg", "imaging/testdata/native/private.pem", "other/testdata/profile.icc", "imaging/testdata/native/.private.png", "imaging/testdata/avif/plugin.so"} {
		if policy.sourceName(filepath.FromSlash(name)) {
			t.Fatal("unreviewed asset allowed", name)
		}
	}
}

func TestGoWaySourceInventory(t *testing.T) {
	root := filepath.Join("..", "..")
	policy, err := policyFor(root)
	if err != nil {
		t.Fatal(err)
	}
	files, err := policy.inventory(root)
	if err != nil {
		t.Fatal(err)
	}
	included := make(map[string]bool, len(files))
	for _, file := range files {
		included[file.Path] = true
		if strings.HasPrefix(file.Path, "tests/fixtures/goway/") {
			t.Fatal("nested fixture must remain a separate candidate consumer")
		}
	}
	for _, directory := range []string{
		"database/orm", "internal/ormcore", "internal/modelmeta", "internal/querycore", "internal/noraw", "tests/oracle"} {
		count := 0
		err := filepath.WalkDir(filepath.Join(root, directory), func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !entry.IsDir() && (filepath.Ext(path) == ".go" || directory == "tests/oracle" && strings.Contains(filepath.ToSlash(path), "/testdata/")) {
				count++
				relative, err := filepath.Rel(root, path)
				if err != nil {
					return err
				}
				if !included[filepath.ToSlash(relative)] {
					t.Errorf("source or oracle evidence omitted: %s", relative)
				}
			}
			return nil
		})
		if err != nil || count == 0 {
			t.Fatalf("inventory %s: count=%d, error=%v", directory, count, err)
		}
	}
	for _, name := range []string{
		"blueprint/goway-data-layer/compilefail-mapping.tsv",
		"database/orm/testdata/joined-has.sql",
		"internal/generate/testdata/scaffold/model.golden",
		"internal/generate/testdata/scaffold/extensions.golden",
		"database/orm/testdata/public-methods.txt",
		"database/lifecycle/counts.go",
		"database/json_kind.go",
		"database/orm/expr/aggregate_rows.go",
		"database/orm/rows/cursor_terminal.go",
		"database/orm/rows/scalar_clauses.go",
		"database/orm/rows/correlated_clauses.go",
		"database/orm/rows/read_terminal.go",
		"database/orm/rows/annotated.go",
		"database/orm/expr/relation_aggregate.go",
		"database/orm/rows/record_clauses.go",
		"internal/ormcore/record_iteration.go",
		"database/orm/rows/transactions.md",
		"database/orm/transaction_phase_test.go",
		"docs/evidence/goway-6a12-capability-audit.tsv",
		"docs/evidence/goway-stage9-c-barrier-freeze.tsv",
		"docs/evidence/goway-stage9-d-helper-references.tsv",
		"tools/goway_compare.py",
		"tools/goway_compare_test.py",
		"docs/guides/goway-final-measurements.md",
		"internal/storemigration/testdata/challenge_prune_projection.json",
		"internal/storemigration/testdata/challengestore_entry_read.json",
		"internal/storemigration/testdata/challengestore_entry_write.json",
		"internal/storemigration/testdata/challengestore_subject_read.json",
		"internal/storemigration/testdata/challengestore_subject_write.json",
		"internal/storemigration/testdata/countries_country_read.json",
		"internal/storemigration/testdata/countries_country_write.json",
		"internal/storemigration/testdata/idempotencystore_record_read.json",
		"internal/storemigration/testdata/idempotencystore_record_write.json",
		"internal/storemigration/testdata/jobarchive_failure_read.json",
		"internal/storemigration/testdata/jobarchive_failure_write.json",
		"internal/storemigration/testdata/mfastore_factor_read.json",
		"internal/storemigration/testdata/mfastore_factor_write.json",
		"internal/storemigration/testdata/sessionstore_entry_read.json",
		"internal/storemigration/testdata/sessionstore_entry_write.json",
		"internal/storemigration/testdata/sessionstore_subject_read.json",
		"internal/storemigration/testdata/sessionstore_subject_write.json",
		"internal/storemigration/testdata/tokenstore_entry_read.json",
		"internal/storemigration/testdata/tokenstore_entry_write.json",
		"internal/storemigration/testdata/tokenstore_family_read.json",
		"internal/storemigration/testdata/tokenstore_family_write.json",
		"internal/storemigration/testdata/tokenstore_subject_read.json",
		"internal/storemigration/testdata/tokenstore_subject_write.json",
		"internal/storemigration/testdata/stage7_3b_attachmentstore_file_delete_each.json",
		"internal/storemigration/testdata/stage7_3b_attachmentstore_file_identity.json",
		"internal/storemigration/testdata/stage7_3b_attachmentstore_file_insert.json",
		"internal/storemigration/testdata/stage7_3b_attachmentstore_file_read.json",
		"internal/storemigration/testdata/stage7_3b_attachmentstore_file_update_each.json",
		"internal/storemigration/testdata/stage7_3b_attachmentstore_file_uuid_seek.json",
		"internal/storemigration/testdata/stage7_3b_attachmentstore_file_write.json",
		"internal/storemigration/testdata/stage7_3b_attachmentstore_variant_identity.json",
		"internal/storemigration/testdata/stage7_3b_attachmentstore_variant_insert.json",
		"internal/storemigration/testdata/stage7_3b_attachmentstore_variant_read.json",
		"internal/storemigration/testdata/stage7_3b_attachmentstore_variant_uuid_seek.json",
		"internal/storemigration/testdata/stage7_3b_attachmentstore_variant_write.json",
		"internal/storemigration/testdata/stage7_3b_auditstore_entry_identity.json",
		"internal/storemigration/testdata/stage7_3b_auditstore_entry_insert.json",
		"internal/storemigration/testdata/stage7_3b_auditstore_entry_prune.json",
		"internal/storemigration/testdata/stage7_3b_auditstore_entry_read.json",
		"internal/storemigration/testdata/stage7_3b_auditstore_entry_write.json",
		"internal/storemigration/testdata/stage7_3b_extensionstore_meta_identity.json",
		"internal/storemigration/testdata/stage7_3b_extensionstore_meta_insert.json",
		"internal/storemigration/testdata/stage7_3b_extensionstore_meta_read.json",
		"internal/storemigration/testdata/stage7_3b_extensionstore_meta_write.json",
		"internal/storemigration/testdata/stage7_3b_extensionstore_setting_identity.json",
		"internal/storemigration/testdata/stage7_3b_extensionstore_setting_insert.json",
		"internal/storemigration/testdata/stage7_3b_extensionstore_setting_read.json",
		"internal/storemigration/testdata/stage7_3b_extensionstore_setting_write.json",
		"internal/storemigration/testdata/stage7_3b_extensionstore_translation_identity.json",
		"internal/storemigration/testdata/stage7_3b_extensionstore_translation_insert.json",
		"internal/storemigration/testdata/stage7_3b_extensionstore_translation_read.json",
		"internal/storemigration/testdata/stage7_3b_extensionstore_translation_source_delete.json",
		"internal/storemigration/testdata/stage7_3b_extensionstore_translation_write.json",
		"internal/storemigration/testdata/stage7_3b_notificationstore_delivery_identity.json",
		"internal/storemigration/testdata/stage7_3b_notificationstore_delivery_insert.json",
		"internal/storemigration/testdata/stage7_3b_notificationstore_delivery_read.json",
		"internal/storemigration/testdata/stage7_3b_notificationstore_delivery_write.json",
		"internal/storemigration/testdata/stage7_3b_notificationstore_envelope_identity.json",
		"internal/storemigration/testdata/stage7_3b_notificationstore_envelope_insert.json",
		"internal/storemigration/testdata/stage7_3b_notificationstore_envelope_read.json",
		"internal/storemigration/testdata/stage7_3b_notificationstore_envelope_write.json",
		"internal/storemigration/testdata/stage7_3b_notificationstore_inbox_delete_each.json",
		"internal/storemigration/testdata/stage7_3b_notificationstore_inbox_identity.json",
		"internal/storemigration/testdata/stage7_3b_notificationstore_inbox_insert.json",
		"internal/storemigration/testdata/stage7_3b_notificationstore_inbox_read.json",
		"internal/storemigration/testdata/stage7_3b_notificationstore_inbox_update_each.json",
		"internal/storemigration/testdata/stage7_3b_notificationstore_inbox_write.json",
		"internal/storemigration/testdata/stage7_3b_webhookstore_delivery_identity.json",
		"internal/storemigration/testdata/stage7_3b_webhookstore_delivery_insert.json",
		"internal/storemigration/testdata/stage7_3b_webhookstore_delivery_read.json",
		"internal/storemigration/testdata/stage7_3b_webhookstore_delivery_uuid_seek.json",
		"internal/storemigration/testdata/stage7_3b_webhookstore_delivery_write.json",
		"internal/storemigration/testdata/stage7_3b_webhookstore_endpoint_identity.json",
		"internal/storemigration/testdata/stage7_3b_webhookstore_endpoint_insert.json",
		"internal/storemigration/testdata/stage7_3b_webhookstore_endpoint_read.json",
		"internal/storemigration/testdata/stage7_3b_webhookstore_endpoint_uuid_seek.json",
		"internal/storemigration/testdata/stage7_3b_webhookstore_endpoint_write.json",
		"internal/storemigration/testdata/stage7_3b_webhookstore_secret_identity.json",
		"internal/storemigration/testdata/stage7_3b_webhookstore_secret_insert.json",
		"internal/storemigration/testdata/stage7_3b_webhookstore_secret_read.json",
		"internal/storemigration/testdata/stage7_3b_webhookstore_secret_uuid_seek.json",
		"internal/storemigration/testdata/stage7_3b_webhookstore_secret_write.json",
		"outbox/publisher/testdata/store_append.json",
		"outbox/publisher/testdata/store_exhausted.json",
		"outbox/publisher/testdata/store_failed.json",
		"outbox/publisher/testdata/store_find.json",
		"outbox/publisher/testdata/store_prune.json",
		"outbox/publisher/testdata/store_publish.json",
		"outbox/publisher/testdata/store_requeue.json",
		"outbox/publisher/testdata/store_retry.json",
		"outbox/publisher/testdata/store_stats.json",
		"outbox/publisher/testdata/store_unknown.json",
	} {
		if !included[name] {
			t.Errorf("reviewed Go-way asset omitted: %s", name)
		}
	}
	for _, name := range []string{
		"unreviewed.tsv", "unreviewed.sql", "unreviewed.txt",
		"internal/generate/testdata/scaffold/unreviewed.golden",
		"other/testdata/scaffold/model.golden",
		"database/orm/testdata/unreviewed.sql",
		"database/orm/testdata/unreviewed.txt",
		"other/testdata/joined-has.sql",
		"other/testdata/public-methods.txt",
		"docs/evidence/unreviewed.tsv",
	} {
		if policy.sourceName(filepath.FromSlash(name)) {
			t.Errorf("Go-way asset allowance leaked to %s", name)
		}
	}
	fixture, err := policy.inventory(filepath.Join(root, "tests/fixtures/goway"))
	if err != nil {
		t.Fatal(err)
	}
	present := make(map[string]bool, len(fixture))
	for _, file := range fixture {
		present[file.Path] = true
	}
	for _, name := range []string{"guideexamples/guides_test.go", "guideadapters/snippets_test.go", "guideadapters/examples_test.go", "migration/lifecycle_test.go", "pgsmoke/adjustment_test.go", "testdata/oracles/README.md", "testdata/oracles/comparisons.json", "testdata/oracles/state.json", "cmd/freeze-oracles/main.go", "migration/guide_test.go", "oldmodels/extensions/.foundry-gen.json", "pgsmoke/transaction_composition_test.go", "pgsmoke/integration_adapters_test.go", "go.mod", "golden_test.go", "compiler_batch_test.go", "footprint_test.go", "models/user.go", "oldmodels/models.go", "models/.foundry-gen.json", "oldmodels/.foundry-gen.json", "oldmodels/joined/.foundry-gen.json", "oldmodels/mutators/.foundry-gen.json", "oldmodels/hooks/.foundry-gen.json", "oldmodels/reads/.foundry-gen.json", "testdata/noraw/contracts/probe.go", "testdata/noraw/negative/interfaces.go", "testdata/noraw/negative/probe.go", "testdata/noraw/negative/refs_test.go", "testdata/noraw/negative/signatures.go", "testdata/noraw/positive/probe.go", "testdata/noraw/positive/signatures.go", "testdata/noraw/positive/typed.go", "testdata/noraw/raw.test/probe.go", "testdata/noraw/raw.test/probe_test.go", "relation_writes_test.go", "composition_golden_test.go", "conflict_golden_test.go", "pgsmoke/relation_writes_test.go", "pgsmoke/composition_test.go", "pgsmoke/conflict_test.go", "result_read_golden_test.go", "window_golden_test.go", "scalar_golden_test.go", "conflict_advanced_golden_test.go", "pgsmoke/result_cursor_test.go", "pgsmoke/windows_test.go", "pgsmoke/scalars_test.go", "pgsmoke/conflict_advanced_test.go", "source_write_golden_test.go", "json_path_golden_test.go", "store_capabilities_golden_test.go", "pgsmoke/source_write_test.go", "pgsmoke/json_paths_test.go", "pgsmoke/store_capabilities_test.go"} {
		if !present[name] {
			t.Errorf("Go-way consumer lost %s", name)
		}
	}
}

func TestReviewedConsumerAssets(t *testing.T) {
	policy, err := policyFor("../..")
	if err != nil {
		t.Fatal(err)
	}
	files, err := policy.inventory("../../tests/fixtures/consumer")
	if err != nil {
		t.Fatal(err)
	}
	present := make(map[string]bool, len(files))
	for _, file := range files {
		present[file.Path] = true
	}
	for _, name := range []string{
		"linkqueries/testdata/legacy-read.json",
		"articles/testdata/legacy-model.json",
		"idempotenthttp/testdata/legacy-model.json",
		"isolatedhttp/testdata/legacy-model.json",
		"multifactor/testdata/legacy-model.json",
		"passwords/testdata/legacy-model.json",
		"profiles/testdata/legacy-model.json",
		"teamworkflow/testdata/legacy-model.json",
		"clientcontracts/testdata/datatable-baseline/README.md",
		"clientcontracts/testdata/datatable-baseline/client/.foundry-gen.json",
		"clientcontracts/testdata/datatable-baseline/client/contracts_foundry.gen.ts",
		"clientcontracts/testdata/datatable-baseline/client/contracts_live_foundry.gen.ts",
		"clientcontracts/testdata/datatable-baseline/client/contracts_manifest_foundry.gen.json",
		"clientcontracts/testdata/datatable-baseline/client/contracts_members_foundry.gen.ts",
		"clientcontracts/testdata/datatable-baseline/client/contracts_openapi_foundry.gen.json",
		"clientcontracts/testdata/datatable-baseline/client/contracts_react_foundry.gen.ts",
		"clientcontracts/testdata/datatable-baseline/client/contracts_runtime_foundry.gen.ts",
		"clientcontracts/testdata/datatable-baseline/client/contracts_runtime_realtime_foundry.gen.ts",
		"clientcontracts/testdata/datatable-baseline/client/contracts_vue_foundry.gen.ts",
		"clientcontracts/testdata/datatable-baseline/provenance.json",
		"clientcontracts/testdata/datatable-baseline/reporting/.foundry-gen.json",
		"clientcontracts/testdata/datatable-baseline/reporting/contracts_exports_foundry.gen.ts",
		"clientcontracts/testdata/datatable-baseline/reporting/contracts_foundry.gen.ts",
		"clientcontracts/testdata/datatable-baseline/reporting/contracts_manifest_foundry.gen.json",
		"clientcontracts/testdata/datatable-baseline/reporting/contracts_openapi_foundry.gen.json",
		"clientcontracts/testdata/datatable-baseline/reporting/contracts_react_foundry.gen.ts",
		"clientcontracts/testdata/datatable-baseline/reporting/contracts_reports_foundry.gen.ts",
		"clientcontracts/testdata/datatable-baseline/reporting/contracts_runtime_foundry.gen.ts",
		"clientcontracts/testdata/datatable-baseline/reporting/contracts_runtime_realtime_foundry.gen.ts",
		"clientcontracts/testdata/datatable-baseline/reporting/contracts_vue_foundry.gen.ts",
		"clientcontracts/testdata/reporting/runtime.mjs",
		"clientcontracts/testdata/reporting/types.ts",
		"mailing/testdata/mailers.toml", "spaportals/web/admin/assets/app-3f2a9c.js",
		"spaportals/web/admin/logo.svg", "spaportals/web/home/main.js",
		"spaportals/web/public/admin/shadow.js", "spaportals/web/public/robots.txt",
	} {
		if !present[name] {
			t.Errorf("reviewed fixture omitted: %s", name)
		}
	}
	for _, name := range []string{"credentials.toml", "other.svg", "unreviewed.js", "notes.txt"} {
		if policy.sourceName(name) {
			t.Errorf("asset allowance leaked to %s", name)
		}
	}
}
