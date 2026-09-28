package generate

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func migrationScaffold(dir string) ScaffoldOptions {
	return ScaffoldOptions{Dir: dir, Kind: MigrationScaffold, Name: "AddRecords", ID: "202609110001_add_records", Origin: "app", Version: "v0.1.0"}
}

func TestScaffoldsCompileAndRemainConsumerOwned(t *testing.T) {
	dir := fixture(t, "package sample\n")
	options := migrationScaffold(dir)
	path, err := Scaffold(t.Context(), options)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(path) != "add_records_migration.go" {
		t.Fatalf("unexpected scaffold filename: %s", path)
	}
	first, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(first), generatedHeader) {
		t.Fatal("handwritten scaffold marked generator-owned")
	}
	secondDir := fixture(t, "package sample\n")
	secondPath, err := Scaffold(t.Context(), migrationScaffold(secondDir))
	if err != nil {
		t.Fatal(err)
	}
	second, err := os.ReadFile(secondPath)
	if err != nil || string(first) != string(second) {
		t.Fatal("explicit scaffold options produced nondeterministic output")
	}
	if _, err := Scaffold(t.Context(), ScaffoldOptions{Dir: dir, Kind: SeederScaffold, Name: "SeedRegions", ID: "app.regions"}); err != nil {
		t.Fatal(err)
	}
	write(t, dir, "scaffolds_test.go", `package sample
import (
 "testing"
 "errors"
 "github.com/weiloon1234/Foundry-Go/database/migrate"
 "github.com/weiloon1234/Foundry-Go/database/seed"
 "github.com/weiloon1234/Foundry-Go/fault"
)
func TestUnfinishedDefinitionsFail(t *testing.T) {
 if _, err := migrate.New(AddRecords()); !errors.Is(err, fault.Invalid) { t.Fatal("empty migration silently accepted") }
 definition := SeedRegions()
 if _, err := seed.New(definition); err != nil { t.Fatal(err) }
 if err := definition.Run(t.Context(), nil); !errors.Is(err, fault.Invalid) { t.Fatal("unfinished seeder silently succeeded") }
}
`)
	cmd := exec.CommandContext(t.Context(), "go", "test", "-mod=readonly", "./...")
	cmd.Dir = dir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("independent scaffold consumer: %s %v", output, err)
	}
	edited := strings.Replace(string(first), "SQL: []string{}", `SQL: []string{"CREATE TABLE records (id bigint PRIMARY KEY)"}`, 1)
	if edited == string(first) {
		t.Fatal("test did not implement the scaffold")
	}
	write(t, dir, filepath.Base(path), edited)
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	current, err := os.ReadFile(path)
	if err != nil || string(current) != edited || len(generatedSnapshot(t, dir)) != 0 {
		t.Fatal("generator changed or took ownership of handwritten scaffolds")
	}
	if _, err := Scaffold(t.Context(), options); err == nil {
		t.Fatal("scaffolding overwrote an existing file")
	}
	current, _ = os.ReadFile(path)
	if string(current) != edited {
		t.Fatal("repeated scaffold altered user implementation")
	}
}

func TestScaffoldValidatesNamesPackageAndGenerationBeforePublication(t *testing.T) {
	for _, kind := range []string{"invalid-name", "private", "path", "id", "origin", "version", "kind", "seeder-origin", "symbol", "symlink", "canceled", "stale"} {
		t.Run(kind, func(t *testing.T) {
			dir := fixture(t, "package sample\n")
			options := migrationScaffold(dir)
			ctx := t.Context()
			switch kind {
			case "invalid-name":
				options.Name = "not-a-go-name"
			case "private":
				options.Name = "hidden"
			case "path":
				options.Name = "../Escaped"
			case "id":
				options.ID = "bad id"
			case "origin":
				options.Origin = "bad origin"
			case "version":
				options.Version = ""
			case "kind":
				options.Kind = "unknown"
			case "seeder-origin":
				options.Kind = SeederScaffold
			case "symbol":
				write(t, dir, "other.go", "package sample\nconst AddRecordsID = 1\n")
			case "symlink":
				outside := filepath.Join(t.TempDir(), "important.go")
				if err := os.WriteFile(outside, []byte("preserve"), 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(outside, filepath.Join(dir, "add_records_migration.go")); err != nil {
					t.Fatal(err)
				}
			case "canceled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			case "stale":
				write(t, dir, "models.go", sampleSource)
			}
			before, err := goSourceNames(dir)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := Scaffold(ctx, options); err == nil {
				t.Fatal("invalid scaffold accepted")
			}
			after, err := goSourceNames(dir)
			if err != nil || !reflect.DeepEqual(before, after) {
				t.Fatal("failed scaffold published source")
			}
		})
	}
}

func TestScaffoldCreationNeverReplacesRacingFile(t *testing.T) {
	dir := fixture(t, "package sample\n")
	input := &packageInput{dir: dir}
	const name = "add_records_migration.go"
	plan := writePlan{changes: map[string][]byte{name: []byte("package sample\n")}, before: map[string]oldFile{name: {}}, scaffold: true}
	create, close, err := confinedCreate(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer close()
	err = publishBatch(t.Context(), dir, []packageWrite{{input, plan}}, nil, func(from, to string) error {
		if err := os.WriteFile(to, []byte("concurrent editor"), 0600); err != nil {
			return err
		}
		return create(from, to)
	})
	if err == nil {
		t.Fatal("racing target accepted")
	}
	data, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil || string(data) != "concurrent editor" {
		t.Fatal("scaffolding replaced the racing target")
	}
}

func TestScaffoldJournalRecoveryAndCreationOnlyBoundary(t *testing.T) {
	if !automaticRecovery {
		t.Skip("automatic recovery not implemented on this OS")
	}
	for _, scenario := range []string{"unpublished", "published", "edited"} {
		t.Run(scenario, func(t *testing.T) {
			dir := t.TempDir()
			const name = "records_migration.go"
			data := []byte("package sample\n")
			plan := writePlan{changes: map[string][]byte{name: data}, before: map[string]oldFile{name: {}}, guards: []string{"."}, scaffold: true}
			if err := prepareStaging(dir, plan); err != nil {
				t.Fatal(err)
			}
			if scenario != "unpublished" {
				create, close, err := confinedCreate(dir)
				if err != nil {
					t.Fatal(err)
				}
				err = create(filepath.Join(dir, lockName, stageName("new", name)), filepath.Join(dir, name))
				close()
				if err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "edited" {
				write(t, dir, name, "package edited\n")
			}
			report, err := Recover(t.Context(), dir)
			if scenario == "edited" {
				if err == nil {
					t.Fatal("recovery overwrote edited scaffold")
				}
				return
			}
			if err != nil || !report.Found || (scenario == "published" && !report.Kept) || (scenario == "unpublished" && !report.RolledBack) {
				t.Fatalf("recover %s: %+v %v", scenario, report, err)
			}
			if _, err := os.Stat(filepath.Join(dir, lockName)); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("recovery leaked staging")
			}
		})
	}
	for _, kind := range []string{"replace", "delete", "arbitrary-name", "multiple"} {
		log := journal{Version: 3, Guards: []string{"."}, Files: map[string]journalEntry{"records_migration.go": {After: state(oldFile{true, []byte("package sample"), 0644})}}}
		entry := log.Files["records_migration.go"]
		switch kind {
		case "replace":
			entry.Before = entry.After
			log.Files["records_migration.go"] = entry
		case "delete":
			entry.Before = entry.After
			entry.After = fileState{}
			log.Files["records_migration.go"] = entry
		case "arbitrary-name":
			delete(log.Files, "records_migration.go")
			log.Files["main.go"] = entry
		case "multiple":
			log.Files["another_migration.go"] = entry
		}
		encoded, err := json.Marshal(log)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := decodeJournal(encoded); err == nil {
			t.Fatalf("unsafe scaffold journal accepted: %s", kind)
		}
	}
}
