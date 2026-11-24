package modelconvert

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"go/format"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var updateConversionGoldens = flag.Bool("update-conversion-goldens", false, "refresh converted model source goldens")

func frameworkRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root, err := moduleRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	return root
}
func fixture(t *testing.T, source string) string {
	t.Helper()
	root := frameworkRoot(t)
	mod, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	data := strings.Replace(string(mod), "module "+framework, "module foundry.test/conversion", 1) + "\nrequire " + framework + " v0.0.0\nreplace " + framework + " => " + root + "\n"
	write(t, filepath.Join(dir, "go.mod"), []byte(data))
	sum, err := os.ReadFile(filepath.Join(root, "go.sum"))
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dir, "go.sum"), sum)
	formatted, err := format.Source([]byte(source))
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dir, "models.go"), formatted)
	return dir
}
func write(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
}
func read(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}
func analyze(t *testing.T, dir string) *Plan {
	t.Helper()
	plans, err := Analyze(context.Background(), dir, []string{"."})
	if err != nil {
		for cause := err; cause != nil; cause = errors.Unwrap(cause) {
			t.Log(cause)
		}
		t.Fatal(err)
	}
	if len(plans) != 1 {
		t.Fatalf("plans: %d", len(plans))
	}
	return plans[0]
}
func TestPlainGolden(t *testing.T) {
	dir := fixture(t, string(read(t, "plain.before")))
	plan := analyze(t, dir)
	if len(plan.Refused) != 0 || len(plan.Models) != 1 || len(plan.Files) != 1 {
		t.Fatalf("models=%v refusals=%v files=%d", plan.Models, plan.Refused, len(plan.Files))
	}
	if *updateConversionGoldens {
		write(t, filepath.Join("testdata", "plain.after"), plan.Files[0].After)
	}
	expected, err := format.Source(read(t, "plain.after"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(plan.Files[0].After, expected) {
		t.Fatalf("golden mismatch:\n%s\nwant:\n%s", plan.Files[0].After, expected)
	}
	unchanged, err := os.ReadFile(filepath.Join(dir, "models.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(unchanged, plan.Files[0].Before) {
		t.Fatal("analysis wrote source")
	}
	write(t, filepath.Join(dir, "models.go"), plan.Files[0].After)
	again := analyze(t, dir)
	if len(again.Files) != 0 || len(again.Models) != 0 {
		t.Fatal("already converted source changed")
	}
}
func TestProofPreparedGenerator(t *testing.T) {
	for _, test := range []struct {
		name    string
		changed bool
	}{{"equal", false}, {"column mismatch", true}} {
		t.Run(test.name, func(t *testing.T) {
			dir := fixture(t, string(read(t, "plain.before")))
			plan := analyze(t, dir)
			before, err := os.ReadFile(filepath.Join(dir, "models.go"))
			if err != nil {
				t.Fatal(err)
			}
			generator := GeneratorFunc(func(ctx context.Context, stage string) error {
				data := read(t, "plain.generated")
				if test.changed {
					source, err := os.ReadFile(filepath.Join(stage, "models.go"))
					if err != nil {
						return err
					}
					source = bytes.Replace(source, []byte("json:\"name\""), []byte("json:\"name\" foundry:\"column=label\""), 1)
					if err = os.WriteFile(filepath.Join(stage, "models.go"), source, 0600); err != nil {
						return err
					}
				}
				return os.WriteFile(filepath.Join(stage, "models_foundry.gen.go"), data, 0600)
			})
			verified, err := Prove(context.Background(), plan, ProofOptions{Generator: generator, FrameworkDirectory: frameworkRoot(t), ScratchDirectory: t.TempDir()})
			if test.changed {
				if err == nil || !strings.Contains(err.Error(), "columns[1].name") {
					t.Fatalf("column difference not reported: %v", err)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				if len(verified.Files()) != 2 {
					t.Fatalf("files %d", len(verified.Files()))
				}
				called := false
				if err := verified.Publish(context.Background(), publishFunc(func(_ context.Context, _ string, files []File) error { called = true; return nil })); err != nil {
					t.Fatal(err)
				}
				if !called {
					t.Fatal("publisher not called")
				}
				write(t, filepath.Join(dir, "models.go"), append(before, '\n'))
				if err := verified.Publish(context.Background(), publishFunc(func(context.Context, string, []File) error { t.Fatal("published stale sources"); return nil })); err == nil {
					t.Fatal("stale proof accepted")
				}
				write(t, filepath.Join(dir, "models.go"), before)
			}
			current, _ := os.ReadFile(filepath.Join(dir, "models.go"))
			if !bytes.Equal(before, current) {
				t.Fatal("proof modified original")
			}
		})
	}
}

type publishFunc func(context.Context, string, []File) error

func (f publishFunc) Publish(c context.Context, d string, files []File) error { return f(c, d, files) }

func TestInventoryExceptionsGolden(t *testing.T) {
	dir := fixture(t, string(read(t, "exceptions.before")))
	plan := analyze(t, dir)
	if len(plan.Refused) > 0 {
		t.Fatalf("refused: %+v", plan.Refused)
	}
	if len(plan.Models) != 7 {
		t.Fatalf("models %d", len(plan.Models))
	}
	if *updateConversionGoldens {
		write(t, filepath.Join("testdata", "exceptions.after"), plan.Files[0].After)
	}
	expected, err := format.Source(read(t, "exceptions.after"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(plan.Files[0].After, expected) {
		t.Fatalf("golden mismatch:\n%s\nwant:\n%s", plan.Files[0].After, expected)
	}
	for _, model := range plan.Models {
		if model.Name == "Page" && (len(model.Slots) != 1 || model.Slots[0] != "PageTitle.Describe()" || len(model.Behaviors) != 1 || !model.Behaviors[0].Inline) {
			t.Fatalf("slot/inline provenance missing: %+v", model)
		}
	}
}

func TestRefusals(t *testing.T) {
	const base = `package models
import "github.com/weiloon1234/Foundry-Go/database/orm"
type User struct {ID int64; Name string}
type UserTable struct {orm.Table[User,int64];Name orm.Text[User,string]}
var Users=orm.Define("users",func(b orm.Builder[User],u *User)UserTable{return UserTable{orm.TableOf(b,&u.ID),orm.TextOf(b,&u.Name)}})
`
	cases := []struct{ name, source, reason string }{
		{"field without binding", strings.ReplaceAll(strings.ReplaceAll(base, ";Name orm.Text[User,string]", ""), ",orm.TextOf(b,&u.Name)", ""), "has no binding"},
		{"custom option", strings.TrimSuffix(base, "\n") + "\nfunc option()orm.Option{return orm.NoTimestamps()}\n", "custom table option"},
		{"dynamic construction", strings.Replace(base, "return UserTable{", "if u.ID!=0 {panic(\"dynamic\")};return UserTable{", 1), "dynamic construction"},
		{"unresolved literal", strings.Replace(base, "return UserTable{orm.TableOf(b,&u.ID),orm.TextOf(b,&u.Name)}", "result:=UserTable{orm.TableOf(b,&u.ID),orm.TextOf(b,&u.Name)};return result", 1), "unresolved table literal"},
		{"binding to other model", strings.Replace(base, "return UserTable{", "other:=new(User);return UserTable{", 1), "binding has no matching model field"},
	}
	cases[1].source = strings.Replace(cases[1].source, "orm.TextOf(b,&u.Name)}})", "orm.TextOf(b,&u.Name)}},option())", 1)
	cases[4].source = strings.Replace(cases[4].source, "orm.TextOf(b,&u.Name)", "orm.TextOf(b,&other.Name)", 1)
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			plan := analyze(t, fixture(t, test.source))
			if len(plan.Files) != 0 || len(plan.Refused) != 1 || !strings.Contains(plan.Refused[0].Reason, test.reason) {
				t.Fatalf("files %d refusals %+v; want %s", len(plan.Files), plan.Refused, test.reason)
			}
		})
	}
}

func TestBehaviorAndMemberGolden(t *testing.T) {
	plan := analyze(t, fixture(t, string(read(t, "behavior.before"))))
	if len(plan.Refused) > 0 || len(plan.Files) != 1 {
		t.Fatalf("refusals: %+v", plan.Refused)
	}
	if *updateConversionGoldens {
		write(t, filepath.Join("testdata", "behavior.after"), plan.Files[0].After)
	}
	expected, err := format.Source(read(t, "behavior.after"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(plan.Files[0].After, expected) {
		t.Fatalf("got:\n%s\nwant:\n%s", plan.Files[0].After, expected)
	}
}

func TestAnalysisHandlesBlankDeclarationsAndConvertedOutput(t *testing.T) {
	source := string(read(t, "plain.before")) + "\nvar _ = User{}\n"
	dir := fixture(t, source)
	plan := analyze(t, dir)
	if len(plan.Refused) > 0 {
		t.Fatal(plan.Refused)
	}
	write(t, filepath.Join(dir, "models.go"), plan.Files[0].After)
	write(t, filepath.Join(dir, "models_foundry.gen.go"), read(t, "plain.generated"))
	second := analyze(t, dir)
	if len(second.Files) != 0 || len(second.Refused) != 0 {
		t.Fatalf("converted package changed: %+v", second.Refused)
	}
}

func TestExtensionPolicyInitializer(t *testing.T) {
	source := string(read(t, "exceptions.before"))
	source = strings.Replace(source, `var PageTitle=translations.TextOf(PageOwner,"page-title",translations.Options{},extensions.BindSlot(Pages.Key(),Pages.Title))`, `var PageTitle=func()translations.TextHandle[Page,int64]{policy:=translations.Options{};return translations.TextOf(PageOwner,"page-title",policy,extensions.BindSlot(Pages.Key(),Pages.Title))}()`, 1)
	plan := analyze(t, fixture(t, source))
	if len(plan.Refused) > 0 {
		t.Fatal(plan.Refused)
	}
	for _, m := range plan.Models {
		if m.Name == "Page" && (len(m.Slots) != 1 || m.Slots[0] != "PageTitle.Describe()") {
			t.Fatal(m.Slots)
		}
	}
}

func TestProofRejectsChangedBehavior(t *testing.T) {
	source := string(read(t, "plain.before"))
	source = strings.Replace(source, "})\nvar usersIdentity", "},orm.WithHooks(userHooks))\nfunc userHooks()orm.Hooks[User]{return orm.Hooks[User]{}}\nvar usersIdentity", 1)
	dir := fixture(t, source)
	plan := analyze(t, dir)
	if len(plan.Refused) > 0 {
		t.Fatal(plan.Refused)
	}
	generator := GeneratorFunc(func(_ context.Context, stage string) error {
		output := string(read(t, "plain.generated"))
		output = strings.Replace(output, "orm.TextOf(b,&u.Name)}})", "orm.TextOf(b,&u.Name)}},orm.WithHooks(userHooks))", 1)
		if err := os.WriteFile(filepath.Join(stage, "models_foundry.gen.go"), []byte(output), 0600); err != nil {
			return err
		}
		path := filepath.Join(stage, "models.go")
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		data = bytes.Replace(data, []byte("return orm.Hooks[User]{}"), []byte("_ = 1; return orm.Hooks[User]{}"), 1)
		return os.WriteFile(path, data, 0600)
	})
	_, err := Prove(context.Background(), plan, ProofOptions{Generator: generator, FrameworkDirectory: frameworkRoot(t), ScratchDirectory: t.TempDir()})
	if err == nil || !strings.Contains(err.Error(), "behavior body changed") {
		t.Fatalf("changed body not rejected: %v", err)
	}
}

func TestProofExtractedHook(t *testing.T) {
	source := strings.Replace(string(read(t, "plain.before")), "})\nvar usersIdentity", "},orm.WithHooks(func()orm.Hooks[User]{return orm.Hooks[User]{}}))\nvar usersIdentity", 1)
	dir := fixture(t, source)
	plan := analyze(t, dir)
	if len(plan.Refused) > 0 {
		t.Fatal(plan.Refused)
	}
	generator := GeneratorFunc(func(_ context.Context, stage string) error {
		output := strings.Replace(string(read(t, "plain.generated")), "orm.TextOf(b,&u.Name)}})", "orm.TextOf(b,&u.Name)}},orm.WithHooks(userHooks))", 1)
		return os.WriteFile(filepath.Join(stage, "models_foundry.gen.go"), []byte(output), 0600)
	})
	if _, err := Prove(context.Background(), plan, ProofOptions{Generator: generator, FrameworkDirectory: frameworkRoot(t), ScratchDirectory: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
}

func TestDuplicateTableVariablesAreRefused(t *testing.T) {
	plan := analyze(t, fixture(t, string(read(t, "plain.before"))+"\nvar OtherUsers=Users\n"))
	if len(plan.Files) != 0 || len(plan.Refused) != 1 || plan.Refused[0].Model != "User" || !strings.Contains(plan.Refused[0].Reason, "multiple table variables") {
		t.Fatalf("duplicate table not refused: %+v", plan.Refused)
	}
}

func TestSelectedCodecProvenance(t *testing.T) {
	source := string(read(t, "plain.before"))
	source = strings.ReplaceAll(source, "orm.Text", "orm.Column")
	source = strings.Replace(source, "import (", "import (\n\"github.com/weiloon1234/Foundry-Go/database/codec\"", 1)
	source = strings.Replace(source, "orm.ColumnOf(b,&u.Name)", "orm.ColumnOf(b,&u.Name,orm.Codec(nameCodec()))", 1)
	source += "\nfunc nameCodec()codec.Codec[string]{return codec.String[string]()}\nfunc otherCodec()codec.Codec[string]{return codec.String[string]()}\n"
	dir := fixture(t, source)
	plan := analyze(t, dir)
	if len(plan.Refused) > 0 {
		t.Fatal(plan.Refused)
	}
	if err := checkBehaviorBindings(plan.analysis, plan); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "models.go")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	write(t, path, bytes.Replace(data, []byte("orm.Codec(nameCodec())"), []byte("orm.Codec(otherCodec())"), 1))
	changed := analyze(t, dir)
	if err := checkBehaviorBindings(changed.analysis, plan); err == nil {
		t.Fatal("different selected factory with identical codec entry points passed provenance")
	}
}
