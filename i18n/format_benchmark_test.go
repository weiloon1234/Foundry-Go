package i18n

import (
	"testing"

	"github.com/weiloon1234/Foundry-Go/decimal"
)

func benchmarkCatalog(b *testing.B) *Catalog {
	b.Helper()
	set, err := NewLocaleSet("en", "en", "ms", "en-GB")
	if err != nil {
		b.Fatal(err)
	}
	d := MessageDefinition{Key: "validation.max_items", Parameters: []Parameter{{Name: "attribute", Kind: TextParameter}, {Name: "max", Kind: NumberParameter}}, Plural: "max", Kind: Cardinal}
	c, err := NewCatalog(b.Context(), set, CatalogOptions{}, []MessageDefinition{d}, map[LocaleID]map[MessageKey]Template{
		"en": {"validation.max_items": {Forms: map[PluralForm]string{One: "{{attribute}} may contain one item.", Other: "{{attribute}} may contain {{max}} items."}}},
		"ms": {"validation.max_items": {Text: "", Forms: map[PluralForm]string{Other: "{{attribute}} boleh mengandungi {{max}} item."}}},
	})
	if err != nil {
		b.Fatal(err)
	}
	return c
}

// BenchmarkPreparedMessageFormat measures the validation-message hot path:
// label substitution followed by catalog rendering of a prepared recipe.
func BenchmarkPreparedMessageFormat(b *testing.B) {
	c := benchmarkCatalog(b)
	d, _ := c.Definition("validation.max_items")
	prepared, err := PrepareMessage(d, map[string]Argument{"attribute": Text("This field"), "max": Number(decimal.FromInt64(3))}, Template{Forms: map[PluralForm]string{One: "{{attribute}} one", Other: "{{attribute}} {{max}}"}})
	if err != nil {
		b.Fatal(err)
	}
	ctx := b.Context()
	b.ReportAllocs()
	for b.Loop() {
		labelled, err := prepared.WithText("attribute", "Items")
		if err != nil {
			b.Fatal(err)
		}
		if _, err := labelled.Format(ctx, c, "ms"); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCatalogFormatDynamic(b *testing.B) {
	c := benchmarkCatalog(b)
	args := map[string]Argument{"attribute": Text("Items"), "max": Number(decimal.FromInt64(1))}
	ctx := b.Context()
	b.ReportAllocs()
	for b.Loop() {
		if _, err := c.FormatDynamic(ctx, "en-GB", "validation.max_items", args); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkWithLocale(b *testing.B) {
	c := benchmarkCatalog(b)
	ctx := b.Context()
	b.ReportAllocs()
	for b.Loop() {
		if _, err := WithLocale(ctx, c, "ms"); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCatalogResolve(b *testing.B) {
	c := benchmarkCatalog(b)
	b.ReportAllocs()
	for b.Loop() {
		if _, err := c.Resolve("", "fr-CA,ms-MY;q=0.8,en;q=0.5"); err != nil {
			b.Fatal(err)
		}
	}
}
