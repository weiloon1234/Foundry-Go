package generate

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const extensionSource = `package sample
import (
 "encoding/json"
 "github.com/weiloon1234/Foundry-Go/attachments"
 "github.com/weiloon1234/Foundry-Go/i18n"
 "github.com/weiloon1234/Foundry-Go/metadata"
 "github.com/weiloon1234/Foundry-Go/model"
 "github.com/weiloon1234/Foundry-Go/storage"
 "github.com/weiloon1234/Foundry-Go/translations"
 "github.com/weiloon1234/Foundry-Go/value"
)
var Files = storage.DefineDisk("files")
//foundry:dto
type SEO struct { Canonical value.Optional[string] ` + "`json:\"canonical,omitzero\"`" + ` }
type Rank int
//foundry:model table=articles
type Article struct {
 ID model.ID[Article]
 Slug string
 Title translations.Text
 Body translations.Text ` + "`foundry:\"name=content\"`" + `
 Logo attachments.One[Article]
 Galleries attachments.Many[Article]
 SEO metadata.Value[SEO]
 Rank metadata.Value[Rank]
 Extra metadata.Value[json.RawMessage]
}
func (Article) DefineExtensions() ArticleExtensionSet {
 return ArticleExtensionSet{
  Title: translations.Options{MaxBytes: 512, Require: i18n.DefaultLocale},
  Logo: attachments.Policy{Disk: Files, AnyMedia: true},
  Galleries: attachments.Policy{Disk: Files, AnyMedia: true, MaxFiles: 8},
 }
}
//foundry:model table=notes
type Note struct {
 ID model.ID[Note]
 Label translations.Text
}
//foundry:model table=plain
type Plain struct { ID model.ID[Plain] }
func typedSlotUse() {
 var slots ArticleExtensionSlots = ArticleExtensions()
 var _ translations.TextSlot[Article, model.ID[Article]] = slots.Title
 var _ attachments.ManySlot[Article, model.ID[Article]] = slots.Galleries
 var _ metadata.ValueSlot[Article, model.ID[Article], SEO] = slots.SEO
 _ = FoundryExtensions()
 _ = NoteExtensionDeclaration()
 _ = QueryArticles().With(slots.Title, slots.Logo, slots.Galleries, slots.SEO)
 _ = QueryArticles().With(slots.All()...)
}
`

func TestFreshExtensionSlotGeneration(t *testing.T) {
	dir := fixture(t, extensionSource)
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	first := generatedSnapshot(t, dir)
	article := first["article_foundry.gen.go"]
	for _, want := range []string{
		"type ArticleExtensionSet struct",
		"type ArticleExtensionSlots struct",
		"func ArticleExtensions() ArticleExtensionSlots",
		"func ArticleExtensionOwner()",
		`DefineOwnerWith("articles"`,
		"func (s ArticleExtensionSlots) From(runtime slots.Runtime) ArticleExtensionSlots",
		"func (s ArticleExtensionSlots) All() []foundryquery.Relation[Article]",
		"func ArticleExtensionDeclaration() slots.Declaration",
		`translations.DefineText(owner, "content", policy.Body`,
		`attachments.DefineMany(owner, "galleries", policy.Galleries`,
		`metadata.DefineValue(owner, "seo", policy.SEO, SEOJSON()`,
		`foundrycontract.IntegerJSON[Rank]()`,
		`foundrycontract.DynamicJSON()`,
		"policy := (Article{}).DefineExtensions()",
		"NewCleanup(runtime, ArticleExtensionOwner(), parts, Article.FoundryReference)",
	} {
		if !strings.Contains(article, want) {
			t.Fatalf("generated article extensions lack %q:\n%s", want, article)
		}
	}
	if strings.Contains(article, "SetTitle(") || strings.Contains(article, `Name: "title"`) {
		t.Fatal("extension slots leaked into persisted model fields")
	}
	note := first["note_foundry.gen.go"]
	if !strings.Contains(note, "var policy NoteExtensionSet") || strings.Contains(note, framework+"/attachments") || strings.Contains(note, framework+"/metadata") {
		t.Fatalf("a text-only model must use default policy and import only translations:\n%s", note)
	}
	if strings.Contains(first["plain_foundry.gen.go"], "ExtensionSlots") {
		t.Fatal("a model without slots generated extension declarations")
	}
	listing := first["foundry_extensions_foundry.gen.go"]
	if !strings.Contains(listing, "return []slots.Declaration{ArticleExtensionDeclaration(), NoteExtensionDeclaration()}") {
		t.Fatalf("package listing is incomplete:\n%s", listing)
	}
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, generatedSnapshot(t, dir)) {
		t.Fatal("extension generation changed unchanged output")
	}
	if _, err := Generate(t.Context(), Options{Dir: dir, Check: true}); err != nil {
		t.Fatal(err)
	}
}

// A model whose only metadata slot uses a same-package DTO descriptor must not
// import the contract package it never names.
func TestExtensionSlotLocalDescriptorImports(t *testing.T) {
	source := strings.NewReplacer(" Rank metadata.Value[Rank]\n", "", " Extra metadata.Value[json.RawMessage]\n", "", ` "encoding/json"`+"\n", "").Replace(extensionSource)
	dir := fixture(t, source)
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(generatedSnapshot(t, dir)["article_foundry.gen.go"], framework+"/contract") {
		t.Fatal("a local DTO descriptor imported the contract package")
	}
}

func TestExtensionSlotFieldNotices(t *testing.T) {
	dir := fixture(t, extensionSource)
	if _, err := Generate(t.Context(), Options{Dir: dir, FieldDocumentation: true}); err != nil {
		t.Fatal(err)
	}
	source, err := os.ReadFile(filepath.Join(dir, "models.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`Its stored name is pinned by foundry:"name=content", so renaming this field keeps its data.`,
		`Renaming this field changes its stored name unless foundry:"name=title" pins it.`,
		`Extension slot, not a column: translated text stored in foundry_model_translations as field "content"`,
		"policy: [Article.DefineExtensions] entry Galleries",
		"Descriptor: NoteExtensions().Label; bind it once with From(runtime), then pass it to With or Load",
		"slot defaults apply; declare DefineExtensions to configure it",
	} {
		if !strings.Contains(string(source), want) {
			t.Fatalf("model source lacks slot notice %q:\n%s", want, source)
		}
	}
	if _, err := Generate(t.Context(), Options{Dir: dir, Check: true, FieldDocumentation: true}); err != nil {
		t.Fatal(err)
	}
}

func TestInvalidExtensionSlotDeclarationsDoNotPublish(t *testing.T) {
	for name, test := range map[string]struct{ from, to, want string }{
		"foreign attachment owner": {"Logo attachments.One[Article]", "Logo attachments.One[Note]", "type argument must be the enclosing model"},
		"column tag on slot":       {"Title translations.Text\n", "Title translations.Text `foundry:\"column=title\"`\n", "cannot declare persistence column/default tags"},
		"name tag on column":       {"Slug string", "Slug string `foundry:\"name=slug\"`", "applies only to extension slot fields"},
		"duplicate stored name":    {"Title translations.Text\n", "Title translations.Text `foundry:\"name=content\"`\n", "duplicate extension slot stored name content"},
		"invalid stored name":      {"Title translations.Text\n", "Title translations.Text `foundry:\"name=Title\"`\n", "stored name must be lowercase"},
		"reserved slot name":       {"Extra metadata.Value[json.RawMessage]", "From metadata.Value[json.RawMessage]", "conflicts with a generated descriptor method"},
		"missing attachment policy": {
			"func (Article) DefineExtensions() ArticleExtensionSet {", "func (Article) Unrelated() ArticleExtensionSet {", "attachment slot Logo requires a policy",
		},
		"pointer policy receiver": {"func (Article) DefineExtensions()", "func (*Article) DefineExtensions()", "requires a value receiver and signature"},
		"wrong policy result":     {"func (Article) DefineExtensions() ArticleExtensionSet {\n return ArticleExtensionSet{", "func (Article) DefineExtensions() NoteExtensionSet {\n return NoteExtensionSet{", "requires a value receiver and signature"},
		"policy without slots":    {"type Plain struct { ID model.ID[Plain] }", "type Plain struct { ID model.ID[Plain] }\nfunc (Plain) DefineExtensions() int { return 0 }", "requires extension slot fields"},
		"owner without slots":     {"//foundry:model table=plain", "//foundry:model table=plain extension_owner=plain", "extension_owner requires extension slot fields"},
		"invalid owner":           {"//foundry:model table=notes", "//foundry:model table=notes extension_owner=Notes", "must be a lowercase table-style name"},
		"uninferable contract":    {"Label translations.Text\n}", "Label translations.Text\n Tags metadata.Value[[]string]\n}", "metadata slot Tags needs an explicit JSON contract"},
		"handwritten generated":   {"func typedSlotUse() {", "func NoteExtensions() {}\nfunc typedSlotUse() {", "NoteExtensions"},
		"wrong policy entry kind": {"Title: translations.Options{MaxBytes: 512, Require: i18n.DefaultLocale},", "Title: attachments.Policy{Disk: Files, AnyMedia: true},", "check generated package overlay"},
	} {
		t.Run(name, func(t *testing.T) {
			source := strings.Replace(extensionSource, test.from, test.to, 1)
			if source == extensionSource {
				t.Fatal("test replacement did not apply")
			}
			dir := fixture(t, source)
			_, err := Generate(t.Context(), Options{Dir: dir})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("expected %q, got %v", test.want, err)
			}
			if len(generatedSnapshot(t, dir)) != 0 {
				t.Fatal("invalid extension declaration published files")
			}
		})
	}
}

const inferenceSource = `package sample
import (
 "encoding/json"
 "foundry.test/generator/shared"
 "github.com/weiloon1234/Foundry-Go/contract"
 "github.com/weiloon1234/Foundry-Go/metadata"
 "github.com/weiloon1234/Foundry-Go/translations"
)
type Code string
type Flag bool
type Ratio float64
//foundry:enum
type Tier string
const TierFree Tier = "free"
type Stamp string
func (Stamp) JSONContract() contract.JSON[Stamp] { return contract.StringJSON[Stamp]() }
func (s Stamp) MarshalJSON() ([]byte, error) { return json.Marshal(string(s)) }
func (s *Stamp) UnmarshalJSON(data []byte) error { var text string; err := json.Unmarshal(data, &text); *s = Stamp(text); return err }
//foundry:model table=countries primary=Code extension_owner=nations
type Country struct {
 Code Code
 Label translations.Text
 Name metadata.Value[string]
 Flag metadata.Value[Flag]
 Ratio metadata.Value[Ratio]
 Stamp metadata.Value[Stamp]
 Info metadata.Value[shared.Info]
 Tier metadata.Value[Tier]
}
func (Country) DefineExtensions() CountryExtensionSet { return CountryExtensionSet{} }
func typedCountryUse() {
 var _ translations.TextSlot[Country, Code] = CountryExtensions().Label
 _ = CountryExtensionOwner().Scope()
}
`

// Natural keys, owner pins, every inferred metadata contract and imported DTO
// descriptors compile; enums require an explicit contract.
func TestExtensionSlotInferenceAndIdentity(t *testing.T) {
	dir := fixture(t, inferenceSource)
	if err := os.Mkdir(filepath.Join(dir, "shared"), 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, dir, "shared/shared.go", "package shared\n//foundry:dto\ntype Info struct { Note string `json:\"note\"` }\n")
	if _, err := Generate(t.Context(), Options{Dir: dir, Recursive: true}); err != nil {
		t.Fatal(err)
	}
	output := generatedSnapshot(t, dir)["country_foundry.gen.go"]
	for _, want := range []string{
		`DefineOwnerWith("nations", foundryquery.IdentityOf(QueryCountries().Query, CountryFields().Code), extensions.OwnerOptions{StorageModel: "nations"})`,
		"foundrycontract.StringJSON[string]()",
		"foundrycontract.BooleanJSON[Flag]()",
		"foundrycontract.NumberJSON[Ratio]()",
		"(*new(Stamp)).JSONContract()",
		"shared.InfoJSON()",
		"foundrycontract.JSON[Tier]{}",
		"extensions.Owner[Country, Code]",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("generated country extensions lack %q:\n%s", want, output)
		}
	}
	// Without DefineExtensions an enum value has no contract to fall back on.
	source := strings.Replace(inferenceSource, "func (Country) DefineExtensions() CountryExtensionSet { return CountryExtensionSet{} }\n", "", 1)
	if source == inferenceSource {
		t.Fatal("test replacement did not apply")
	}
	write(t, dir, "models.go", source)
	if _, err := Generate(t.Context(), Options{Dir: dir, Recursive: true}); err == nil || !strings.Contains(err.Error(), "metadata slot Tier needs an explicit JSON contract") {
		t.Fatalf("enum metadata inferred a scalar contract: %v", err)
	}
}

func TestExtensionSlotOwnerAndContractDiagnostics(t *testing.T) {
	for name, test := range map[string]struct{ from, to, want string }{
		"duplicate owners":   {"//foundry:model table=notes", "//foundry:model table=notes extension_owner=articles", "duplicate extension owner articles"},
		"malformed contract": {"type Rank int", "type Rank int\nfunc (*Rank) JSONContract() int { return 0 }", "metadata slot Rank: JSONContract must be a value method"},
	} {
		t.Run(name, func(t *testing.T) {
			source := strings.Replace(extensionSource, test.from, test.to, 1)
			if source == extensionSource {
				t.Fatal("test replacement did not apply")
			}
			dir := fixture(t, source)
			if _, err := Generate(t.Context(), Options{Dir: dir}); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("expected %q, got %v", test.want, err)
			}
		})
	}
}
