package articles_test

import (
	"bytes"
	"io"
	"testing"

	"foundry.test/consumer/articles"
	"github.com/weiloon1234/Foundry-Go/attachments"
	attachmentcommand "github.com/weiloon1234/Foundry-Go/attachments/command"
	"github.com/weiloon1234/Foundry-Go/i18n"
	"github.com/weiloon1234/Foundry-Go/metadata"
	"github.com/weiloon1234/Foundry-Go/translations"
	translationcommand "github.com/weiloon1234/Foundry-Go/translations/command"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Renaming a slot field without pinning its stored name leaves rows under the
// old name. The read-only undeclared command reports them by name only.
func TestUndeclaredStoredNamesAfterARename(t *testing.T) {
	f := startApplication(t)
	runtime, err := f.services.ModelExtensions()
	if err != nil {
		t.Fatal(err)
	}
	article := f.createArticle(t, "renamed")
	// Declared slots hold data too; only the old name may be reported.
	x := articles.ArticleExtensions().From(runtime)
	if err := x.Title.Save(t.Context(), article, map[i18n.LocaleID]string{"en": "Current title"}); err != nil {
		t.Fatal(err)
	}
	if err := x.SEO.Save(t.Context(), article, articles.SEO{Canonical: value.Set("/renamed")}); err != nil {
		t.Fatal(err)
	}
	if _, err := x.Logo.Replace(t.Context(), article, attachments.Upload{Source: bytes.NewReader(pngImage(t, 40)), OriginalName: "logo.png"}); err != nil {
		t.Fatal(err)
	}
	// The field was once declared as Headline; its rows keep that name.
	headline := translations.Define(articles.ArticleExtensionOwner(), "headline", translations.Options{})
	previous, err := translations.New(f.store, mustLocales(t, f), headline.Registration())
	if err != nil {
		t.Fatal(err)
	}
	if err := headline.Set(t.Context(), previous, article.FoundryReference(), "en", "Old headline"); err != nil {
		t.Fatal(err)
	}
	report, err := translations.InspectUndeclared(t.Context(), runtime.Translations, "articles")
	if err != nil || len(report.Names) != 1 || report.Names[0] != "headline" || report.Truncated {
		t.Fatalf("undeclared translation names: %+v %v", report, err)
	}
	if report, err := metadata.InspectUndeclared(t.Context(), runtime.Metadata, "articles"); err != nil || len(report.Names) != 0 {
		t.Fatalf("declared metadata reported: %+v %v", report, err)
	}
	command, err := translationcommand.Parse([]string{"translations", "undeclared", "--owner", "articles"}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := command.Run(t.Context(), runtime.Translations, &output); err != nil || output.String() != "headline\n" {
		t.Fatalf("command output %q: %v", output.String(), err)
	}
	files, err := attachmentcommand.Parse([]string{"attachments", "undeclared", "--owner", "articles", "--format", "json"}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	output.Reset()
	if err := files.Run(t.Context(), runtime.Attachments, &output); err != nil || output.String() != "{\"names\":[],\"truncated\":false}\n" {
		t.Fatalf("attachment command output %q: %v", output.String(), err)
	}
	// The report never alters data: the old text is still stored.
	if text, err := headline.Get(t.Context(), previous, article.FoundryReference(), "en"); err != nil || !text.IsSet() {
		t.Fatal("inspection modified stored text", err)
	}
}

func mustLocales(t *testing.T, f fixture) *i18n.Catalog {
	t.Helper()
	catalog, err := f.services.Locales()
	if err != nil {
		t.Fatal(err)
	}
	return catalog
}
