package generate

import (
	"bytes"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
)

func TestOriginalFieldHelpCommentForms(t *testing.T) {
	for name, test := range map[string]struct {
		source string
		want   string
	}{
		"leading":             {"// Delivery address.\nValue string", "Delivery address."},
		"trailing":            {"Value string // Delivery address.", "Delivery address."},
		"both":                {"// Delivery address.\nValue string // Used for receipts.", "Delivery address.\n\nUsed for receipts."},
		"block":               {"/*\nDelivery address.\n\nUsed for receipts.\n*/\nValue string", "Delivery address.\n\nUsed for receipts."},
		"inline padded block": {"/* Delivery address.\n\nUsed for receipts. */\nValue string", "\tDelivery address.\n\nUsed for receipts."},
		"multiple names":      {"// Delivery address.\nValue, Other string", "Delivery address."},
		"directives":          {"//go:noinline\n// foundry:field\n//foundry:field\nValue string", ""},
		"managed":             {fieldNotePrefix + "Managed copy.\nValue string", ""},
		"ordinary prose":      {"// Mentions foundry:field and go:noinline in prose.\n// Foundry field behavior (generated) is ordinary prose here.\nValue string", "Mentions foundry:field and go:noinline in prose.\nFoundry field behavior (generated) is ordinary prose here."},
		"paragraphs":          {"// Delivery address.\n//\n// Examples use [ordinary brackets].\n//\n//\tmember@example.test\nValue string", "Delivery address.\n\nExamples use [ordinary brackets].\n\n\tmember@example.test"},
	} {
		t.Run(name, func(t *testing.T) {
			p := documentationSource(t, "package sample\ntype Record struct {\n"+test.source+"\n}\n")
			reader := p.sourceDocumentation()
			source, err := reader.structure(p.types.Scope().Lookup("Record").Type())
			if err != nil {
				t.Fatal(err)
			}
			got, err := reader.help(source, "Value")
			if err != nil || string(got) != test.want {
				t.Fatalf("field help = %q, %v; want %q", got, err, test.want)
			}
			if name == "multiple names" {
				other, err := reader.help(source, "Other")
				if err != nil || other != got {
					t.Fatalf("multiple-name help = %q, %v", other, err)
				}
			}
		})
	}
}

func documentationSource(t *testing.T, text string) *packageInput {
	t.Helper()
	fset := token.NewFileSet()
	syntax, err := parser.ParseFile(fset, "source.go", text, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	config := types.Config{Importer: importer.Default()}
	checked, err := config.Check("example.test/source", fset, []*ast.File{syntax}, nil)
	if err != nil {
		t.Fatal(err)
	}
	p := &packageInput{path: checked.Path(), types: checked, fset: fset, files: []source{{name: "source.go", data: []byte(text), syntax: syntax}}, defined: make(map[string]bool), definedAt: make(map[string]token.Pos), methods: make(map[string]map[string]token.Pos)}
	for _, name := range checked.Scope().Names() {
		p.defined[name] = true
	}
	for _, declaration := range syntax.Decls {
		p.recordHandwritten(declaration)
	}
	return p
}

func TestOriginalFieldHelpPreservesSourceLinkMeaning(t *testing.T) {
	p := documentationSource(t, `package sample
import clock "time"
type Display string
func (Display) Render() {}
type Record struct {
 // See [clock.Duration], [Display] and [Display.Render].
 // Keep [Unknown], [ordinary brackets], and map[Display]string as written.
 // See the [reference page].
 //
 // [reference page]: https://example.test/reference
 Value clock.Duration
}
`)
	reader := p.sourceDocumentation()
	source, err := reader.structure(p.types.Scope().Lookup("Record").Type())
	if err != nil {
		t.Fatal(err)
	}
	help, err := reader.help(source, "Value")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"[time.Duration]", "[example.test/source.Display]", "[example.test/source.Display.Render]", "[Unknown]", "[ordinary brackets]", "map[Display]string", "[reference page]: https://example.test/reference"} {
		if !strings.Contains(string(help), want) {
			t.Fatalf("relocated help lost %q: %s", want, help)
		}
	}
	if strings.Contains(string(help), "[clock.Duration]") {
		t.Fatal("source import alias leaked into relocated documentation")
	}
}

func generatedHelp(t *testing.T, data string) map[string]string {
	t.Helper()
	syntax, err := parser.ParseFile(token.NewFileSet(), "generated.go", data, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	result := make(map[string]string)
	for _, declaration := range syntax.Decls {
		switch declaration := declaration.(type) {
		case *ast.GenDecl:
			for _, spec := range declaration.Specs {
				spec, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}
				if structure, ok := spec.Type.(*ast.StructType); ok {
					for _, field := range structure.Fields.List {
						for _, name := range field.Names {
							result[spec.Name.Name+"."+name.Name] = field.Doc.Text()
						}
					}
				}
			}
		case *ast.FuncDecl:
			if declaration.Recv != nil && len(declaration.Recv.List) == 1 {
				result[receiverTypeName(declaration.Recv.List[0].Type)+"."+declaration.Name.Name] = declaration.Doc.Text()
			}
		}
	}
	return result
}

func TestGeneratedOriginalModelHelpFollowsEveryFieldUse(t *testing.T) {
	input := `package sample
import "github.com/weiloon1234/Foundry-Go/value"
//foundry:model table=users primary=Key
type User struct {
 Key int
 // Email reaches the member.
 Email string // Delivery address.
 // Alternate reaches the backup recipient.
 Alternate value.Nullable[string]
}
func (u User) AccessEmail() (string,error) { return u.Email,nil }
func (User) MutateEmail(v string)(string,error) { return v,nil }
`
	dir := fixture(t, input)
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	first := generatedSnapshot(t, dir)
	docs := generatedHelp(t, first["user_foundry.gen.go"])
	for _, symbol := range []string{"UserScopedFieldSet.Email", "UserNullableFieldSet.Email", "UserDraft.Email", "UserDraft.SetEmail", "UserDraft.UnsetEmail", "UserFieldChanges.Email"} {
		if !strings.Contains(docs[symbol], "Email reaches the member.\n\nDelivery address.") {
			t.Fatalf("%s lost original help: %q", symbol, docs[symbol])
		}
	}
	for _, symbol := range []string{"UserScopedFieldSet.Alternate", "UserNullableFieldSet.Alternate", "UserDraft.Alternate", "UserDraft.SetAlternate", "UserDraft.UnsetAlternate", "UserDraft.ClearAlternate"} {
		if !strings.Contains(docs[symbol], "Alternate reaches the backup recipient.") {
			t.Fatalf("%s lost nullable field help: %q", symbol, docs[symbol])
		}
	}
	for _, managed := range []bool{true, false, true} {
		if _, err := Generate(t.Context(), Options{Dir: dir, FieldDocumentation: managed}); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(first, generatedSnapshot(t, dir)) {
			t.Fatal("managed source notes changed original generated help")
		}
		if _, err := Generate(t.Context(), Options{Dir: dir, Check: true, FieldDocumentation: managed}); err != nil {
			t.Fatal(err)
		}
	}
	for _, replacement := range []string{"Updated delivery address.", ""} {
		updated := strings.Replace(input, "Delivery address.", replacement, 1)
		if replacement == "" {
			updated = strings.Replace(updated, "// Email reaches the member.\n", "", 1)
		}
		write(t, dir, "models.go", updated)
		if _, err := Generate(t.Context(), Options{Dir: dir, Check: true}); err == nil || !strings.Contains(err.Error(), string(StaleComments)) {
			t.Fatalf("original help edit was not reported as comments: %v", err)
		}
		if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
			t.Fatal(err)
		}
		next := generatedSnapshot(t, dir)["user_foundry.gen.go"]
		if same, err := sameGoTokens([]byte(first["user_foundry.gen.go"]), []byte(next), false); err != nil || !same {
			t.Fatalf("original help changed executable tokens: %v", err)
		}
		docs := generatedHelp(t, next)
		if replacement == "" {
			if strings.Contains(docs["UserDraft.SetEmail"], "reaches the member") || strings.Contains(docs["UserDraft.SetEmail"], "delivery address") {
				t.Fatal("removed source help survived generation")
			}
		} else if !strings.Contains(docs["UserDraft.SetEmail"], replacement) {
			t.Fatal("edited source help did not reach draft")
		}
		if report, err := Generate(t.Context(), Options{Dir: dir}); err != nil || len(report.Written) != 0 {
			t.Fatalf("repeat original-help generation: %+v %v", report, err)
		}
	}
}

func TestGeneratedConfigurationHelpFollowsDeclarationChains(t *testing.T) {
	dir := fixture(t, `package sample
import "foundry.test/generator/sourcepath"
type time string
type Copy actualname.Options
type Alias = Copy
type Generic[T any] struct {
 // Generic leaf.
 Value T
}
type GenericGroup[T any] struct { Child T }
type CopiedGeneric GenericGroup[struct {
 // Bound anonymous leaf.
 Count int
}]
//foundry:config
type Settings struct {
 // Imported group.
 Imported actualname.Options
 // Copied group.
 Copied Alias
 Generic Generic[int]
 Bound CopiedGeneric
 Anonymous struct {
  // Shared limits.
  Low, High int
  Hidden int `+"`config:\"-\"`"+`
 }
 // Generated group boundary.
 Generated actualname.Generated
 AliasGenerated actualname.GeneratedAlias
 LocalGenerated GeneratedLocal
}
`)
	sourceDir := filepath.Join(dir, "sourcepath")
	if err := os.Mkdir(sourceDir, 0755); err != nil {
		t.Fatal(err)
	}
	write(t, sourceDir, "options.go", `package actualname
import clock "time"
type Label string
type Options struct {
 // Timeout bounds one attempt. See [clock.Duration] and [Label].
 Timeout clock.Duration
 // Inner group.
 Inner struct {
 Count int /* Imported anonymous count. */
 }
}
`)
	write(t, sourceDir, "generated.go", generatedHeader+`
package actualname
type Generated struct {
 // Generated copied help must not become original help.
 Nested Options
}
type GeneratedAlias = Options
`)
	write(t, dir, "generated_local.go", `// Code generated by a test tool; DO NOT EDIT.
package sample
type GeneratedLocal struct {
 // Generated local leaf must not become original help.
 Count int
}
`)
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	output := generatedSnapshot(t, dir)["settings_foundry.gen.go"]
	docs := generatedHelp(t, output)
	for symbol, want := range map[string]string{
		"SettingsConfigKeySet.Imported":           "Imported group.",
		"SettingsConfigKeySet.Copied":             "Copied group.",
		"SettingsImportedConfigKeySet.Timeout":    "Timeout bounds one attempt.",
		"SettingsCopiedConfigKeySet.Timeout":      "Timeout bounds one attempt.",
		"SettingsImportedConfigKeySet.Inner":      "Inner group.",
		"SettingsImportedInnerConfigKeySet.Count": "Imported anonymous count.",
		"SettingsGenericConfigKeySet.Value":       "Generic leaf.",
		"SettingsBoundChildConfigKeySet.Count":    "Bound anonymous leaf.",
		"SettingsAnonymousConfigKeySet.Low":       "Shared limits.",
		"SettingsAnonymousConfigKeySet.High":      "Shared limits.",
		"SettingsConfigKeySet.Generated":          "Generated group boundary.",
	} {
		if !strings.Contains(docs[symbol], want) {
			t.Fatalf("%s missing %q: %q", symbol, want, docs[symbol])
		}
	}
	for _, symbol := range []string{"SettingsGeneratedNestedConfigKeySet.Timeout", "SettingsAliasGeneratedConfigKeySet.Timeout", "SettingsLocalGeneratedConfigKeySet.Count"} {
		if strings.Contains(docs[symbol], "bounds one attempt") || strings.Contains(docs[symbol], "Generated local leaf") {
			t.Fatalf("generated declaration recycled help on %s", symbol)
		}
	}
	if !strings.Contains(docs["SettingsImportedConfigKeySet.Timeout"], "[time.Duration]") || !strings.Contains(docs["SettingsImportedConfigKeySet.Timeout"], "[foundry.test/generator/sourcepath.Label]") {
		t.Fatal("imported links lost source context")
	}
	if strings.Contains(output, `"foundry.test/generator/sourcepath"`) || strings.Contains(output, "[clock.Duration]") {
		t.Fatal("documentation introduced a Go import or retained a source-only alias")
	}
	if !strings.Contains(output, "time1.Duration") {
		t.Fatal("fixture did not exercise a generated import alias collision")
	}
	if _, err := Generate(t.Context(), Options{Dir: dir, Check: true}); err != nil {
		t.Fatal(err)
	}
	original := snapshotFile(t, sourceDir, "options.go")
	for _, replacement := range []string{"Updated imported timeout.", ""} {
		write(t, sourceDir, "options.go", strings.Replace(string(original.data), "Timeout bounds one attempt. See [clock.Duration] and [Label].", replacement, 1))
		if _, err := Generate(t.Context(), Options{Dir: dir, Check: true}); err == nil {
			t.Fatal("imported help edit was reported current")
		}
		if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
			t.Fatal(err)
		}
		next := generatedSnapshot(t, dir)["settings_foundry.gen.go"]
		if same, err := sameGoTokens([]byte(output), []byte(next), false); err != nil || !same {
			t.Fatalf("imported help changed executable tokens: %v", err)
		}
		for _, symbol := range []string{"SettingsImportedConfigKeySet.Timeout", "SettingsCopiedConfigKeySet.Timeout"} {
			help := generatedHelp(t, next)[symbol]
			if strings.Contains(help, "bounds one attempt") || (replacement == "" && strings.Contains(help, "Updated imported timeout.")) || (replacement != "" && !strings.Contains(help, replacement)) {
				t.Fatalf("%s retained stale imported help: %q", symbol, help)
			}
		}
		if report, err := Generate(t.Context(), Options{Dir: dir}); err != nil || len(report.Written) != 0 {
			t.Fatalf("repeat imported-help generation: %+v %v", report, err)
		}
	}
}

func TestGeneratedHelpKeepsDotImportsAndDestinationAliasesDistinct(t *testing.T) {
	dir := fixture(t, `package sample
import (
 . "foundry.test/generator/first"
 . "foundry.test/generator/second"
 stdtime "time"
 weird "foundry.test/generator/clock"
)
var _ stdtime.Duration
type FirstCopy First
type SecondCopy Second
//foundry:config
type Settings struct {
 First FirstCopy
 Second SecondCopy
 Other weird.Duration
 // See [stdtime.Duration], [weird.Duration] and [Unknown].
 Value int
}
`)
	for path, text := range map[string]string{
		"first":  "package first\ntype First struct {\n// First dot import help.\nValue string\n}\n",
		"second": "package second\ntype Second struct {\n// Second dot import help.\nValue string\n}\n",
		"clock":  "package time\ntype Duration int64\n",
	} {
		sourceDir := filepath.Join(dir, path)
		if err := os.Mkdir(sourceDir, 0755); err != nil {
			t.Fatal(err)
		}
		write(t, sourceDir, "source.go", text)
	}
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	output := generatedSnapshot(t, dir)["settings_foundry.gen.go"]
	docs := generatedHelp(t, output)
	for symbol, want := range map[string]string{
		"SettingsFirstConfigKeySet.Value":  "First dot import help.",
		"SettingsSecondConfigKeySet.Value": "Second dot import help.",
		"SettingsConfigKeySet.Value":       "See time.Duration, [foundry.test/generator/clock.Duration] and [Unknown].",
	} {
		if !strings.Contains(docs[symbol], want) {
			t.Fatalf("%s lost source symbol meaning: %q", symbol, docs[symbol])
		}
	}
	if !strings.Contains(output, `time "foundry.test/generator/clock"`) || strings.Contains(output, `"time"`) {
		t.Fatalf("fixture did not retain the generated destination alias: %s", output)
	}
	source := snapshotFile(t, dir, "models.go")
	write(t, dir, "models.go", strings.Replace(string(source.data), " // See [stdtime.Duration], [weird.Duration] and [Unknown].\n", "", 1))
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	withoutHelp := generatedSnapshot(t, dir)["settings_foundry.gen.go"]
	if same, err := sameGoTokens([]byte(output), []byte(withoutHelp), false); err != nil || !same {
		t.Fatalf("documentation link handling changed executable tokens: %v", err)
	}
}

func TestOriginalHelpReachesProjectionJSONAndValidationFields(t *testing.T) {
	dir := fixture(t, `package sample
import (
 "github.com/weiloon1234/Foundry-Go/value"
 "github.com/weiloon1234/Foundry-Go/translations"
)
type Embedded struct {
 // Promoted message title.
 Title string
}
type HiddenPayload struct {
 // Hidden original help must not be recycled through generated aliases.
 Text string
}
type NestedAlias = struct {
 // Nested aliased display text.
 Text string
}
type PointerAlias = *struct {
 // Pointer alias text.
 Text string
}
type OptionalAlias = value.Optional[struct {
 // Optional alias text.
 Text string
}]
type Payload struct {
 Embedded
 Ptr PointerAlias
 Optional OptionalAlias
 GeneratedPointer GeneratedPointerAlias
 GeneratedOptional GeneratedOptionalAlias
 Child NestedAlias
 Hidden GeneratedNestedAlias
 // Preferred display theme.
 Theme string
}
type Rows []struct {
 // Slice item text.
 Text string
}
type Entries map[string]struct {
 // Map item text.
 Text string
}
type Fixed [1]struct {
 // Array item text.
 Text string
}
type RowsCopy Rows
type RowsAlias = Rows
type AnonymousAlias = []struct {
 // Aliased anonymous item text.
 Text string
}
type GenericAlias[T any] = []T
type GenericRows[T any] []T
type GenericCopy GenericRows[struct {
 // Generic copied item text.
 Text string
}]
//foundry:model table=articles primary=Key
type Article struct {
 Key int
 // Translated headline.
 Headline translations.Text
 Payload value.JSON[Payload]
 PointerTop value.JSON[PointerAlias]
 OptionalTop value.JSON[OptionalAlias]
 Rows value.JSON[Rows]
 Entries value.JSON[Entries]
 Fixed value.JSON[Fixed]
 Copied value.JSON[RowsCopy]
 Aliased value.JSON[RowsAlias]
 AnonymousAlias value.JSON[AnonymousAlias]
 Generic value.JSON[GenericCopy]
 GenericAlias value.JSON[GenericAlias[struct {
  // Generic aliased item text.
  Text string
 }]]
 Generated value.JSON[GeneratedJSON]
 GeneratedAlias value.JSON[GeneratedAlias]
 Inline value.JSON[struct {
  // Inline JSON theme.
  Theme string
 }]
}
//foundry:projection dto=true
type Row struct {
 // Selected heading.
 Heading string
}
//foundry:dto
type Request struct {
 *Embedded
 // Original request text.
 Text string
}
//foundry:form
type Form struct {
 // Original form text.
 Text string
}
//foundry:multipart
type Multipart struct {
 // Original multipart text.
 Text string
}
`)
	write(t, dir, "generated_payload.go", `// Code generated by a test tool; DO NOT EDIT.
package sample
import "github.com/weiloon1234/Foundry-Go/value"
type GeneratedNestedAlias = HiddenPayload
type GeneratedPointerAlias = *HiddenPayload
type GeneratedOptionalAlias = value.Optional[HiddenPayload]
type GeneratedJSON struct {
 // Generated JSON leaf must not become original help.
 Child Payload
 Anonymous struct {
  // Generated anonymous leaf must not become original help.
  Text string
 }
 Entries map[string]struct {
  // Generated map leaf must not become original help.
  Text string
 }
 Rows []struct {
  // Generated slice leaf must not become original help.
  Text string
 }
}
type GeneratedAlias = []Payload
`)
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	output := generatedSnapshot(t, dir)
	for file, expected := range map[string]map[string]string{
		"article_foundry.gen.go": {
			"ArticleExtensionSet.Headline":                      "Translated headline.",
			"ArticleExtensionSlots.Headline":                    "Translated headline.",
			"Article_PayloadJSONProperties.Theme":               "Preferred display theme.",
			"Article_PointerTopJSONProperties.Text":             "Pointer alias text.",
			"Article_OptionalTopJSONProperties.Text":            "Optional alias text.",
			"Article_Payload_PtrJSONProperties.Text":            "Pointer alias text.",
			"Article_Payload_OptionalJSONProperties.Text":       "Optional alias text.",
			"Article_Payload_ChildJSONProperties.Text":          "Nested aliased display text.",
			"Article_GenericAlias_ElementJSONProperties.Text":   "Generic aliased item text.",
			"Article_PayloadJSONProperties.Title":               "Promoted message title.",
			"Article_InlineJSONProperties.Theme":                "Inline JSON theme.",
			"Article_Rows_ElementJSONProperties.Text":           "Slice item text.",
			"Article_Entries_EntryJSONProperties.Text":          "Map item text.",
			"Article_Fixed_ElementJSONProperties.Text":          "Array item text.",
			"Article_Copied_ElementJSONProperties.Text":         "Slice item text.",
			"Article_Aliased_ElementJSONProperties.Text":        "Slice item text.",
			"Article_AnonymousAlias_ElementJSONProperties.Text": "Aliased anonymous item text.",
			"Article_Generic_ElementJSONProperties.Text":        "Generic copied item text.",
		},
		"row_foundry.gen.go": {
			"RowFieldSet.Heading":           "Selected heading.",
			"RowSelection.Heading":          "Selected heading.",
			"RowScopedFieldSet.Heading":     "Selected heading.",
			"RowNullableFieldSet.Heading":   "Selected heading.",
			"RowValidationFieldSet.Heading": "Selected heading.",
		},
		"request_foundry.gen.go": {
			"RequestValidationFieldSet.Title": "Promoted message title.",
			"RequestValidationFieldSet.Text":  "Original request text.",
		},
		"form_foundry.gen.go":      {"FormValidationFieldSet.Text": "Original form text."},
		"multipart_foundry.gen.go": {"MultipartValidationFieldSet.Text": "Original multipart text."},
	} {
		docs := generatedHelp(t, output[file])
		for symbol, want := range expected {
			if !strings.Contains(docs[symbol], want) {
				t.Fatalf("%s: %s missing %q: %q", file, symbol, want, docs[symbol])
			}
		}
		if file == "row_foundry.gen.go" {
			builders := 0
			for symbol, help := range docs {
				if strings.HasSuffix(symbol, ".SelectHeading") {
					builders++
					if !strings.Contains(help, "Selected heading.") {
						t.Fatalf("%s lost projection builder help", symbol)
					}
				}
			}
			if builders == 0 {
				t.Fatal("no projection builder documentation inspected")
			}
		}
	}
	docs := generatedHelp(t, output["article_foundry.gen.go"])
	for _, symbol := range []string{"Article_Payload_GeneratedPointerJSONProperties.Text", "Article_Payload_GeneratedOptionalJSONProperties.Text", "Article_Payload_HiddenJSONProperties.Text", "Article_GeneratedJSONProperties.Child", "Article_Generated_ChildJSONProperties.Theme", "Article_Generated_AnonymousJSONProperties.Text", "Article_Generated_Entries_EntryJSONProperties.Text", "Article_Generated_Rows_ElementJSONProperties.Text", "Article_GeneratedAlias_ElementJSONProperties.Theme"} {
		if help, exists := docs[symbol]; !exists || help != "" {
			t.Fatalf("generated payload field %s missing or recycled help: %q", symbol, help)
		}
	}
	if _, err := Generate(t.Context(), Options{Dir: dir, Check: true}); err != nil {
		t.Fatal(err)
	}
}

func TestDocumentationReadsRejectStalePublicationAndCheckResults(t *testing.T) {
	dir := fixture(t, `package sample
import "foundry.test/help"
//foundry:config
type Settings struct {
 // Root group.
 Group help.Options
}
`)
	dependency, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	module := snapshotFile(t, dir, "go.mod")
	var goVersion string
	for line := range strings.SplitSeq(string(module.data), "\n") {
		if strings.HasPrefix(line, "go ") {
			goVersion = line
		}
	}
	write(t, dependency, "go.mod", "module foundry.test/help\n"+goVersion+"\n")
	write(t, dependency, "options.go", "package help\ntype Options struct {\n// Imported original help.\nValue string\nEmpty int\n}\n")
	write(t, dir, "go.mod", string(module.data)+"\nrequire foundry.test/help v0.0.0\nreplace foundry.test/help => "+strconv.Quote(dependency)+"\n")
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"unchanged", "check", "publish"} {
		t.Run(mode, func(t *testing.T) {
			source := snapshotFile(t, dir, "models.go")
			if mode == "publish" {
				write(t, dir, "models.go", strings.Replace(string(source.data), "Root group.", "Pending root group.", 1))
				t.Cleanup(func() { write(t, dir, "models.go", string(source.data)) })
			}
			graph, err := listGraph(t.Context(), dir, false)
			if err != nil {
				t.Fatal(err)
			}
			writes, err := graph.prepare(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			before := generatedSnapshot(t, dir)
			if mode != "publish" {
				if _, err := finishGeneration(t.Context(), Options{Dir: dir, Check: mode == "check"}, graph, writes); err != nil {
					t.Fatalf("unmodified documentation inputs failed %s: %v", mode, err)
				}
			}
			original := snapshotFile(t, dependency, "options.go")
			mod := snapshotFile(t, dependency, "go.mod")
			for _, mutation := range []string{"comment", "new comment", "add file", "remove file", "module", "symlink"} {
				t.Run(mutation, func(t *testing.T) {
					switch mutation {
					case "comment":
						write(t, dependency, "options.go", strings.Replace(string(original.data), "Imported original help.", "Changed imported help.", 1))
					case "new comment":
						write(t, dependency, "options.go", strings.Replace(string(original.data), "Empty int", "// Newly documented.\nEmpty int", 1))
					case "add file":
						write(t, dependency, "added.go", "package help\n")
						t.Cleanup(func() { _ = os.Remove(filepath.Join(dependency, "added.go")) })
					case "remove file":
						if err := os.Remove(filepath.Join(dependency, "options.go")); err != nil {
							t.Fatal(err)
						}
					case "module":
						write(t, dependency, "go.mod", string(mod.data)+"\n// module edited while generation was prepared\n")
					case "symlink":
						if err := os.Remove(filepath.Join(dependency, "options.go")); err != nil {
							t.Fatal(err)
						}
						if err := os.Symlink(filepath.Join(dir, "models.go"), filepath.Join(dependency, "options.go")); err != nil {
							t.Fatal(err)
						}
					}
					t.Cleanup(func() {
						if mutation == "symlink" {
							_ = os.Remove(filepath.Join(dependency, "options.go"))
						}
						write(t, dependency, "options.go", string(original.data))
						write(t, dependency, "go.mod", string(mod.data))
					})
					if _, err := finishGeneration(t.Context(), Options{Dir: dir, Check: mode == "check"}, graph, writes); err == nil {
						t.Fatalf("%s accepted changed documentation input", mode)
					}
					if !reflect.DeepEqual(before, generatedSnapshot(t, dir)) {
						t.Fatal("stale documentation observation changed output")
					}
					if _, err := os.Stat(filepath.Join(dependency, lockName)); !os.IsNotExist(err) {
						t.Fatal("documentation dependency acquired publication ownership")
					}
				})
			}
		})
	}
}

func TestDocumentationReadersUseIndependentGraphStateAndOverlays(t *testing.T) {
	dir := fixture(t, "package sample\n")
	for _, child := range []string{"shared", "left", "right"} {
		if err := os.Mkdir(filepath.Join(dir, child), 0755); err != nil {
			t.Fatal(err)
		}
	}
	write(t, filepath.Join(dir, "shared"), "options.go", "package shared\ntype Options struct {\n// Shared original help.\nValue int\n}\n")
	for _, child := range []string{"left", "right"} {
		write(t, filepath.Join(dir, child), "settings.go", "package "+child+"\nimport \"foundry.test/generator/shared\"\n//foundry:config\ntype Settings struct{ Group shared.Options }\n")
	}
	graph, err := listGraph(t.Context(), dir, true)
	if err != nil {
		t.Fatal(err)
	}
	overlay := map[string][]byte{
		filepath.Join(dir, "shared", "extra.go"): []byte("package shared\ntype Extra struct{\n// Overlay help.\nValue int\n}\n"),
		filepath.Join(dir, "left", "extra.go"):   []byte("package left\nimport \"foundry.test/generator/shared\"\n//foundry:config\ntype Extra struct{ Group shared.Extra }\n"),
	}
	var group sync.WaitGroup
	for range 3 {
		group.Go(func() {
			writes, err := graph.withOverlay(overlay).prepare(t.Context())
			if err != nil {
				t.Error(err)
				return
			}
			for _, write := range writes {
				if write.input.documentation != nil {
					t.Error("discovery retained imported source indexes")
				}
				for name, data := range write.plan.changes {
					if name == "settings_foundry.gen.go" && !bytes.Contains(data, []byte("Shared original help.")) {
						t.Error("concurrent reader lost shared source help")
					}
					if name == "extra_foundry.gen.go" && !bytes.Contains(data, []byte("Overlay help.")) {
						t.Error("imported selected package ignored its source overlay")
					}
				}
			}
		})
	}
	group.Wait()
	if len(graphSnapshot(t, dir)) != 0 {
		t.Fatal("documentation discovery published its proposed overlays")
	}
}

func TestJSONHelpTracksPromotedGeneratedOrigin(t *testing.T) {
	for name, child := range map[string]string{
		"anonymous": "struct { Text string }",
		"named":     "HandwrittenChild",
	} {
		t.Run(name, func(t *testing.T) {
			dir := fixture(t, `package sample
import "github.com/weiloon1234/Foundry-Go/value"
type HandwrittenChild struct {
 // Original child display text.
 Text string
}
type Payload struct {
 GeneratedEmbedded
 Visible HandwrittenChild
}
//foundry:model table=records primary=ID
type Record struct {
 ID int
 Data value.JSON[Payload]
}
`)
			write(t, dir, "generated_payload.go", "// Code generated by a test tool; DO NOT EDIT.\npackage sample\ntype GeneratedEmbedded struct { Child "+child+" }\n")
			if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
				t.Fatal(err)
			}
			docs := generatedHelp(t, generatedSnapshot(t, dir)["record_foundry.gen.go"])
			if help, ok := docs["Record_Data_ChildJSONProperties.Text"]; !ok || help != "" {
				t.Fatalf("promoted generated child is missing or copied original help: %q", help)
			}
			if !strings.Contains(docs["Record_Data_VisibleJSONProperties.Text"], "Original child display text.") {
				t.Fatal("generated sibling suppressed handwritten child help")
			}
		})
	}
}
