package generate

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	pluginmanifest "github.com/weiloon1234/Foundry-Go/plugin/manifest"
)

func distributionOwner(version pluginmanifest.Version) pluginmanifest.Distribution {
	return pluginmanifest.Distribution{Plugin: "reports", Release: version, Kind: pluginmanifest.Assets, Name: "public"}
}

func distributionSnapshot(t *testing.T, root string) map[string][]byte {
	t.Helper()
	result := make(map[string][]byte)
	err := filepath.WalkDir(root, func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Name() == lockName || strings.HasPrefix(entry.Name(), ".foundry-generate.finished-") {
			return filepath.SkipDir
		}
		if entry.IsDir() || entry.Name() == ".foundry-generate.guard" {
			return nil
		}
		relative, err := filepath.Rel(root, name)
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			target, err := os.Readlink(name)
			if err != nil {
				return err
			}
			result[filepath.ToSlash(relative)] = []byte("symlink:" + target)
			return nil
		}
		data, err := os.ReadFile(name)
		if err != nil {
			return err
		}
		result[filepath.ToSlash(relative)] = data
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func stageDistribution(t *testing.T, root string, owner pluginmanifest.Distribution, outputs map[string][]byte) writePlan {
	t.Helper()
	owned, err := SnapshotDistribution(outputs)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := planWritePolicy(&packageInput{dir: root}, owned, distributionPolicy(owner))
	if err != nil {
		t.Fatal(err)
	}
	if err := planDistributionDirectories(root, &plan); err != nil {
		t.Fatal(err)
	}
	if err := prepareStaging(root, plan); err != nil {
		t.Fatal(err)
	}
	if err := createDistributionDirectories(root, plan); err != nil {
		t.Fatal(err)
	}
	return plan
}

func applyDistributionPart(t *testing.T, root string, plan writePlan, count int) {
	t.Helper()
	for i, name := range publicationOrder(plan.changes) {
		if count >= 0 && i >= count {
			break
		}
		var err error
		if plan.changes[name] == nil {
			err = os.Remove(filepath.Join(root, name))
		} else {
			err = os.Rename(filepath.Join(root, lockName, stageName("new", name)), filepath.Join(root, name))
		}
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestDistributionPublicationOwnsNestedFilesAndRelease(t *testing.T) {
	root := t.TempDir()
	outputs := map[string][]byte{"css/site.css": []byte("body {}"), "js/site.js": []byte("export {};"), "empty.txt": nil}
	if _, err := PublishDistribution(t.Context(), root, distributionOwner("1.0.0"), outputs, true); err == nil {
		t.Fatal("missing distribution accepted by check")
	}
	if entries, err := os.ReadDir(root); err != nil || len(entries) != 0 {
		t.Fatal("check wrote directories", entries, err)
	}
	report, err := PublishDistribution(t.Context(), root, distributionOwner("1.0.0"), outputs, false)
	if err != nil || len(report.Written) != 3 {
		t.Fatal(report, err)
	}
	before := distributionSnapshot(t, root)
	if _, err := os.Stat(filepath.Join(root, "empty.txt")); err != nil {
		t.Fatal("nil bytes became deletion", err)
	}
	for _, check := range []bool{true, false} {
		report, err := PublishDistribution(t.Context(), root, distributionOwner("1.0.0"), outputs, check)
		if err != nil || len(report.Written)+len(report.Removed) != 0 {
			t.Fatal(report, err)
		}
	}
	if !reflect.DeepEqual(before, distributionSnapshot(t, root)) {
		t.Fatal("repeat changed distribution")
	}
	if _, err := decodeManifest(before[manifestName]); err == nil {
		t.Fatal("Go publisher accepted plugin ownership")
	}
	if _, err := decodeManifestPolicy(before[manifestName], artifactOutputPolicy); err == nil {
		t.Fatal("client publisher accepted plugin ownership")
	}
	if err := os.WriteFile(filepath.Join(root, "handwritten.txt"), []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	next := map[string][]byte{"css/site.css": []byte("body { color: blue; }"), "icons/new.svg": []byte("<svg/>")}
	report, err = PublishDistribution(t.Context(), root, distributionOwner("1.1.0"), next, false)
	if err != nil || len(report.Written) != 2 || len(report.Removed) != 2 {
		t.Fatal(report, err)
	}
	if data, err := os.ReadFile(filepath.Join(root, "handwritten.txt")); err != nil || string(data) != "keep" {
		t.Fatal("unowned data changed", err)
	}
	if _, err := PublishDistribution(t.Context(), root, distributionOwner("1.0.0"), outputs, false); err == nil {
		t.Fatal("release downgrade accepted")
	}
	if _, err := PublishDistribution(t.Context(), root, distributionOwner("1.1.0"), nil, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "icons/new.svg")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("owned retired file was retained", err)
	}
}

func TestDistributionRejectsUnsafePathsBeforeWriting(t *testing.T) {
	for _, name := range []string{"../escape", "/absolute", "a/../../escape", "a\\b", "a//b", ".env", ".git/config", "a/.env", "a/CON.txt", "a/end.", "a/../b", "a/", "a\x00b"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			if _, err := PublishDistribution(t.Context(), root, distributionOwner("1.0.0"), map[string][]byte{name: []byte("value")}, false); err == nil {
				t.Fatal("unsafe path accepted")
			}
			if entries, err := os.ReadDir(root); err != nil || len(entries) != 0 {
				t.Fatal("unsafe path wrote output", entries, err)
			}
		})
	}
	for _, outputs := range []map[string][]byte{{"a": nil, "a/b": nil}, {"A.txt": nil, "a.txt": nil}, {"Assets/a": nil, "assets/b": nil}, {"file": make([]byte, MaxDistributionFileBytes+1)}} {
		if _, err := SnapshotDistribution(outputs); err == nil {
			t.Fatal("invalid set accepted")
		}
	}
	for _, scenario := range []string{"edited", "unowned", "foreign-owner", "family", "parent-file", "parent-symlink", "target-symlink"} {
		t.Run(scenario, func(t *testing.T) {
			root := t.TempDir()
			outputs := map[string][]byte{"nested/file.txt": []byte("ours")}
			owner := distributionOwner("1.0.0")
			switch scenario {
			case "edited", "foreign-owner":
				if _, err := PublishDistribution(t.Context(), root, owner, outputs, false); err != nil {
					t.Fatal(err)
				}
				if scenario == "edited" {
					if err := os.WriteFile(filepath.Join(root, "nested/file.txt"), []byte("mine"), 0600); err != nil {
						t.Fatal(err)
					}
				} else {
					owner.Plugin = "other"
				}
			case "unowned":
				if err := os.Mkdir(filepath.Join(root, "nested"), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(root, "nested/file.txt"), []byte("mine"), 0600); err != nil {
					t.Fatal(err)
				}
			case "family":
				if err := os.WriteFile(filepath.Join(root, manifestName), []byte(`{"version":1,"files":{}}`), 0600); err != nil {
					t.Fatal(err)
				}
			case "parent-file":
				if err := os.WriteFile(filepath.Join(root, "nested"), []byte("mine"), 0600); err != nil {
					t.Fatal(err)
				}
			case "parent-symlink":
				if err := os.Symlink(t.TempDir(), filepath.Join(root, "nested")); err != nil {
					t.Fatal(err)
				}
			case "target-symlink":
				if err := os.Mkdir(filepath.Join(root, "nested"), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(filepath.Join(t.TempDir(), "file.txt"), filepath.Join(root, "nested/file.txt")); err != nil {
					t.Fatal(err)
				}
			}
			before := distributionSnapshot(t, root)
			for _, check := range []bool{true, false} {
				if _, err := PublishDistribution(t.Context(), root, owner, outputs, check); err == nil {
					t.Fatal("unsafe destination accepted")
				}
			}
			if !reflect.DeepEqual(before, distributionSnapshot(t, root)) {
				t.Fatal("refusal changed files")
			}
		})
	}
}

func TestDistributionRecoveryUsesOwnershipProofAndSurvivesRepeatedRecovery(t *testing.T) {
	for _, phase := range []string{"partial", "complete", "restored-before-cleanup"} {
		t.Run(phase, func(t *testing.T) {
			root := t.TempDir()
			old := map[string][]byte{"first.txt": []byte("before"), "obsolete.txt": []byte("retire")}
			if _, err := PublishDistribution(t.Context(), root, distributionOwner("1.0.0"), old, false); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(filepath.Join(root, "first.txt"), 0604); err != nil {
				t.Fatal(err)
			}
			before := distributionSnapshot(t, root)
			next := map[string][]byte{"first.txt": []byte("after"), "nested/next.txt": []byte("new")}
			plan := stageDistribution(t, root, distributionOwner("1.1.0"), next)
			raw, err := os.ReadFile(filepath.Join(root, lockName, journalName))
			if err != nil {
				t.Fatal(err)
			}
			log, err := decodeJournal(raw)
			if err != nil || log.Version != 6 {
				t.Fatal(log, err)
			}
			for version := 1; version <= 5; version++ {
				copy := log
				copy.Version = version
				copy.Distribution = nil
				copy.NewDirs = nil
				if version == 1 {
					copy.Guards = nil
				}
				data, _ := json.Marshal(copy)
				if _, err := decodeJournal(data); err == nil {
					t.Fatal("legacy journal gained arbitrary file authority", version)
				}
			}
			count := -1
			if phase == "partial" {
				count = 1
			}
			applyDistributionPart(t, root, plan, count)
			if phase == "restored-before-cleanup" {
				// Simulate another interruption after old files/manifest were
				// restored, after the original new staging files were consumed.
				rename, close, err := confinedRename(root)
				if err != nil {
					t.Fatal(err)
				}
				defer close()
				for _, name := range publicationOrder(plan.changes) {
					if err := restoreFile(root, name, plan.before[name], rename); err != nil {
						t.Fatal(err)
					}
				}
			}
			if _, err := PublishDistribution(t.Context(), root, distributionOwner("1.1.0"), next, true); err == nil {
				t.Fatal("check recovered pending publication")
			}
			recovery, err := Recover(t.Context(), root)
			if err != nil || !recovery.Found || recovery.Kept != (phase == "complete") {
				t.Fatal(recovery, err)
			}
			if phase != "complete" && !reflect.DeepEqual(before, distributionSnapshot(t, root)) {
				t.Fatal("rollback did not restore original files")
			}
			info, err := os.Stat(filepath.Join(root, "first.txt"))
			if err != nil || info.Mode().Perm() != 0604 {
				t.Fatal("recovery lost file mode", err)
			}
		})
	}
}

func TestDistributionRecoveryRefusesTamperedOwnershipBeforeRestoring(t *testing.T) {
	root := t.TempDir()
	plan := stageDistribution(t, root, distributionOwner("1.0.0"), map[string][]byte{"nested/file.txt": []byte("after")})
	applyDistributionPart(t, root, plan, 1)
	before := distributionSnapshot(t, root)
	if err := os.WriteFile(filepath.Join(root, lockName, distributionOwnershipProof), []byte(`{"changed":true}`), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := Recover(t.Context(), root); err == nil {
		t.Fatal("tampered ownership proof accepted")
	}
	if !reflect.DeepEqual(before, distributionSnapshot(t, root)) {
		t.Fatal("recovery changed files before validating ownership")
	}
}

func TestDistributionRecoveryRefusesForgedUnownedFileAndParentSymlink(t *testing.T) {
	for _, scenario := range []string{"unowned-file", "parent-symlink"} {
		t.Run(scenario, func(t *testing.T) {
			root := t.TempDir()
			_ = stageDistribution(t, root, distributionOwner("1.0.0"), map[string][]byte{"nested/file.txt": []byte("after")})
			if scenario == "unowned-file" {
				if err := os.WriteFile(filepath.Join(root, "manual.txt"), []byte("mine"), 0600); err != nil {
					t.Fatal(err)
				}
				file, err := readWithin(root, "manual.txt")
				if err != nil {
					t.Fatal(err)
				}
				raw, err := os.ReadFile(filepath.Join(root, lockName, journalName))
				if err != nil {
					t.Fatal(err)
				}
				log, err := decodeJournal(raw)
				if err != nil {
					t.Fatal(err)
				}
				log.Files["manual.txt"] = journalEntry{Before: state(file)}
				raw, err = json.Marshal(log)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(root, lockName, journalName), raw, 0600); err != nil {
					t.Fatal(err)
				}
			} else {
				outside := t.TempDir()
				if err := os.WriteFile(filepath.Join(outside, "file.txt"), []byte("outside"), 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Remove(filepath.Join(root, "nested")); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(outside, filepath.Join(root, "nested")); err != nil {
					t.Fatal(err)
				}
			}
			before := distributionSnapshot(t, root)
			if _, err := Recover(t.Context(), root); err == nil {
				t.Fatal("unsafe recovery accepted")
			}
			if !reflect.DeepEqual(before, distributionSnapshot(t, root)) {
				t.Fatal("refused recovery changed targets")
			}
		})
	}
}

func TestDistributionRenameFailureRollsBackFilesAndKeepsDirectories(t *testing.T) {
	root := t.TempDir()
	if _, err := PublishDistribution(t.Context(), root, distributionOwner("1.0.0"), map[string][]byte{"first.txt": []byte("before")}, false); err != nil {
		t.Fatal(err)
	}
	before := distributionSnapshot(t, root)
	input := &packageInput{dir: root}
	plan, err := planWritePolicy(input, map[string][]byte{"first.txt": []byte("after"), "nested/new.txt": []byte("new")}, distributionPolicy(distributionOwner("1.1.0")))
	if err != nil {
		t.Fatal(err)
	}
	if err := planDistributionDirectories(root, &plan); err != nil {
		t.Fatal(err)
	}
	calls := 0
	err = publishBatch(t.Context(), root, []packageWrite{{input, plan}}, nil, func(from, to string) error {
		calls++
		if calls == 2 {
			return errors.New("injected publication failure")
		}
		return os.Rename(from, to)
	})
	if err == nil || !reflect.DeepEqual(before, distributionSnapshot(t, root)) {
		t.Fatal("failed publication did not roll back owned files", err)
	}
	if info, err := os.Stat(filepath.Join(root, "nested")); err != nil || !info.IsDir() {
		t.Fatal("new directory was unexpectedly removed", err)
	}
	if _, err := os.Stat(filepath.Join(root, lockName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("successful rollback retained active staging", err)
	}
}

func TestDistributionPartialDirectoryCreationPreservesConcurrentUserFiles(t *testing.T) {
	root := t.TempDir()
	input := &packageInput{dir: root}
	plan, err := planWritePolicy(input, map[string][]byte{"a/new.txt": []byte("a"), "b/new.txt": []byte("b")}, distributionPolicy(distributionOwner("1.0.0")))
	if err != nil {
		t.Fatal(err)
	}
	if err := planDistributionDirectories(root, &plan); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "b"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "b/keep.txt"), []byte("user file"), 0600); err != nil {
		t.Fatal(err)
	}
	before := distributionSnapshot(t, root)
	if err := publishBatch(t.Context(), root, []packageWrite{{input, plan}}, nil, nil); err == nil {
		t.Fatal("changed creation directory accepted")
	}
	if !reflect.DeepEqual(before, distributionSnapshot(t, root)) {
		t.Fatal("partial directory creation changed user files")
	}
	if info, err := os.Stat(filepath.Join(root, "a")); err != nil || !info.IsDir() {
		t.Fatal("partly created directory was removed", err)
	}
}

func TestDistributionRefusesPendingChildPublication(t *testing.T) {
	root := t.TempDir()
	child := filepath.Join(root, "child")
	if err := os.Mkdir(child, 0755); err != nil {
		t.Fatal(err)
	}
	plan, err := planWritePolicy(&packageInput{dir: child}, artifactOutputs("1"), artifactOutputPolicy)
	if err != nil {
		t.Fatal(err)
	}
	plan.guards = []string{"."}
	if err := prepareStaging(child, plan); err != nil {
		t.Fatal(err)
	}
	if _, err := PublishDistribution(t.Context(), root, distributionOwner("1.0.0"), map[string][]byte{"child/note.txt": []byte("new")}, false); err == nil {
		t.Fatal("publication crossed pending child recovery")
	}
	if _, err := os.Stat(filepath.Join(root, manifestName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("root ownership was written before child recovery", err)
	}
}

func FuzzDistributionPaths(f *testing.F) {
	f.Add("css/site.css")
	f.Add("../escape")
	f.Fuzz(func(t *testing.T, name string) {
		outputs, err := SnapshotDistribution(map[string][]byte{name: []byte("value")})
		if err == nil && (!DistributionPath(name) || !bytes.Equal(outputs[name], []byte("value"))) {
			t.Fatal("invalid path snapshot")
		}
	})
}

func TestPublicationOwnershipJSONRejectsDuplicateKeys(t *testing.T) {
	for _, raw := range []string{
		`{"version":1,"version":1,"files":{}}`,
		`{"version":1,"files":{},"files":{}}`,
		`{"version":1,"files":{}} {}`,
	} {
		if _, err := decodeManifest([]byte(raw)); err == nil {
			t.Fatal("ambiguous ownership JSON accepted")
		}
	}
	if _, err := decodeJournal([]byte(`{"version":1,"version":1,"files":{}}`)); err == nil {
		t.Fatal("ambiguous journal version accepted")
	}
}
