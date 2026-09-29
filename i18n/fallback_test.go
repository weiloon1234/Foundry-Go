package i18n

import (
	"slices"
	"testing"
)

func TestLocaleSetMatchUsesSupportedParentsOnly(t *testing.T) {
	set, err := NewLocaleSet("ms", "ms", "en", "zh-Hant", "pt-BR")
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		id, want LocaleID
		ok       bool
	}{{"en", "en", true}, {"en-GB", "en", true}, {"en-u-ca-gregory", "en", true}, {"zh-Hant-TW", "zh-Hant", true}, {"zh-Hans", "", false}, {"pt", "", false}, {"pt-PT", "", false}, {"fr-CA", "", false}} {
		got, ok := set.Match(test.id)
		if got != test.want || ok != test.ok {
			t.Fatal(test, got, ok)
		}
	}
	if got, ok := set.MatchTag("EN-gb"); !ok || got != "en" {
		t.Fatal("external tag normalization", got, ok)
	}
	if _, ok := set.MatchTag("bad!tag"); ok {
		t.Fatal("malformed tag matched")
	}
}

// A script subtag after the language ends the parent chain, as in CLDR:
// traditional Chinese never falls back to zh, nor Latin Serbian to sr.
func TestLocaleParentsNeverSwitchWritingSystem(t *testing.T) {
	set, err := NewLocaleSet("en", "en", "zh", "sr", "zh-Hant")
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		id, want LocaleID
		ok       bool
	}{
		{"zh-Hant-TW", "zh-Hant", true}, {"zh-Hans", "", false}, {"zh-Hans-CN", "", false}, {"zh-TW", "zh", true},
		{"sr-Latn", "", false}, {"sr-Latn-RS", "", false}, {"sr-RS", "sr", true}, {"en-u-nu-thai", "en", true},
	} {
		if got, ok := set.Match(test.id); got != test.want || ok != test.ok {
			t.Fatal(test, got, ok)
		}
	}
	chain, err := set.Fallbacks("zh-Hant")
	if err != nil || chain[1] != "en" {
		t.Fatal("script locale fell back to its language before the default", chain, err)
	}
}

func TestModelContentFallbacksTryRegionalParentBeforeDefault(t *testing.T) {
	set, err := NewLocaleSet("ms", "ms", "en", "en-GB", "zh", "zh-Hant", "zh-Hant-TW")
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		requested LocaleID
		want      []LocaleID
	}{
		{"en-GB", []LocaleID{"en-GB", "en", "ms", "zh", "zh-Hant", "zh-Hant-TW"}},
		{"zh-Hant-TW", []LocaleID{"zh-Hant-TW", "zh-Hant", "ms", "en", "en-GB", "zh"}},
		{"zh-Hant", []LocaleID{"zh-Hant", "ms", "en", "en-GB", "zh", "zh-Hant-TW"}},
		{"ms", []LocaleID{"ms", "en", "en-GB", "zh", "zh-Hant", "zh-Hant-TW"}},
		{"en", []LocaleID{"en", "ms", "en-GB", "zh", "zh-Hant", "zh-Hant-TW"}},
	} {
		got, err := set.Fallbacks(test.requested)
		if err != nil || !slices.Equal(got, test.want) {
			t.Fatal(test.requested, got, err)
		}
	}
	if _, err := set.Fallbacks("en-AU"); err == nil {
		t.Fatal("unsupported requested locale used a parent")
	}
}

func TestCatalogLookupTriesRegionalParentThenConfiguredFallback(t *testing.T) {
	set, err := NewLocaleSet("ms", "ms", "en", "en-GB", "fr")
	if err != nil {
		t.Fatal(err)
	}
	definitions := []MessageDefinition{{Key: "colour"}, {Key: "local"}, {Key: "only.fr"}}
	c, err := NewCatalog(t.Context(), set, CatalogOptions{}, definitions, map[LocaleID]map[MessageKey]Template{
		"en":    {"colour": {Text: "Color"}},
		"en-GB": {"local": {Text: "Colour"}},
		"ms":    {"local": {Text: "Warna"}},
		"fr":    {"only.fr": {Text: "Couleur"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		locale      LocaleID
		key         MessageKey
		text        string
		selected    LocaleID
		fallback    bool
		wantMissing bool
	}{
		{"en-GB", "colour", "Color", "en", true, false},
		{"en-GB", "local", "Colour", "en-GB", false, false},
		{"en", "local", "Warna", "ms", true, false},
		{"en-GB", "only.fr", "only.fr", "", false, true},
	} {
		result, err := c.FormatDynamic(t.Context(), test.locale, test.key, nil)
		if err != nil || result.Text != test.text || result.Locale != test.selected || result.Fallback != test.fallback || result.Missing != test.wantMissing {
			t.Fatal(test, result, err)
		}
	}
}
