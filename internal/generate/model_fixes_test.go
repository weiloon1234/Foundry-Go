package generate

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestDirectiveMisspellings(t *testing.T) {
	for _, kind := range []string{"model", "enum", "path", "dto", "query", "form", "multipart", "message", "config", "union"} {
		for _, prefix := range []string{"//foudry:", "// foundry:", "//foundry :", "//foundry-", "//foundry_"} {
			t.Run(prefix+kind, func(t *testing.T) {
				comment := prefix + kind
				source := "package sample\n" + comment + "\ntype Record struct{}\n"
				if !mayDeclare([]byte(source)) {
					t.Fatal("prefilter skipped misspelling")
				}
				found, err := hasDeclarationDirective("models.go", []byte(source))
				if err != nil || !found {
					t.Fatalf("discovery: %t %v", found, err)
				}
				f, err := parser.ParseFile(token.NewFileSet(), "models.go", source, parser.ParseComments)
				if err != nil {
					t.Fatal(err)
				}
				_, _, err = directive(f.Decls[0].(*ast.GenDecl).Doc)
				if err == nil || !strings.Contains(err.Error(), "use //foundry:"+kind) {
					t.Fatalf("%s: %v", comment, err)
				}
			})
		}
	}
	group := &ast.CommentGroup{List: []*ast.Comment{{Text: "//foundry:unknown"}}}
	if _, _, err := directive(group); err == nil || !strings.Contains(err.Error(), "unsupported Foundry declaration directive") {
		t.Fatalf("unknown directive: %v", err)
	}
	dir := fixture(t, strings.Replace(simpleModel, "//foundry:model", "//foudry:model", 1))
	if _, err := generateFixture(t.Context(), Options{Dir: dir}); err == nil || !strings.Contains(err.Error(), "models.go:") {
		t.Fatalf("missing located typo error: %v", err)
	}
}

func TestTimestampSuggestionsGenerate(t *testing.T) {
	fixtures := newRejectionFixtures(t)
	for _, tc := range []struct{ name, fields, options, suggestion, fix string }{
		{"created", "CreatedAt temporal.DateTime", "", "role=time", `CreatedAt temporal.DateTime ` + "`foundry:\"role=time\"`"},
		{"nullable created", "CreatedAt value.Nullable[temporal.DateTime]", "", "role=time", `CreatedAt value.Nullable[temporal.DateTime] ` + "`foundry:\"role=time\"`"},
		{"default created", "CreatedAt temporal.DateTime `foundry:\"default=database\"`", "", "role=time", "CreatedAt temporal.DateTime `foundry:\"role=time,default=database\"`"},
		{"noninstant pair", "CreatedAt string; UpdatedAt string", "timestamps=none", "timestamps=none", "CreatedAt string; UpdatedAt string"},
		{"noninstant deleted", "DeletedAt string", "softdeletes=none", "softdeletes=none", "DeletedAt string"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pre := `package sample
import("github.com/weiloon1234/Foundry-Go/temporal";"github.com/weiloon1234/Foundry-Go/value")
var _ temporal.DateTime
var _ value.Nullable[int]
//foundry:model table=accounts var=Accounts key=ID `
			dir := fixtures.fixture(t, pre+"\ntype Account struct{ID int;"+tc.fields+"}")
			_, err := generateFixture(t.Context(), Options{Dir: dir})
			if err == nil || !strings.Contains(err.Error(), tc.suggestion) {
				t.Fatalf("suggestion: %v", err)
			}
			write(t, dir, "models.go", pre+tc.options+"\ntype Account struct{ID int;"+tc.fix+"}")
			if _, err := generateFixture(t.Context(), Options{Dir: dir}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestModelCommentStaleness(t *testing.T) {
	for name, changed := range map[string]string{
		"directive": strings.Replace(simpleModel, "table=accounts", "table=other_accounts", 1),
		"field":     strings.Replace(simpleModel, "Name string", "// Name is the revised label.\nName string", 1),
	} {
		t.Run(name, func(t *testing.T) {
			dir := fixture(t, simpleModel)
			if _, err := generateFixture(t.Context(), Options{Dir: dir}); err != nil {
				t.Fatal(err)
			}
			output := generatedSnapshot(t, dir)["models_foundry.gen.go"]
			if !strings.Contains(output, "Directive and field comment changes are checked only by foundry generate --check.") {
				t.Fatal("missing limitation in generated header")
			}
			write(t, dir, "models.go", changed)
			if _, err := generateFixture(t.Context(), Options{Dir: dir, Check: true}); err == nil || !strings.Contains(err.Error(), "stale") {
				t.Fatalf("comment change: %v", err)
			}
			write(t, dir, "guard_test.go", `package sample
import("testing";"github.com/weiloon1234/Foundry-Go/database/orm")
func TestGuard(t *testing.T){if err:=orm.Check(Accounts);err!=nil{t.Fatal(err)}}`)
			cmd := exec.CommandContext(t.Context(), "go", "test", "-count=1", "-p", "4", "-run", "^TestGuard$", ".")
			cmd.Dir = dir
			cmd.Env = append(os.Environ(), "GOWORK=off")
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("%v\n%s", err, out)
			}
		})
	}
}

func TestKeyRoleSuggestionGenerates(t *testing.T) {
	source := strings.Replace(simpleModel, "ID int64", "ID int64 `foundry:\"role=number\"`", 1)
	dir := fixture(t, source)
	if _, err := generateFixture(t.Context(), Options{Dir: dir}); err == nil || !strings.Contains(err.Error(), "role applies only to non-key fields; remove it") {
		t.Fatalf("suggestion: %v", err)
	}
	write(t, dir, "models.go", simpleModel)
	if _, err := generateFixture(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
}
