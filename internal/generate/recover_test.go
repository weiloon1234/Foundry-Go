package generate

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestRecoveryAfterForcedProcessTermination(t *testing.T) {
	if !automaticRecovery {
		t.Skip("advisory recovery adapter not implemented on this OS")
	}
	for _, scenario := range []struct {
		after     int
		automatic bool
	}{{1, false}, {2, false}, {1, true}} {
		t.Run(fmt.Sprint(scenario), func(t *testing.T) {
			dir := fixture(t, sampleSource)
			if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(filepath.Join(dir, "user_foundry.gen.go"), 0604); err != nil {
				t.Fatal(err)
			}
			before := generatedSnapshot(t, dir)
			write(t, dir, "models.go", strings.Replace(sampleSource, "Name string;", "Name string; Age int;", 1))
			ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestPublicationCrashHelper$", "--", "--generate-crash", dir, strconv.Itoa(scenario.after))
			output, err := cmd.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			cmd.Stderr = os.Stderr
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
			line, err := bufio.NewReader(output).ReadString('\n')
			if err != nil || line != "ready\n" {
				_ = cmd.Process.Kill()
				_ = cmd.Wait()
				t.Fatalf("crash helper: %q %v", line, err)
			}
			// Recovery must reject an active publisher, even after a file is visible.
			if _, err := Recover(t.Context(), dir); err == nil {
				t.Fatal("recovery entered an active publisher")
			}
			if err := cmd.Process.Kill(); err != nil {
				t.Fatal(err)
			}
			if err := cmd.Wait(); err == nil {
				t.Fatal("helper was not terminated")
			}
			if scenario.automatic {
				if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
					t.Fatal(err)
				}
				if _, err := Generate(t.Context(), Options{Dir: dir, Check: true}); err != nil {
					t.Fatal(err)
				}
			} else {
				recovery, err := Recover(t.Context(), dir)
				if err != nil {
					t.Fatal(err)
				}
				if scenario.after == 1 {
					if !recovery.RolledBack || !reflect.DeepEqual(before, generatedSnapshot(t, dir)) {
						t.Fatalf("incomplete set was not restored: %+v", recovery)
					}
				} else {
					if !recovery.Kept {
						t.Fatalf("complete set was not retained: %+v", recovery)
					}
					if _, err := Generate(t.Context(), Options{Dir: dir, Check: true}); err != nil {
						t.Fatal(err)
					}
				}
			}
			info, err := os.Stat(filepath.Join(dir, "user_foundry.gen.go"))
			if err != nil || info.Mode().Perm() != 0604 {
				t.Fatalf("file mode not preserved: %v", err)
			}
			if _, err := os.Stat(filepath.Join(dir, lockName)); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("recovery retained unfinished staging")
			}
		})
	}
}

func TestPublicationCrashHelper(t *testing.T) {
	if len(os.Args) < 4 || (os.Args[len(os.Args)-3] != "--generate-crash" && os.Args[len(os.Args)-3] != "--generate-graph-crash") {
		return
	}
	dir := os.Args[len(os.Args)-2]
	after, err := strconv.Atoi(os.Args[len(os.Args)-1])
	if err != nil {
		t.Fatal(err)
	}
	g, err := listGraph(t.Context(), dir, os.Args[len(os.Args)-3] == "--generate-graph-crash")
	if err != nil {
		t.Fatal(err)
	}
	// Field-note publications share this crash path; fixtures without notes
	// publish only generated outputs.
	g.fieldDocumentation = true
	writes, err := g.prepare(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	err = publishBatch(t.Context(), g.root, writes, g.checkSourceTree, func(from, to string) error {
		err := os.Rename(from, to)
		if err != nil {
			return err
		}
		calls++
		if calls == after {
			fmt.Fprintln(os.Stdout, "ready")
			time.Sleep(time.Minute)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Fatal("helper reached end without forced termination")
}

func TestRecoveryRefusesChangedTargetsAndDamagedBackups(t *testing.T) {
	if !automaticRecovery {
		t.Skip("advisory recovery adapter not implemented on this OS")
	}
	for _, scenario := range []string{"edited", "mode", "symlink", "backup"} {
		t.Run(scenario, func(t *testing.T) {
			dir := t.TempDir()
			original := []byte(generatedHeader + "\npackage sample\n")
			write(t, dir, "user_foundry.gen.go", string(original))
			before := snapshotFile(t, dir, "user_foundry.gen.go")
			plan := writePlan{changes: map[string][]byte{"user_foundry.gen.go": append(append([]byte{}, original...), []byte("// next\n")...), manifestName: []byte("manifest")}, before: map[string]oldFile{"user_foundry.gen.go": before, manifestName: {}}}
			if err := prepareStaging(dir, plan); err != nil {
				t.Fatal(err)
			}
			if err := os.Rename(filepath.Join(dir, lockName, "new-user_foundry.gen.go"), filepath.Join(dir, "user_foundry.gen.go")); err != nil {
				t.Fatal(err)
			}
			switch scenario {
			case "edited":
				write(t, dir, "user_foundry.gen.go", "user's edit")
			case "mode":
				if err := os.Chmod(filepath.Join(dir, "user_foundry.gen.go"), before.mode^0o200); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				if err := os.Remove(filepath.Join(dir, "user_foundry.gen.go")); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink("models.go", filepath.Join(dir, "user_foundry.gen.go")); err != nil {
					t.Fatal(err)
				}
			case "backup":
				write(t, filepath.Join(dir, lockName), "old-user_foundry.gen.go", "damaged backup")
			}
			if _, err := Recover(t.Context(), dir); err == nil {
				t.Fatal("unsafe recovery accepted")
			}
			if _, err := os.Stat(filepath.Join(dir, lockName, journalName)); err != nil {
				t.Fatal("failed recovery lost journal")
			}
			if scenario == "edited" {
				data, err := os.ReadFile(filepath.Join(dir, "user_foundry.gen.go"))
				if err != nil || string(data) != "user's edit" {
					t.Fatal("user edit overwritten")
				}
			}
		})
	}
}

func TestRecoveryBeforeJournalAndUnsafeJournal(t *testing.T) {
	if !automaticRecovery {
		t.Skip("advisory recovery adapter not implemented on this OS")
	}
	dir := t.TempDir()
	lock := filepath.Join(dir, lockName)
	if err := os.Mkdir(lock, 0700); err != nil {
		t.Fatal(err)
	}
	write(t, lock, stagingProtocol, "1\n")
	write(t, lock, "new-user_foundry.gen.go", "unpublished")
	if result, err := Recover(t.Context(), dir); err != nil || !result.Found || result.Kept || result.RolledBack {
		t.Fatalf("unpublished recovery: %+v %v", result, err)
	}
	if _, err := decodeJournal([]byte(`{"version":1,"files":{"../escape.go":{"before":{},"after":{}}}}`)); err == nil {
		t.Fatal("unsafe journal path accepted")
	}
}

func TestRecoveryRestoresDeletedAndRemovesNewOutputs(t *testing.T) {
	if !automaticRecovery {
		t.Skip("advisory recovery adapter not implemented on this OS")
	}
	dir := t.TempDir()
	original := []byte(generatedHeader + "\npackage sample\n")
	write(t, dir, "deleted_foundry.gen.go", string(original))
	deleted := snapshotFile(t, dir, "deleted_foundry.gen.go")
	plan := writePlan{changes: map[string][]byte{"created_foundry.gen.go": original, "deleted_foundry.gen.go": nil, manifestName: []byte("new manifest")}, before: map[string]oldFile{"created_foundry.gen.go": {}, "deleted_foundry.gen.go": deleted, manifestName: {}}}
	if err := prepareStaging(dir, plan); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(dir, lockName, "new-created_foundry.gen.go"), filepath.Join(dir, "created_foundry.gen.go")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, "deleted_foundry.gen.go")); err != nil {
		t.Fatal(err)
	}
	before := generatedSnapshot(t, dir)
	if _, err := Generate(t.Context(), Options{Dir: dir, Check: true}); err == nil {
		t.Fatal("check accepted interrupted publication")
	}
	if !reflect.DeepEqual(before, generatedSnapshot(t, dir)) {
		t.Fatal("check performed recovery")
	}
	result, err := Recover(t.Context(), dir)
	if err != nil || !result.RolledBack {
		t.Fatalf("recovery: %+v, %v", result, err)
	}
	if data, err := os.ReadFile(filepath.Join(dir, "deleted_foundry.gen.go")); err != nil || string(data) != string(original) {
		t.Fatal("deleted output not restored")
	}
	if state(snapshotFile(t, dir, "deleted_foundry.gen.go")) != state(deleted) {
		t.Fatal("deleted output permissions not restored")
	}
	if _, err := os.Stat(filepath.Join(dir, "created_foundry.gen.go")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("new output not removed")
	}
}

func TestRollbackPreservesConcurrentEdits(t *testing.T) {
	dir := t.TempDir()
	original := []byte(generatedHeader + "\npackage sample\n")
	write(t, dir, "user_foundry.gen.go", string(original))
	plan := writePlan{changes: map[string][]byte{"user_foundry.gen.go": append(append([]byte{}, original...), []byte("// new\n")...), manifestName: []byte("manifest")}, before: map[string]oldFile{"user_foundry.gen.go": snapshotFile(t, dir, "user_foundry.gen.go"), manifestName: {}}}
	calls := 0
	err := publishWithRename(t.Context(), &packageInput{dir: dir}, plan, func(from, to string) error {
		calls++
		if calls == 2 {
			write(t, dir, "user_foundry.gen.go", "concurrent edit")
			return errors.New("publish failed")
		}
		return os.Rename(from, to)
	})
	if err == nil || !strings.Contains(err.Error(), "rollback refuses changed target") {
		t.Fatalf("error = %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "user_foundry.gen.go"))
	if err != nil || string(data) != "concurrent edit" {
		t.Fatal("rollback overwrote an edit")
	}
}
