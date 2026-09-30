package validation_test

import (
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/i18n"
	"github.com/weiloon1234/Foundry-Go/validation"
)

type translated map[i18n.LocaleID]string

func TestLocalesReportsUnsupportedAndRequiredEntries(t *testing.T) {
	t.Parallel()
	catalog, err := i18n.NewLocaleSet("en", "en", "ms")
	if err != nil {
		t.Fatal(err)
	}
	title := validation.DefineField("title", func(input translated) translated { return input })
	for name, test := range map[string]struct {
		require i18n.LocaleRequirement
		input   translated
		want    string
	}{
		"optional accepts any supported subset": {i18n.OptionalLocales, translated{"ms": ""}, ""},
		"unsupported keys in order":             {i18n.OptionalLocales, translated{"fr": "x", "de": "y", "en": "z"}, "/title/de foundry.supported_locale,/title/fr foundry.supported_locale"},
		"default locale required":               {i18n.DefaultLocale, translated{"ms": "Helo"}, "/title/en foundry.required"},
		"empty required text":                   {i18n.DefaultLocale, translated{"en": ""}, "/title/en foundry.required"},
		"every locale required":                 {i18n.AllLocales, translated{"en": "Hello"}, "/title/ms foundry.required"},
		"complete":                              {i18n.AllLocales, translated{"en": "Hello", "ms": "Helo"}, ""},
	} {
		t.Run(name, func(t *testing.T) {
			rule := title.Rules(validation.Locales[translated](catalog, test.require))
			err := rule.Check(t.Context(), test.input, validation.DefaultLimits())
			if test.want == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			var found []string
			for _, issue := range rejection(t, err).Issues() {
				found = append(found, issue.Path+" "+string(issue.Code))
			}
			if strings.Join(found, ",") != test.want {
				t.Fatalf("issues %v, want %s", found, test.want)
			}
		})
	}
	// A key that cannot be a path segment is still a rejection at the map,
	// never an execution failure.
	issues := rejection(t, title.Rules(validation.Locales[translated](catalog, i18n.OptionalLocales)).Check(t.Context(), translated{"\x00": "x"}, validation.DefaultLimits())).Issues()
	if len(issues) != 1 || issues[0].Path != "/title" || issues[0].Code != "foundry.supported_locale" {
		t.Fatalf("unrepresentable locale key: %+v", issues)
	}
	rule := title.Rules(validation.Locales[translated](catalog, i18n.DefaultLocale))
	info, err := rule.Description()
	if err != nil || !info.ServerOnly {
		t.Fatal("catalog-dependent locale rules must be server-only", err)
	}
	// Built-in IDs carry no application definition, so two fields' locale
	// rules compose in one tree.
	summary := validation.DefineField("summary", func(input translated) translated { return input })
	if err := validation.All(rule, summary.Rules(validation.Locales[translated](catalog, i18n.AllLocales))).Validate(); err != nil {
		t.Fatal("locale rules of two fields did not compose", err)
	}
	if validation.Locales[translated](nil, i18n.OptionalLocales).Validate() == nil || validation.Locales[translated](catalog, i18n.LocaleRequirement(9)).Validate() == nil {
		t.Fatal("invalid locale rule declarations were accepted")
	}
}

func TestMaxBytesCountsEncodedBytes(t *testing.T) {
	t.Parallel()
	rule := validation.MaxBytes[string](4)
	if err := rule.Check(t.Context(), "abcd", validation.DefaultLimits()); err != nil {
		t.Fatal(err)
	}
	if issues := rejection(t, rule.Check(t.Context(), "ééé", validation.DefaultLimits())).Issues(); len(issues) != 1 || issues[0].Code != "foundry.max_bytes" {
		t.Fatalf("multibyte text beyond the byte bound: %+v", issues)
	}
	if validation.MaxBytes[string](-1).Validate() == nil {
		t.Fatal("negative byte bound was accepted")
	}
}
