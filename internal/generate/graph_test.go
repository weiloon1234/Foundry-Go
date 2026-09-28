package generate

import (
	"bufio"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

func graphFixture(t *testing.T) string {
	t.Helper()
	dir := fixture(t, `package sample
import "foundry.test/generator/domain"
func Draft() domain.UserDraft {return domain.UserDraft{}.SetState(domain.DefaultState())}
`)
	for _, sub := range []string{"domain", "state"} {
		if err := os.Mkdir(filepath.Join(dir, sub), 0755); err != nil {
			t.Fatal(err)
		}
	}
	write(t, filepath.Join(dir, "state"), "state.go", `package state
//foundry:enum
type State string
const(Active State="active";Disabled State="disabled")
`)
	write(t, filepath.Join(dir, "domain"), "user.go", `package domain
import "foundry.test/generator/state"
//foundry:model table=users primary=Key
type User struct{Key int; State state.State}
func DefaultState() state.State{return state.Active}
func(u User)Draft()UserDraft{return UserDraft{}.SetKey(u.Key).SetState(u.State)}
`)
	return dir
}

func TestGraphCrashRecoveryIsOneDecisionAcrossPackages(t *testing.T) {
	if !automaticRecovery {
		t.Skip("advisory recovery adapter not implemented on this OS")
	}
	for _, after := range []int{3, 4} {
		t.Run(strconv.Itoa(after), func(t *testing.T) {
			dir := graphFixture(t)
			ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestPublicationCrashHelper$", "--", "--generate-graph-crash", dir, strconv.Itoa(after))
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
				t.Fatalf("helper = %q, %v", line, err)
			}
			if _, err := Recover(t.Context(), dir); err == nil {
				t.Fatal("recovered an active graph publisher")
			}
			if err := cmd.Process.Kill(); err != nil {
				t.Fatal(err)
			}
			_ = cmd.Wait()
			// Adding a nested module after the crash must not let a child bypass
			// the scope already recorded in the pending graph journal.
			write(t, filepath.Join(dir, "domain"), "go.mod", "module changed.test/domain\n")
			for _, child := range []string{"domain", "state"} {
				if _, err := Generate(t.Context(), Options{Dir: filepath.Join(dir, child)}); err == nil || !strings.Contains(err.Error(), "pending graph publication") {
					t.Fatalf("child bypassed pending graph: %v", err)
				}
				if _, err := Recover(t.Context(), filepath.Join(dir, child)); err == nil {
					t.Fatal("child recovery bypassed graph decision")
				}
			}
			if err := os.Remove(filepath.Join(dir, "domain", "go.mod")); err != nil {
				t.Fatal(err)
			}
			result, err := Recover(t.Context(), dir)
			if err != nil {
				t.Fatal(err)
			}
			if after == 3 {
				if !result.RolledBack || len(graphSnapshot(t, dir)) != 0 {
					t.Fatalf("partial graph not entirely restored: %+v", result)
				}
				if _, err := Generate(t.Context(), Options{Dir: dir, Recursive: true}); err != nil {
					t.Fatal(err)
				}
			} else if !result.Kept {
				t.Fatalf("complete graph not retained: %+v", result)
			}
			if _, err := Generate(t.Context(), Options{Dir: dir, Recursive: true, Check: true}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestGraphRecoveryRejectsSymlinkDirectoriesAndEscapingJournal(t *testing.T) {
	if !automaticRecovery {
		t.Skip("advisory recovery adapter not implemented on this OS")
	}
	dir := graphFixture(t)
	g, err := listGraph(t.Context(), dir, true)
	if err != nil {
		t.Fatal(err)
	}
	writes, err := g.prepare(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	plan, err := combineWrites(dir, writes)
	if err != nil {
		t.Fatal(err)
	}
	if err := prepareStaging(dir, plan); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.Rename(filepath.Join(dir, "state"), filepath.Join(dir, "original-state")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "state")); err != nil {
		t.Fatal(err)
	}
	if _, err := Recover(t.Context(), dir); err == nil {
		t.Fatal("recovery followed a package symlink")
	}
	entries, err := os.ReadDir(outside)
	if err != nil || len(entries) != 0 {
		t.Fatal("recovery wrote outside its root")
	}
	for _, data := range []string{
		`{"version":2,"guards":[".","../outside"],"files":{"a_foundry.gen.go":{"before":{},"after":{}}}}`,
		`{"version":2,"guards":["."],"files":{"child/a_foundry.gen.go":{"before":{},"after":{}}}}`,
	} {
		if _, err := decodeJournal([]byte(data)); err == nil {
			t.Fatal("invalid graph journal scope accepted")
		}
	}
}
func graphSnapshot(t *testing.T, dir string) map[string]string {
	t.Helper()
	result := make(map[string]string)
	if err := filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if strings.HasPrefix(entry.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if outputName.MatchString(entry.Name()) || entry.Name() == manifestName {
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			relative, err := filepath.Rel(dir, path)
			if err != nil {
				return err
			}
			result[filepath.ToSlash(relative)] = string(data)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return result
}

func TestFreshGraphGenerationAndDependentValidation(t *testing.T) {
	dir := graphFixture(t)
	if _, err := Generate(t.Context(), Options{Dir: dir, Check: true, Recursive: true}); err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("fresh check = %v", err)
	}
	if len(graphSnapshot(t, dir)) != 0 {
		t.Fatal("check published files")
	}
	result, err := Generate(t.Context(), Options{Dir: dir, Recursive: true})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(result.Written, []string{"domain/user_foundry.gen.go", "state/state_foundry.gen.go"}) {
		t.Fatalf("outputs = %+v", result)
	}
	first := graphSnapshot(t, dir)
	if !strings.Contains(first["domain/user_foundry.gen.go"], "ScalarField[FoundryScope, state.State]") || !strings.Contains(first["domain/user_foundry.gen.go"], "type UserFieldSet = UserScopedFieldSet[User]") {
		t.Fatal("imported generated enum lost scalar operations")
	}
	if _, err := Generate(t.Context(), Options{Dir: dir, Check: true, Recursive: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := Generate(t.Context(), Options{Dir: dir, Recursive: true}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, graphSnapshot(t, dir)) {
		t.Fatal("repeat graph generation changed files")
	}
	write(t, dir, "models.go", "package sample\nimport \"foundry.test/generator/domain\"\nvar broken = domain.UserDraft{}.SetMissing(1)\n")
	write(t, filepath.Join(dir, "state"), "state.go", "package state\n//foundry:enum\ntype State string\nconst Active State=\"changed\"\n")
	if _, err := Generate(t.Context(), Options{Dir: dir, Recursive: true}); err == nil || !strings.Contains(err.Error(), "overlay") {
		t.Fatalf("dependent validation = %v", err)
	}
	if !reflect.DeepEqual(first, graphSnapshot(t, dir)) {
		t.Fatal("dependent failure partially published dependencies")
	}
}

func TestGraphCyclesAndDuplicateTables(t *testing.T) {
	cycle := graphFixture(t)
	write(t, filepath.Join(cycle, "state"), "cycle.go", "package state\nimport \"foundry.test/generator/domain\"\nvar User domain.User\n")
	if _, err := Generate(t.Context(), Options{Dir: cycle, Recursive: true}); err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("package cycle accepted: %v", err)
	}
	if len(graphSnapshot(t, cycle)) != 0 {
		t.Fatal("cyclic graph published output")
	}
	dir := graphFixture(t)
	write(t, filepath.Join(dir, "state"), "duplicate.go", "package state\n//foundry:model table=users primary=Key\ntype Other struct{Key int}\n")
	if _, err := Generate(t.Context(), Options{Dir: dir, Recursive: true}); err == nil || !strings.Contains(err.Error(), "duplicate model table") {
		t.Fatalf("duplicate table = %v", err)
	}
	if len(graphSnapshot(t, dir)) != 0 {
		t.Fatal("duplicate table published output")
	}
}

func TestGraphSkipsNestedModulesAndDetectsNewPackages(t *testing.T) {
	dir := graphFixture(t)
	nested := filepath.Join(dir, "separate")
	if err := os.Mkdir(nested, 0755); err != nil {
		t.Fatal(err)
	}
	write(t, nested, "go.mod", "module example.test/separate\n")
	write(t, nested, "invalid.go", "not valid Go")
	g, err := listGraph(t.Context(), dir, true)
	if err != nil {
		t.Fatal(err)
	}
	writes, err := g.prepare(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	added := filepath.Join(dir, "added")
	if err := os.Mkdir(added, 0755); err != nil {
		t.Fatal(err)
	}
	write(t, added, "added.go", "package added\n")
	if err := publishBatch(t.Context(), dir, writes, g.checkSourceTree, nil); err == nil || !strings.Contains(err.Error(), "package/file set changed") {
		t.Fatalf("concurrent package creation = %v", err)
	}
	if len(graphSnapshot(t, dir)) != 0 {
		t.Fatal("changed graph published output")
	}
}

func TestGraphFailureRollsBackAllPackages(t *testing.T) {
	dir := graphFixture(t)
	g, err := listGraph(t.Context(), dir, true)
	if err != nil {
		t.Fatal(err)
	}
	writes, err := g.prepare(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	failure := errors.New("injected graph failure")
	calls := 0
	err = publishBatch(context.Background(), dir, writes, g.checkSourceTree, func(from, to string) error {
		calls++
		if calls == 2 {
			return failure
		}
		return os.Rename(from, to)
	})
	if !errors.Is(err, failure) {
		t.Fatal(err)
	}
	if len(graphSnapshot(t, dir)) != 0 {
		t.Fatal("failed graph retained partially generated packages")
	}
}

func TestGraphRefusesChangedModuleFiles(t *testing.T) {
	dir := graphFixture(t)
	g, err := listGraph(t.Context(), dir, true)
	if err != nil {
		t.Fatal(err)
	}
	writes, err := g.prepare(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(filepath.Join(dir, "go.mod"), os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString("\n// edited during generation\n"); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := publishBatch(t.Context(), dir, writes, g.checkSourceTree, nil); err == nil || !strings.Contains(err.Error(), "module/workspace file") {
		t.Fatalf("module change = %v", err)
	}
	if len(graphSnapshot(t, dir)) != 0 {
		t.Fatal("changed module graph published output")
	}
}
