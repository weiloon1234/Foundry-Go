package generate

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestFieldDocumentationForcedTermination(t *testing.T) {
	if !automaticRecovery {
		t.Skip("recovery adapter unavailable")
	}
	for _, after := range []int{1, 3} {
		t.Run(strconv.Itoa(after), func(t *testing.T) {
			dir := fixture(t, "package sample\n//foundry:model table=users primary=Key\ntype User struct{Key int;Email string}\nfunc(u User)AccessEmail()(string,error){return u.Email,nil}\n")
			before := snapshotFile(t, dir, "models.go")
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestPublicationCrashHelper$", "--", "--generate-crash", dir, strconv.Itoa(after))
			stdout, err := cmd.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			cmd.Stderr = os.Stderr
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
			line, err := bufio.NewReader(stdout).ReadString('\n')
			if err != nil || line != "ready\n" {
				t.Fatalf("publication helper: %q, %v", line, err)
			}
			if _, err := Recover(t.Context(), dir); err == nil {
				t.Fatal("entered active source publication")
			}
			if err := cmd.Process.Kill(); err != nil {
				t.Fatal(err)
			}
			if err := cmd.Wait(); err == nil {
				t.Fatal("helper was not terminated")
			}
			result, err := Recover(t.Context(), dir)
			if err != nil {
				t.Fatal(err)
			}
			if after == 1 {
				if !result.RolledBack || !bytes.Equal(snapshotFile(t, dir, "models.go").data, before.data) || len(generatedSnapshot(t, dir)) != 0 {
					t.Fatalf("partial source/output publication not restored: %+v", result)
				}
			} else {
				if !result.Kept {
					t.Fatalf("complete source/output publication not retained: %+v", result)
				}
				if _, err := Generate(t.Context(), Options{Dir: dir, Check: true, FieldDocumentation: true}); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestFieldDocumentationRejectsConcurrentSourceEdit(t *testing.T) {
	dir := fixture(t, "package sample\n//foundry:model table=users primary=Key\ntype User struct{Key int;Email string}\nfunc(u User)AccessEmail()(string,error){return u.Email,nil}\n")
	g, err := listGraph(t.Context(), dir, false)
	if err != nil {
		t.Fatal(err)
	}
	g.fieldDocumentation = true
	writes, err := g.prepare(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	original := snapshotFile(t, dir, "models.go")
	edited := append(bytes.Clone(original.data), []byte("// A concurrent user edit.\n")...)
	write(t, dir, "models.go", string(edited))
	if err := publishBatch(t.Context(), dir, writes, g.checkSourceTree, nil); err == nil {
		t.Fatal("concurrent source was overwritten")
	}
	if !bytes.Equal(snapshotFile(t, dir, "models.go").data, edited) || len(generatedSnapshot(t, dir)) != 0 {
		t.Fatal("failed publication changed user data")
	}
}

func TestFieldDocumentationJournalCannotAuthorizeOtherWrites(t *testing.T) {
	data := []byte("package sample\ntype User struct{Email string}\n")
	base := journal{Version: 4, Guards: []string{"."}, FieldNotes: []string{"models.go"}, Files: map[string]journalEntry{"models.go": {Before: state(oldFile{true, data, 0644}), After: state(oldFile{true, data, 0644})}}}
	for _, change := range []func(*journal){
		func(j *journal) { j.Version = 2 },
		func(j *journal) { j.FieldNotes = append(j.FieldNotes, "models.go") },
		func(j *journal) { j.FieldNotes = []string{"missing.go"} },
		func(j *journal) {
			entry := j.Files["models.go"]
			entry.Before = fileState{}
			j.Files["models.go"] = entry
		},
		func(j *journal) { entry := j.Files["models.go"]; entry.After.Mode = 0600; j.Files["models.go"] = entry },
		func(j *journal) { j.Files["arbitrary.go"] = j.Files["models.go"] },
		func(j *journal) {
			j.Files["../models.go"] = j.Files["models.go"]
			delete(j.Files, "models.go")
			j.FieldNotes = []string{"../models.go"}
		},
	} {
		encoded, err := json.Marshal(base)
		if err != nil {
			t.Fatal(err)
		}
		var changed journal
		if err := json.Unmarshal(encoded, &changed); err != nil {
			t.Fatal(err)
		}
		change(&changed)
		encoded, err = json.Marshal(changed)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := decodeJournal(encoded); err == nil {
			t.Fatalf("unsafe source-documentation journal accepted: %s", encoded)
		}
	}
	// A recognized source name does not imply arbitrary directories are allowed.
	if fieldDocumentationName(filepath.Join("child", "models.go")) {
		t.Fatal("package-local source name accepted a directory")
	}
}

func TestFieldDocumentationRecoverySurvivesASecondInterruption(t *testing.T) {
	if !automaticRecovery {
		t.Skip("recovery adapter unavailable")
	}
	for _, mode := range []string{"current", "legacy", "legacy-already-restored"} {
		t.Run(mode, func(t *testing.T) {
			dir := fixture(t, "package sample\ntype User struct{Email string}\n")
			before := snapshotFile(t, dir, "models.go")
			after := []byte("package sample\ntype User struct{\n" + fieldNotePrefix + "Custom getter.\nEmail string}\n")
			generated := "other_foundry.gen.go"
			plan := writePlan{changes: map[string][]byte{"models.go": after, generated: []byte("generated"), manifestName: []byte("next")}, before: map[string]oldFile{"models.go": before, generated: {}, manifestName: {}}, guards: []string{"."}, fieldNotes: map[string]bool{"models.go": true}}
			if err := prepareStaging(dir, plan); err != nil {
				t.Fatal(err)
			}
			if mode != "current" {
				if err := os.Remove(filepath.Join(dir, lockName, fieldDocumentationProof("models.go"))); err != nil {
					t.Fatal(err)
				}
			}
			for _, name := range []string{"models.go", generated} {
				if err := os.Rename(filepath.Join(dir, lockName, stageName("new", name)), filepath.Join(dir, name)); err != nil {
					t.Fatal(err)
				}
			}
			if mode != "legacy-already-restored" {
				data, err := os.ReadFile(filepath.Join(dir, lockName, journalName))
				if err != nil {
					t.Fatal(err)
				}
				log, err := decodeJournal(data)
				if err != nil {
					t.Fatal(err)
				}
				if err := validateRecoveryFieldDocumentation(dir, log, map[string]oldFile{"models.go": snapshotFile(t, dir, "models.go")}); err != nil {
					t.Fatal(err)
				}
			}
			// The first rollback restored this source but died before removing the
			// generated target or staging. A second recovery must complete safely.
			if err := os.WriteFile(filepath.Join(dir, "models.go"), before.data, before.mode); err != nil {
				t.Fatal(err)
			}
			result, err := Recover(t.Context(), dir)
			if err != nil || !result.RolledBack || state(snapshotFile(t, dir, "models.go")) != state(before) {
				t.Fatal("second recovery lost source or proof", result, err)
			}
			if _, err := os.Stat(filepath.Join(dir, generated)); !os.IsNotExist(err) {
				t.Fatal("second recovery retained partial output", err)
			}
		})
	}
}
