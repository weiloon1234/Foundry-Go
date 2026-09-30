package articles_test

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"foundry.test/consumer/articles"
	"github.com/weiloon1234/Foundry-Go/application"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/extensions"
	"github.com/weiloon1234/Foundry-Go/extensions/slots"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/i18n"
	"github.com/weiloon1234/Foundry-Go/inspection"
)

// A generated owner keeps the scope of an equivalent hand-declared owner, so
// data written through explicit declarations survives adopting slots.
func TestGeneratedOwnerMatchesHandDeclaredScope(t *testing.T) {
	generated := articles.ArticleExtensionOwner()
	manual := extensions.DefineOwner("articles", query.IdentityOf(articles.QueryArticles().Query, articles.ArticleFields().ID))
	if err := generated.Validate(); err != nil {
		t.Fatal(err)
	}
	if generated.Scope() != manual.Scope() || generated.Name() != "articles" || generated.StorageModel() != "articles" {
		t.Fatal("generated owner changed the persisted scope")
	}
	x := articles.ArticleExtensions()
	if x.Title.Name() != "title" || x.Galleries.Name() != "galleries" || x.SEO.Name() != "seo" || x.Title.Options().Require != i18n.DefaultLocale {
		t.Fatal("slot names or policy were not generated from the model")
	}
	for _, err := range []error{x.Title.Validate(), x.Summary.Validate(), x.Logo.Validate(), x.Galleries.Validate(), x.SEO.Validate()} {
		if err != nil {
			t.Fatal(err)
		}
	}
	declarations := articles.FoundryExtensions()
	if len(declarations) != 1 || declarations[0].Owner().Name() != "articles" {
		t.Fatal("package listing lacks the article declaration")
	}
	parts := declarations[0].Parts()
	if len(parts.Translations) != 2 || len(parts.Attachments) != 2 || len(parts.Metadata) != 1 {
		t.Fatal("declaration parts differ from the model's slots")
	}
	// A declared slot kind without its manager fails cleanup construction
	// instead of silently skipping that kind's data.
	if _, err := slots.NewCleanup(slots.Runtime{}, generated, parts, articles.Article.FoundryReference); !errors.Is(err, fault.Missing) || !strings.Contains(err.Error(), "translations") {
		t.Fatal("cleanup accepted a runtime without the declared translations manager", err)
	}
}

// Declarations needing a disabled feature fail at Build, naming their owner,
// before any database or storage resource is acquired.
func TestModelDeclarationsRequireTheirFeatures(t *testing.T) {
	for name, test := range map[string]struct {
		configure func(*application.Settings)
		want      string
	}{
		"extensions": {func(*application.Settings) {}, "requires features.extensions"},
		"locales": {func(s *application.Settings) {
			s.Features.Extensions.Enabled = true
		}, "require features.locales"},
		"attachments": {func(s *application.Settings) {
			s.Features.Extensions.Enabled = true
			s.Features.Locales.Enabled = true
			s.Features.Locales.Locales = []i18n.LocaleID{"en"}
		}, "require features.attachments"},
	} {
		t.Run(name, func(t *testing.T) {
			s := application.DefaultSettings()
			s.HTTP.Enabled = false
			test.configure(&s)
			_, err := application.New(s).Models(articles.FoundryExtensions()...).Build(t.Context())
			if err == nil || !strings.Contains(err.Error(), test.want) || !strings.Contains(err.Error(), "articles") {
				t.Fatalf("expected %q naming the owner, got %v", test.want, err)
			}
		})
	}
}

// Declaration inspection lists owners and slots without services or I/O, so
// agents and operators see a model's storage from its declaration alone.
func TestInspectionDescribesModelSlots(t *testing.T) {
	report, err := inspection.Collect(t.Context(), inspection.Sources{Models: articles.FoundryExtensions()})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Extensions) != 1 || report.Extensions[0].Owner.Name != "articles" || report.Extensions[0].Owner.StorageModel != "articles" {
		t.Fatalf("owner description: %+v", report.Extensions)
	}
	var fields []string
	for _, slot := range report.Extensions[0].Slots {
		fields = append(fields, slot.Field+":"+slot.Kind+":"+slot.Name)
	}
	if strings.Join(fields, ",") != "Title:text:title,Summary:text:summary,Logo:one:logo,Galleries:many:galleries,SEO:value:seo" {
		t.Fatal("slot descriptions", fields)
	}
	galleries := report.Extensions[0].Slots[3]
	if galleries.Disk != "article-files" || galleries.MaxFiles != 4 || len(galleries.Variants) != 1 || galleries.Variants[0] != "thumbnail" || report.Extensions[0].Slots[0].Require != "default" {
		t.Fatalf("slot policy summary: %+v", report.Extensions[0].Slots)
	}
	var first, second bytes.Buffer
	for _, output := range []*bytes.Buffer{&first, &second} {
		if err := inspection.Write(t.Context(), output, report, inspection.Arguments{Section: inspection.Extensions, Format: inspection.JSON}); err != nil {
			t.Fatal(err)
		}
	}
	if first.String() != second.String() || !strings.Contains(first.String(), `"storage":"foundry_model_translations"`) {
		t.Fatal("extension inspection output is not a deterministic snapshot", first.String())
	}
}
