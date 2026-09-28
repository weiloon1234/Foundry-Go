package generate

import (
	"context"
	"errors"
	"fmt"
	"github.com/weiloon1234/Foundry-Go/testkit"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const sampleSource = `package sample

//foundry:enum
type Status string
const (
 Active Status = "active"
 Disabled Status = "disabled"
)

//foundry:model table=users primary=Key
type User struct { Key int; Name string; State Status }

func (u User) Draft() UserDraft { return UserDraft{}.SetName(u.Name) }
var initial = UserDraft{}.SetName("initial")
`

func fixture(t *testing.T, source string) string {
	testkit.TrackExternalInputs(t)
	t.Helper()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	mod, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	moduleLine := "module " + framework + "\n"
	if !strings.HasPrefix(string(mod), moduleLine) {
		t.Fatal("root go.mod has an unexpected module declaration")
	}
	sum, err := os.ReadFile(filepath.Join(root, "go.sum"))
	if err != nil {
		t.Fatal(err)
	}
	// Keep approved dependency requirements/checksums in sync with the actual
	// framework, as a consumer would after resolving its framework dependency.
	fixtureMod := strings.Replace(string(mod), moduleLine, "module foundry.test/generator\n", 1)
	fixtureMod += fmt.Sprintf("\nrequire %s v0.0.0\nreplace %s => %q\n", framework, framework, root)
	// macOS can return a /var temporary path whose canonical form is /private/var.
	// Private graph helpers require the same canonical root as Go package discovery.
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	write(t, dir, "go.mod", fixtureMod)
	write(t, dir, "go.sum", string(sum))
	write(t, dir, "models.go", source)
	return dir
}
func write(t *testing.T, dir, name, data string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

func snapshotFile(t *testing.T, dir, name string) oldFile {
	t.Helper()
	file, err := readFile(filepath.Join(dir, name))
	if err != nil || !file.exists {
		t.Fatalf("read fixture snapshot %s: %v", name, err)
	}
	return file
}
func generatedSnapshot(t *testing.T, dir string) map[string]string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	result := make(map[string]string)
	for _, entry := range entries {
		if outputName.MatchString(entry.Name()) || entry.Name() == manifestName {
			data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
			if err != nil {
				t.Fatal(err)
			}
			result[entry.Name()] = string(data)
		}
	}
	return result
}

func TestFreshGenerationStaleCheckAndRegeneration(t *testing.T) {
	dir := fixture(t, sampleSource)
	report, err := Generate(t.Context(), Options{Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Written) != 2 || len(report.Removed) != 0 {
		t.Fatalf("unexpected outputs: %+v", report)
	}
	first := generatedSnapshot(t, dir)
	if strings.Contains(first["user_foundry.gen.go"], dir) {
		t.Fatal("generated output contains an absolute path")
	}
	info, err := os.Stat(filepath.Join(dir, "user_foundry.gen.go"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Generate(t.Context(), Options{Dir: dir, Check: true}); err != nil {
		t.Fatal(err)
	}
	report, err = Generate(t.Context(), Options{Dir: dir})
	if err != nil || len(report.Written) != 0 {
		t.Fatalf("repeat generation changed output: %+v %v", report, err)
	}
	after, err := os.Stat(filepath.Join(dir, "user_foundry.gen.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, generatedSnapshot(t, dir)) || !after.ModTime().Equal(info.ModTime()) {
		t.Fatal("unchanged output was rewritten")
	}
	if _, err := os.Stat(filepath.Join(dir, lockName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("generation leaked its lock")
	}
	updated := strings.Replace(sampleSource, "Name string;", "Name string; Age int;", 1)
	write(t, dir, "models.go", updated)
	if _, err := Generate(t.Context(), Options{Dir: dir, Check: true}); err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("stale output accepted: %v", err)
	}
	if !reflect.DeepEqual(first, generatedSnapshot(t, dir)) {
		t.Fatal("--check modified output")
	}
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	second := generatedSnapshot(t, dir)
	if !strings.Contains(second["user_foundry.gen.go"], "SetAge") {
		t.Fatal("new field not generated")
	}
	// Full-overlay validation must reject broken business methods, even though
	// declaration-only analysis deliberately ignores their generated references.
	write(t, dir, "models.go", strings.Replace(updated, "SetName(u.Name)", "SetMissing(u.Name)", 1))
	if _, err := Generate(t.Context(), Options{Dir: dir}); err == nil || !strings.Contains(err.Error(), "overlay") {
		t.Fatalf("invalid handwritten method accepted: %v", err)
	}
	if !reflect.DeepEqual(second, generatedSnapshot(t, dir)) {
		t.Fatal("failed overlay check rewrote output")
	}
	write(t, dir, "models.go", strings.Replace(updated, "//foundry:enum\n", "", 1))
	report, err = Generate(t.Context(), Options{Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(report.Removed, []string{"status_foundry.gen.go"}) {
		t.Fatalf("obsolete output not removed: %+v", report)
	}
}

func TestFreshDeclarationsWithGeneratedAliasesAndUnrelatedConstants(t *testing.T) {
	dir := fixture(t, `package sample
import "unsafe"
//foundry:enum
type State uint8
type StateAlias = State
const (
 Idle StateAlias = iota
 Active
)
const AliasActive = Active + 1
const Size = unsafe.Sizeof(initial)
type DraftAlias = UserDraft
var initial = DraftAlias{}
var driver = "domain variable with the same name as an emitted import"
var enum = "another emitted import name"
//foundry:model table=users primary=Key
type User struct {Key int; State State; Computed DraftAlias `+"`foundry:\"-\"`"+`}
`)
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	output := generatedSnapshot(t, dir)["state_foundry.gen.go"]
	if !strings.Contains(output, "AliasActive") || !strings.Contains(output, "driver1") || !strings.Contains(output, "enum1") {
		t.Fatal("enum aliases or import collision handling were lost")
	}
	if _, err := Generate(t.Context(), Options{Dir: dir, Check: true}); err != nil {
		t.Fatal(err)
	}
	write(t, dir, "bad.go", "package sample\ntype Invalid = DoesNotExist\n")
	if _, err := Generate(t.Context(), Options{Dir: dir}); err == nil || !strings.Contains(err.Error(), "overlay") {
		t.Fatalf("unrelated declaration errors were suppressed: %v", err)
	}
}

func TestInvalidDeclarationsDoNotWrite(t *testing.T) {
	for _, test := range []struct{ name, source, want string }{
		{"missing table", "//foundry:model\ntype User struct {ID int}", "valid table"},
		{"unknown option", "//foundry:model table=users primry=ID\ntype User struct {ID int}", "unsupported model option"},
		{"missing primary", "//foundry:model table=users\ntype User struct {Name string}", "missing primary"},
		{"untyped primary", "//foundry:model table=users\ntype User struct {ID int}", "default primary"},
		{"duplicate column", "//foundry:model table=users primary=ID\ntype User struct {ID int; Other int `foundry:\"column=id\"`}", "duplicate column"},
		{"unsupported field", "//foundry:model table=users primary=ID\ntype User struct {ID int; Values []string}", "unsupported persisted field"},
		{"unknown field option", "//foundry:model table=users primary=ID\ntype User struct {ID int `foundry:\"colum=id\"`}", "unsupported foundry field tag"},
		{"invalid default", "//foundry:model table=users primary=ID\ntype User struct {ID int `foundry:\"default=now()\"`}", "unsupported foundry field tag"},
		{"malformed tag", "//foundry:model table=users primary=ID\ntype User struct {ID int `foundry:\"column=id`}", "malformed quoted struct tag"},
		{"duplicate table", "//foundry:model table=users primary=ID\ntype User struct {ID int}\n//foundry:model table=users primary=ID\ntype Other struct {ID int}", "duplicate model table"},
		{"enum duplicate", "//foundry:enum\ntype Status string\nconst(A Status=\"same\";B Status=\"same\")", "duplicate serialized values"},
		{"enum invalid utf8", "//foundry:enum\ntype Status string\nconst A Status=\"\\xff\"", "valid UTF-8"},
		{"enum pointer integer", "//foundry:enum\ntype Status uintptr\nconst A Status=1", "string or integer underlying type"},
		{"model alias", "//foundry:model table=users primary=ID\ntype User = struct {ID int}", "defined type"},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := fixture(t, "package sample\n"+test.source+"\n")
			if _, err := Generate(t.Context(), Options{Dir: dir}); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("expected %q: %v", test.want, err)
			}
			if len(generatedSnapshot(t, dir)) != 0 {
				t.Fatal("invalid declaration wrote files")
			}
		})
	}
}

func TestImportedEnumRetainsScalarOperatorsAndPackageName(t *testing.T) {
	dir := fixture(t, `package sample
import kind "foundry.test/generator/value-kinds"
//foundry:model table=users primary=Key
type User struct{Key int; State kind.Status}
`)
	values := filepath.Join(dir, "value-kinds")
	if err := os.Mkdir(values, 0755); err != nil {
		t.Fatal(err)
	}
	write(t, values, "values.go", "package kinds\n//foundry:enum\ntype Status string\nconst Active Status=\"active\"\n")
	if _, err := Generate(t.Context(), Options{Dir: values}); err != nil {
		t.Fatal(err)
	}
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	output := generatedSnapshot(t, dir)["user_foundry.gen.go"]
	if !strings.Contains(output, "ScalarField[FoundryScope, kinds.Status]") || !strings.Contains(output, "type UserFieldSet = UserScopedFieldSet[User]") {
		t.Fatal("imported enum lost its owner, package name, or operator restriction")
	}
	write(t, dir, "invalid.go", "package sample\nvar invalid = UserFields().State.Like(\"a%\")\n")
	if _, err := Generate(t.Context(), Options{Dir: dir}); err == nil || !strings.Contains(err.Error(), "Like undefined") {
		t.Fatalf("imported enum accepted text operator: %v", err)
	}
}

func TestGeneratedSQLIntegerRangeAndExactContract(t *testing.T) {
	dir := fixture(t, "package sample\n//foundry:enum\ntype Huge uint64\nconst(Safe Huge=1;Excessive Huge=1<<63)\n")
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	write(t, dir, "codec_test.go", `package sample
import "testing"
func TestBoundary(t *testing.T){
 if _,err:=Excessive.Value();err==nil{t.Fatal("SQL signed integer overflow accepted")}
 if v,err:=Safe.Value();err!=nil||v!=int64(1){t.Fatal("SQL integer representation changed")}
 definition,err:=Excessive.EnumDescriptor().Definition();if err!=nil||string(definition.Cases[1].Value)!="9223372036854775808"{t.Fatal("contract integer precision lost")}
 v:=Safe;if err:=v.Scan(int64(-1));err==nil||v!=Safe{t.Fatal("negative value wrapped into unsigned enum")}
}
`)
	cmd := exec.CommandContext(t.Context(), "go", "test", "-mod=readonly", ".")
	cmd.Dir = dir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated codec behavior: %v\n%s", err, output)
	}
}

func TestGenerationOwnershipAndEmptyPackages(t *testing.T) {
	t.Run("orphaned generated output", func(t *testing.T) {
		dir := fixture(t, sampleSource)
		write(t, dir, "orphan_foundry.gen.go", generatedHeader+"\npackage sample\nvar broken = MissingType{}\n")
		if _, err := Generate(t.Context(), Options{Dir: dir}); err == nil || !strings.Contains(err.Error(), "unowned generated file") {
			t.Fatalf("orphaned generated source was silently ignored: %v", err)
		}
	})
	t.Run("empty", func(t *testing.T) {
		dir := fixture(t, "package sample\n")
		report, err := Generate(t.Context(), Options{Dir: dir})
		if err != nil || len(report.Written) != 0 || len(generatedSnapshot(t, dir)) != 0 {
			t.Fatalf("empty package changed: %+v %v", report, err)
		}
	})
	t.Run("unowned output", func(t *testing.T) {
		dir := fixture(t, sampleSource)
		manual := "package sample\n// handwritten file\n"
		write(t, dir, "user_foundry.gen.go", manual)
		if _, err := Generate(t.Context(), Options{Dir: dir}); err == nil || !strings.Contains(err.Error(), "unowned") {
			t.Fatalf("unowned file replaced: %v", err)
		}
		if generatedSnapshot(t, dir)["user_foundry.gen.go"] != manual {
			t.Fatal("handwritten file changed")
		}
	})
	t.Run("edited generated output", func(t *testing.T) {
		dir := fixture(t, sampleSource)
		if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
			t.Fatal(err)
		}
		snapshot := generatedSnapshot(t, dir)
		write(t, dir, "user_foundry.gen.go", snapshot["user_foundry.gen.go"]+"\n// manual edit\n")
		if _, err := Generate(t.Context(), Options{Dir: dir}); err == nil || !strings.Contains(err.Error(), "edited outside") {
			t.Fatalf("edited output overwritten: %v", err)
		}
	})
	t.Run("unsafe manifest", func(t *testing.T) {
		dir := fixture(t, sampleSource)
		write(t, dir, manifestName, `{"version":1,"files":{"../outside.go":"`+strings.Repeat("0", 64)+`"}}`)
		if _, err := Generate(t.Context(), Options{Dir: dir}); err == nil || !strings.Contains(err.Error(), "invalid owned-file") {
			t.Fatalf("unsafe manifest accepted: %v", err)
		}
	})
}

func TestFieldAliasesAndModelIDOwnership(t *testing.T) {
	source := `package sample
import("github.com/weiloon1234/Foundry-Go/model";"github.com/weiloon1234/Foundry-Go/value")
type Email = string
type UserID = model.ID[User]
//foundry:model table=users
type User struct{ ID UserID; Email Email; Parent value.Nullable[UserID] }
`
	dir := fixture(t, source)
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	output := generatedSnapshot(t, dir)["user_foundry.gen.go"]
	if !strings.Contains(output, "SetEmail(v Email)") || !strings.Contains(output, "SetParent(v UserID)") {
		t.Fatal("field aliases were erased")
	}
	write(t, dir, "models.go", strings.Replace(source, "type UserID = model.ID[User]", "type Other struct{}\ntype UserID = model.ID[Other]", 1))
	if _, err := Generate(t.Context(), Options{Dir: dir}); err == nil || !strings.Contains(err.Error(), "must belong to this model") {
		t.Fatalf("cross-model primary ID accepted: %v", err)
	}
}

func TestPublicationRollsBackFailuresAndCancellation(t *testing.T) {
	for _, cancelMidway := range []bool{false, true} {
		t.Run(fmt.Sprint(cancelMidway), func(t *testing.T) {
			dir := t.TempDir()
			p := &packageInput{dir: dir}
			original := []byte(generatedHeader + "\npackage sample\n")
			write(t, dir, "user_foundry.gen.go", string(original))
			before := snapshotFile(t, dir, "user_foundry.gen.go")
			plan := writePlan{changes: map[string][]byte{"user_foundry.gen.go": append(append([]byte{}, original...), []byte("// updated\n")...), manifestName: []byte("new manifest")}, before: map[string]oldFile{"user_foundry.gen.go": before, manifestName: {}}}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			calls := 0
			failure := errors.New("injected rename failure")
			rename := func(from, to string) error {
				calls++
				if calls == 2 && !cancelMidway {
					return failure
				}
				err := os.Rename(from, to)
				if calls == 1 && cancelMidway {
					cancel()
				}
				return err
			}
			err := publishWithRename(ctx, p, plan, rename)
			if cancelMidway {
				if !errors.Is(err, context.Canceled) {
					t.Fatal(err)
				}
			} else if !errors.Is(err, failure) {
				t.Fatal(err)
			}
			data, readErr := os.ReadFile(filepath.Join(dir, "user_foundry.gen.go"))
			if readErr != nil || string(data) != string(original) {
				t.Fatal("rollback did not restore original")
			}
			if state(snapshotFile(t, dir, "user_foundry.gen.go")) != state(before) {
				t.Fatal("rollback did not restore original file permissions")
			}
			if _, err := os.Stat(filepath.Join(dir, manifestName)); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("failed publication left manifest")
			}
			if _, err := os.Stat(filepath.Join(dir, lockName)); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("successful rollback retained staging")
			}
		})
	}
}
