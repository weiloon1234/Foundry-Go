package generate

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func artifactOutputs(value string) map[string][]byte {
	return map[string][]byte{"api_foundry.gen.ts": []byte(ArtifactHeader + "\nexport const value = " + value + ";\n"), "api_foundry.gen.json": []byte(`{"value":` + value + "}\n")}
}

func artifactSnapshot(t *testing.T, dir string) map[string][]byte {
	t.Helper()
	result := make(map[string][]byte)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() || entry.Name() == ".foundry-generate.guard" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		result[entry.Name()] = data
	}
	return result
}

func TestArtifactPublicationOwnsDeterministicSetAndCheckIsReadOnly(t *testing.T) {
	dir := t.TempDir()
	output := artifactOutputs("1")
	if _, err := PublishArtifacts(t.Context(), dir, output, true); err == nil {
		t.Fatal("missing set accepted")
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 0 {
		t.Fatal("check created filesystem entries", err)
	}
	report, err := PublishArtifacts(t.Context(), dir, output, false)
	if err != nil || len(report.Written) != 2 {
		t.Fatal(report, err)
	}
	before := artifactSnapshot(t, dir)
	for _, check := range []bool{true, false} {
		report, err := PublishArtifacts(t.Context(), dir, output, check)
		if err != nil || len(report.Written)+len(report.Removed) != 0 {
			t.Fatal(report, err)
		}
	}
	if !reflect.DeepEqual(before, artifactSnapshot(t, dir)) {
		t.Fatal("repeat changed output")
	}
	if _, err := decodeManifest(before[manifestName]); err == nil {
		t.Fatal("Go publisher accepted client ownership")
	}
	if err := os.WriteFile(filepath.Join(dir, "handwritten.ts"), []byte("user content"), 0600); err != nil {
		t.Fatal(err)
	}
	next := map[string][]byte{"renamed_foundry.gen.ts": output["api_foundry.gen.ts"]}
	report, err = PublishArtifacts(t.Context(), dir, next, false)
	if err != nil || len(report.Removed) != 2 || len(report.Written) != 1 {
		t.Fatal(report, err)
	}
	if data, err := os.ReadFile(filepath.Join(dir, "handwritten.ts")); err != nil || string(data) != "user content" {
		t.Fatal("unowned content changed")
	}
	if _, err := PublishArtifacts(t.Context(), dir, nil, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "renamed_foundry.gen.ts")); !os.IsNotExist(err) {
		t.Fatal("obsolete artifact retained")
	}
}

func TestArtifactPublicationRefusesEditedUnownedAndUnsafeTargets(t *testing.T) {
	for _, scenario := range []string{"edited", "unowned", "orphan", "symlink", "go-family", "invalid-json", "missing-header", "escape"} {
		t.Run(scenario, func(t *testing.T) {
			dir := t.TempDir()
			outputs := artifactOutputs("1")
			switch scenario {
			case "edited":
				if _, err := PublishArtifacts(t.Context(), dir, outputs, false); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, "api_foundry.gen.json"), []byte(`{"mine":true}`), 0600); err != nil {
					t.Fatal(err)
				}
			case "unowned":
				if err := os.WriteFile(filepath.Join(dir, "api_foundry.gen.ts"), outputs["api_foundry.gen.ts"], 0600); err != nil {
					t.Fatal(err)
				}
			case "orphan":
				if err := os.WriteFile(filepath.Join(dir, "orphan_foundry.gen.ts"), outputs["api_foundry.gen.ts"], 0600); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				outside := filepath.Join(t.TempDir(), "owned.ts")
				if err := os.WriteFile(outside, []byte("outside"), 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(outside, filepath.Join(dir, "api_foundry.gen.ts")); err != nil {
					t.Fatal(err)
				}
			case "go-family":
				if err := os.WriteFile(filepath.Join(dir, manifestName), []byte(`{"version":1,"files":{}}`), 0600); err != nil {
					t.Fatal(err)
				}
			case "invalid-json":
				outputs["api_foundry.gen.json"] = []byte("not JSON")
			case "missing-header":
				outputs["api_foundry.gen.ts"] = []byte("export const mine = 1;")
			case "escape":
				outputs["../escape_foundry.gen.ts"] = outputs["api_foundry.gen.ts"]
			}
			for _, check := range []bool{true, false} {
				if _, err := PublishArtifacts(t.Context(), dir, outputs, check); err == nil {
					t.Fatal("unsafe publication accepted")
				}
			}
		})
	}
}

func TestArtifactRecoveryUsesVersionedTargetsAndOriginalModes(t *testing.T) {
	if !automaticRecovery {
		t.Skip("OS has no recovery adapter")
	}
	for _, complete := range []bool{false, true} {
		t.Run(map[bool]string{false: "partial", true: "complete"}[complete], func(t *testing.T) {
			dir := t.TempDir()
			if _, err := PublishArtifacts(t.Context(), dir, artifactOutputs("1"), false); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(filepath.Join(dir, "api_foundry.gen.ts"), 0604); err != nil {
				t.Fatal(err)
			}
			before := artifactSnapshot(t, dir)
			input := &packageInput{dir: dir}
			plan, err := planWritePolicy(input, artifactOutputs("2"), artifactOutputPolicy)
			if err != nil {
				t.Fatal(err)
			}
			plan.guards = []string{"."}
			if err := prepareStaging(dir, plan); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(filepath.Join(dir, lockName, journalName))
			if err != nil {
				t.Fatal(err)
			}
			log, err := decodeJournal(data)
			if err != nil || log.Version != 5 {
				t.Fatal(log, err)
			}
			for version := 1; version <= 4; version++ {
				changed := log
				changed.Version = version
				if version == 1 {
					changed.Guards = nil
				}
				raw, _ := json.Marshal(changed)
				if _, err := decodeJournal(raw); err == nil {
					t.Fatal("old journal gained client artifact authority", version)
				}
			}
			for i, name := range publicationOrder(plan.changes) {
				if !complete && i > 0 {
					break
				}
				if err := os.Rename(filepath.Join(dir, lockName, stageName("new", name)), filepath.Join(dir, name)); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := PublishArtifacts(t.Context(), dir, artifactOutputs("2"), true); err == nil {
				t.Fatal("check recovered staging")
			}
			recovery, err := Recover(t.Context(), dir)
			if err != nil || recovery.Kept != complete || recovery.RolledBack == complete {
				t.Fatal(recovery, err)
			}
			if !complete && !reflect.DeepEqual(before, artifactSnapshot(t, dir)) {
				t.Fatal("incomplete artifact set was not restored")
			}
			if complete && bytes.Equal(before[manifestName], artifactSnapshot(t, dir)[manifestName]) {
				t.Fatal("completed ownership not retained")
			}
			info, err := os.Stat(filepath.Join(dir, "api_foundry.gen.ts"))
			if err != nil || info.Mode().Perm() != 0604 {
				t.Fatal("mode lost", err)
			}
		})
	}
}

func TestArtifactPlanRejectsConcurrentTargetChange(t *testing.T) {
	dir := t.TempDir()
	if _, err := PublishArtifacts(t.Context(), dir, artifactOutputs("1"), false); err != nil {
		t.Fatal(err)
	}
	input := &packageInput{dir: dir}
	plan, err := planWritePolicy(input, artifactOutputs("2"), artifactOutputPolicy)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "api_foundry.gen.json"), []byte(`{"user":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := publishBatch(t.Context(), dir, []packageWrite{{input, plan}}, nil, nil); err == nil {
		t.Fatal("concurrent edit overwritten")
	}
	data, err := os.ReadFile(filepath.Join(dir, "api_foundry.gen.json"))
	if err != nil || string(data) != `{"user":true}` {
		t.Fatal("user edit changed", err)
	}
}
