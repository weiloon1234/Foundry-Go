package modelconvert

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/internal/generate"
)

func TestRemovedDeclarationDocumentation(t *testing.T) {
	source := string(read(t, "plain.before"))
	for _, decl := range []string{"type UserTable", "var Users", "var usersIdentity", "func (u User) FoundryReference", "func (u User) FoundryIdentity"} {
		source = strings.Replace(source, decl, "// Removed declaration documentation.\n"+decl, 1)
	}
	source = strings.Replace(source, " Name orm.Text", " // Member documentation is retained.\n Name orm.Text", 1)
	source = strings.Replace(source, " return UserTable", " // Removed declaration documentation.\n return UserTable", 1)
	source = strings.Replace(source, "var usersIdentity = orm.IdentityOf(Users.Table,Users.Key())", "var usersIdentity = orm.IdentityOf(Users.Table,Users.Key()) // Removed declaration documentation.", 1)
	plan := analyze(t, fixture(t, source))
	if len(plan.Refused) > 0 {
		t.Fatal(plan.Refused)
	}
	got := string(plan.Files[0].After)
	if !strings.Contains(got, "Member documentation is retained.") || strings.Contains(got, "Removed declaration documentation") || !strings.Contains(got, "// User is retained.") || !strings.Contains(got, "// Keep this business declaration.") {
		t.Fatalf("wrong retained documentation:\n%s", got)
	}
}
func TestExtractedFactoryNames(t *testing.T) {
	for _, kind := range []struct{ option, typ, suffix string }{{"WithHooks", "Hooks", "Hooks"}, {"WithGlobalScopes", "GlobalScopes", "Scopes"}, {"WithRetrievalHooks", "RetrievalHooks", "Retrieval"}} {
		t.Run(kind.suffix, func(t *testing.T) {
			factory := "func()orm." + kind.typ + "[User]{return orm." + kind.typ + "[User]{}}"
			if kind.suffix == "Scopes" {
				factory = "func(UserTable) []orm.GlobalScope[User]{return nil}"
			}
			source := strings.Replace(string(read(t, "plain.before")), "})\nvar usersIdentity", "},orm."+kind.option+"("+factory+"))\nvar usersIdentity", 1)
			source += "\nvar user" + kind.suffix + "=1\n"
			plan := analyze(t, fixture(t, source))
			if len(plan.Refused) > 0 {
				t.Fatal(plan.Refused)
			}
			if len(plan.Models) != 1 || len(plan.Models[0].Behaviors) != 1 || plan.Models[0].Behaviors[0].Name != "user"+kind.suffix+"2" {
				t.Fatalf("wrong extracted name: %+v", plan.Models)
			}
		})
	}
}
func TestProofEmbeddedAssetsAndBuildFailure(t *testing.T) {
	for _, broken := range []bool{false, true} {
		t.Run(map[bool]string{false: "assets", true: "build error"}[broken], func(t *testing.T) {
			source := strings.Replace(string(read(t, "plain.before")), "import (", "import (\n\"embed\"", 1) + "\n//go:embed assets/*.json assets/sub\nvar embedded embed.FS\n"
			if broken {
				source += "\nfunc init(){panic(\"proof runtime detail\")}\n"
			}
			dir := fixture(t, source)
			if err := os.MkdirAll(filepath.Join(dir, "assets/sub"), 0700); err != nil {
				t.Fatal(err)
			}
			write(t, filepath.Join(dir, "assets/data.json"), []byte(`{"ok":true}`))
			write(t, filepath.Join(dir, "assets/sub/table.tsv"), []byte("a\tb"))
			plan := analyze(t, dir)
			generator := GeneratorFunc(func(ctx context.Context, stage string) error {
				for _, file := range []string{"assets/data.json", "assets/sub/table.tsv"} {
					if _, err := os.ReadFile(filepath.Join(stage, file)); err != nil {
						return err
					}
				}
				_, err := generate.Generate(ctx, generate.Options{Dir: stage})
				return err
			})
			_, err := Prove(t.Context(), plan, ProofOptions{Generator: generator, FrameworkDirectory: frameworkRoot(t)})
			if broken {
				if err == nil || !strings.Contains(err.Error(), "proof runtime detail") {
					t.Fatalf("missing subprocess error: %v", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
		})
	}
}
func TestProofReportsEveryModelDifference(t *testing.T) {
	source := string(read(t, "plain.before"))
	second := strings.ReplaceAll(source[strings.Index(source, "// User is retained."):], "User", "Account")
	second = strings.ReplaceAll(strings.ReplaceAll(second, "users", "accounts"), "Greeting", "Welcome")
	dir := fixture(t, source+"\n"+second)
	plan := analyze(t, dir)
	before, _ := os.ReadFile(filepath.Join(dir, "models.go"))
	generator := GeneratorFunc(func(ctx context.Context, stage string) error {
		file := filepath.Join(stage, "models.go")
		data, _ := os.ReadFile(file)
		data = bytes.ReplaceAll(data, []byte("json:\"name\""), []byte("json:\"name\" foundry:\"column=label\""))
		write(t, file, data)
		_, err := generate.Generate(ctx, generate.Options{Dir: stage})
		return err
	})
	verified, err := Prove(t.Context(), plan, ProofOptions{Generator: generator, FrameworkDirectory: frameworkRoot(t)})
	if err == nil || verified != nil {
		t.Fatal("changed metadata accepted")
	}
	for _, want := range []string{"models.go:", "model User", "model Account", "Name: name differs", `hand-written "name", generated "label"`} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("missing %q: %v", want, err)
		}
	}
	after, _ := os.ReadFile(filepath.Join(dir, "models.go"))
	if !bytes.Equal(before, after) {
		t.Fatal("refused package changed")
	}
}

func TestSnapshotBuildFailureDiagnostic(t *testing.T) {
	dir := fixture(t, string(read(t, "plain.before")))
	plan := analyze(t, dir)
	write(t, filepath.Join(dir, "broken.go"), []byte("package models\nvar _ = MissingSnapshotSymbol\n"))
	_, err := capture(t.Context(), dir, plan)
	if err == nil || !strings.Contains(err.Error(), "MissingSnapshotSymbol") {
		t.Fatalf("missing compiler error: %v", err)
	}
}

func TestNamedHookDocumentationPreserved(t *testing.T) {
	source := strings.Replace(string(read(t, "plain.before")), "})\nvar usersIdentity", "},orm.WithHooks(userHooks))\nvar usersIdentity", 1)
	source += "\n// userHooks keeps the model append-only.\nfunc userHooks()orm.Hooks[User]{return orm.Hooks[User]{}}\n"
	plan := analyze(t, fixture(t, source))
	generator := GeneratorFunc(func(ctx context.Context, dir string) error {
		_, err := generate.Generate(ctx, generate.Options{Dir: dir})
		return err
	})
	verified, err := Prove(t.Context(), plan, ProofOptions{Generator: generator, FrameworkDirectory: frameworkRoot(t)})
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range verified.Files() {
		if file.Name == "models.go" && !bytes.Contains(file.After, []byte("// userHooks keeps the model append-only.")) {
			t.Fatal("hook documentation removed")
		}
	}
}
