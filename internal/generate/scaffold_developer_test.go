package generate

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestDeveloperScaffoldsGenerateAndCompileInFreshConsumer(t *testing.T) {
	dir := fixture(t, "package sample\n")
	for _, options := range []ScaffoldOptions{
		{Kind: ModelScaffold, Name: "Widget", Table: "widgets"},
		{Kind: DTOScaffold, Name: "WidgetResponse"},
		{Kind: JobScaffold, Name: "DeliverWidget", ID: "widget.deliver", Queue: "deliveries"},
		{Kind: CommandScaffold, Name: "InspectWidgets", ID: "widgets.inspect"},
	} {
		options.Dir = dir
		path, err := Scaffold(t.Context(), options)
		if err != nil {
			t.Fatal(options.Kind, err)
		}
		before, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(before), generatedHeader) {
			t.Fatal("scaffold became generator-owned")
		}
		if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
			t.Fatal(options.Kind, err)
		}
		after, err := os.ReadFile(path)
		if err != nil || string(after) != string(before) {
			t.Fatal("generation changed consumer-owned scaffold", err)
		}
		if _, err := Scaffold(t.Context(), options); err == nil {
			t.Fatal("existing scaffold overwritten")
		}
	}
	write(t, dir, "scaffolds_test.go", `package sample
import (
 "context"
 "errors"
 "io"
 "strings"
 "testing"
 "github.com/weiloon1234/Foundry-Go/cli"
 "github.com/weiloon1234/Foundry-Go/fault"
)
func TestScaffoldBehavior(t *testing.T){
 if _,err:=QueryWidgets().Compile();err!=nil{t.Fatal(err)}
 if err:=WidgetResponseJSON().Validate();err!=nil{t.Fatal(err)}
 if err:=DeliverWidgetJob().Validate();err!=nil{t.Fatal(err)}
 if err:=HandleDeliverWidget(context.Background(),DeliverWidget{});!errors.Is(err,fault.Invalid){t.Fatal("unfinished job succeeded",err)}
 declaration,err:=InspectWidgetsCommand().Declare(NewInspectWidgets);if err!=nil{t.Fatal(err)}
 registry,err:=cli.New(declaration);if err!=nil{t.Fatal(err)}
 invocation,err:=registry.Parse([]string{"widgets.inspect"},io.Discard);if err!=nil{t.Fatal(err)}
 if err:=invocation.Run(t.Context(),nil,cli.Streams{In:strings.NewReader(""),Out:io.Discard,Err:io.Discard});!errors.Is(err,fault.Invalid){t.Fatal("unfinished command succeeded",err)}
}
`)
	command := exec.CommandContext(t.Context(), "go", "test", "-mod=readonly", "./...")
	command.Dir = dir
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("fresh developer scaffolds: %s %v", output, err)
	}
}

// A model scaffold with slots is checked with the declarations generation
// creates for it, then generated, so the package compiles at once. Its
// attachment policies accept nothing until the application lists media types.
func TestModelScaffoldDeclaresExtensionSlots(t *testing.T) {
	dir := fixture(t, "package sample\n")
	options := ScaffoldOptions{Dir: dir, Kind: ModelScaffold, Name: "Article", Table: "articles", Translated: []string{"Title", "Summary"}, Attachment: []string{"Logo"}, Attachments: []string{"Galleries"}, Metadata: []string{"SEO"}, Disk: "public"}
	path, err := Scaffold(t.Context(), options)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"Title   translations.Text", "Logo      attachments.One[Article]", "Galleries attachments.Many[Article]", "SEO       metadata.Value[ArticleSEO]", "type ArticleSEO struct{}", `var ArticleDisk = storage.DefineDisk("public")`, "func (Article) DefineExtensions() ArticleExtensionSet"} {
		if !strings.Contains(strings.Join(strings.Fields(string(data)), " "), strings.Join(strings.Fields(expected), " ")) {
			t.Fatalf("scaffold lacks %q:\n%s", expected, data)
		}
	}
	if _, err := Generate(t.Context(), Options{Dir: dir, Check: true}); err != nil {
		t.Fatal("scaffold left generation stale", err)
	}
	if _, err := Scaffold(t.Context(), options); err == nil {
		t.Fatal("existing scaffold overwritten")
	}
	// A plain model and translated-only slots need no disk or policy.
	for _, other := range []ScaffoldOptions{{Kind: ModelScaffold, Name: "Tag", Table: "tags", Translated: []string{"Label"}}, {Kind: ModelScaffold, Name: "Plain", Table: "plains"}} {
		other.Dir = dir
		if _, err := Scaffold(t.Context(), other); err != nil {
			t.Fatal(other.Name, err)
		}
	}
	// In a subpackage the new file is checked with its generated output too: a
	// generated name colliding with a handwritten one refuses the scaffold
	// before any file is written.
	models := filepath.Join(dir, "models")
	if err := os.Mkdir(models, 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, models, "doc.go", "package models\n\nfunc PostExtensions() {}\n")
	collision := ScaffoldOptions{Dir: models, Kind: ModelScaffold, Name: "Post", Table: "posts", Attachment: []string{"Cover"}, Disk: "public"}
	if _, err := Scaffold(t.Context(), collision); err == nil {
		t.Fatal("a scaffold colliding with a handwritten declaration was accepted")
	}
	if _, err := os.Stat(filepath.Join(models, "post_model.go")); !os.IsNotExist(err) {
		t.Fatal("a refused scaffold wrote its file", err)
	}
	if err := os.WriteFile(filepath.Join(models, "doc.go"), []byte("package models\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Scaffold(t.Context(), collision); err != nil {
		t.Fatal("subpackage scaffold", err)
	}
	// A plain model is generated by the next ordinary generation, as before.
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	write(t, dir, "slots_test.go", `package sample
import (
 "errors"
 "testing"
 "github.com/weiloon1234/Foundry-Go/fault"
)
func TestScaffoldedSlots(t *testing.T){
 if err:=ArticleExtensionDeclaration().Validate();err!=nil{t.Fatal(err)}
 if len(ArticleExtensions().All())!=5||len(TagExtensions().All())!=1{t.Fatal("slot descriptors")}
 if err:=ArticleSEOJSON().Validate();err!=nil{t.Fatal(err)}
 if err:=ArticleExtensions().Logo.Collection().Validate();!errors.Is(err,fault.Invalid){t.Fatal("policy accepting nothing was valid",err)}
 if _,err:=QueryPlains().Compile();err!=nil{t.Fatal(err)}
}
`)
	command := exec.CommandContext(t.Context(), "go", "test", "-mod=readonly", "./...")
	command.Dir = dir
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("scaffolded slots: %s %v", output, err)
	}
}

func TestDeveloperScaffoldOptionsAndRecoveryAuthority(t *testing.T) {
	for _, options := range []ScaffoldOptions{
		{Kind: ModelScaffold, Name: "Model", Table: "bad; table"},
		{Kind: ModelScaffold, Name: "Model", Table: "records", ID: "ignored"},
		{Kind: DTOScaffold, Name: "DTO", ID: "ignored"},
		{Kind: DTOScaffold, Name: "DTO", Table: "ignored"},
		{Kind: JobScaffold, Name: "Job", ID: "job.id", Queue: "invalid queue"},
		{Kind: CommandScaffold, Name: "../Command", ID: "command.id"},
		{Kind: CommandScaffold, Name: "Command", ID: "command.id", Origin: "ignored"},
		{Kind: ModelScaffold, Name: "Model", Table: "records", Attachment: []string{"Logo"}},
		{Kind: ModelScaffold, Name: "Model", Table: "records", Translated: []string{"Title"}, Disk: "public"},
		{Kind: ModelScaffold, Name: "Model", Table: "records", Attachment: []string{"Logo"}, Disk: "not a disk"},
		{Kind: ModelScaffold, Name: "Model", Table: "records", Translated: []string{"Title"}, Metadata: []string{"Title"}},
		{Kind: ModelScaffold, Name: "Model", Table: "records", Translated: []string{"ID"}},
		{Kind: ModelScaffold, Name: "Model", Table: "records", Metadata: []string{"From"}},
		{Kind: ModelScaffold, Name: "Model", Table: "records", Translated: []string{"title"}},
		{Kind: DTOScaffold, Name: "DTO", Translated: []string{"Title"}},
	} {
		if err := ValidateScaffold(options); err == nil {
			t.Fatal("invalid developer scaffold options accepted")
		}
	}
	for _, name := range []string{"202610010001_admins-v2.create_migration.go", "_hidden_migration.go", ".hidden_migration.go"} {
		if scaffoldName.MatchString(name) != (name[0] != '_' && name[0] != '.') {
			t.Fatal("migration ID file name boundary changed", name)
		}
	}
	for _, kind := range []ScaffoldKind{ModelScaffold, DTOScaffold, JobScaffold, CommandScaffold} {
		name := "record_" + string(kind) + ".go"
		entry := journalEntry{After: state(oldFile{true, []byte("package sample\n"), 0644})}
		for _, version := range []int{3, 7} {
			log := journal{Version: version, Guards: []string{"."}, Files: map[string]journalEntry{name: entry}}
			data, _ := json.Marshal(log)
			_, err := decodeJournal(data)
			if (err == nil) != (version == 7) {
				t.Fatal("legacy journal authority changed", version, kind, err)
			}
			log.Files[name] = journalEntry{Before: entry.After, After: entry.After}
			data, _ = json.Marshal(log)
			if _, err := decodeJournal(data); err == nil {
				t.Fatal("scaffold journal authorized overwrite")
			}
		}
		if !scaffoldName.MatchString(filepath.Base(name)) {
			t.Fatal("valid new scaffold rejected")
		}
	}
}

// A slot model scaffolded with FieldDocumentation runs its generation with
// managed field notes, as foundry generate --field-docs does, so a field-docs
// check of the package is current afterwards.
func TestModelScaffoldGeneratesFieldDocumentation(t *testing.T) {
	dir := fixture(t, "package sample\n")
	path, err := Scaffold(t.Context(), ScaffoldOptions{Dir: dir, Kind: ModelScaffold, Name: "Note", Table: "notes", Translated: []string{"Title"}, FieldDocumentation: true})
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(data), fieldNotePrefix) {
		t.Fatal("the scaffolded model has no managed field notes", err)
	}
	if _, err := Generate(t.Context(), Options{Dir: dir, Check: true, FieldDocumentation: true}); err != nil {
		t.Fatal("field-docs generation is stale after the scaffold", err)
	}
	if err := ValidateScaffold(ScaffoldOptions{Kind: ModelScaffold, Name: "Plain", Table: "plains", FieldDocumentation: true}); err == nil {
		t.Fatal("field docs accepted for a scaffold that runs no generation")
	}
}
