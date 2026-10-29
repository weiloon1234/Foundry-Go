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
			rule := validation.Rules(title, validation.Locales[translated](catalog, test.require))
			err := validation.Check(rule, t.Context(), test.input, validation.DefaultLimits())
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
	issues := rejection(t, validation.Check(validation.Rules(title, validation.Locales[translated](catalog, i18n.OptionalLocales)), t.Context(), translated{"\x00": "x"}, validation.DefaultLimits())).Issues()
	if len(issues) != 1 || issues[0].Path != "/title" || issues[0].Code != "foundry.supported_locale" {
		t.Fatalf("unrepresentable locale key: %+v", issues)
	}
	rule := validation.Rules(title, validation.Locales[translated](catalog, i18n.DefaultLocale))
	info, err := validation.DescribeRule(rule)
	if err != nil || !info.ServerOnly {
		t.Fatal("catalog-dependent locale rules must be server-only", err)
	}
	// Built-in IDs carry no application definition, so two fields' locale
	// rules compose in one tree.
	summary := validation.DefineField("summary", func(input translated) translated { return input })
	if err := validation.ValidateRule(validation.All(rule, validation.Rules(summary, validation.Locales[translated](catalog, i18n.AllLocales)))); err != nil {
		t.Fatal("locale rules of two fields did not compose", err)
	}
	if validation.ValidateRule(validation.Locales[translated](nil, i18n.OptionalLocales)) == nil || validation.ValidateRule(validation.Locales[translated](catalog, i18n.LocaleRequirement(9))) == nil {
		t.Fatal("invalid locale rule declarations were accepted")
	}
}

func TestMaxBytesCountsEncodedBytes(t *testing.T) {
	t.Parallel()
	rule := validation.MaxBytes[string](4)
	if err := validation.Check(rule, t.Context(), "abcd", validation.DefaultLimits()); err != nil {
		t.Fatal(err)
	}
	if issues := rejection(t, validation.Check(rule, t.Context(), "ééé", validation.DefaultLimits())).Issues(); len(issues) != 1 || issues[0].Code != "foundry.max_bytes" {
		t.Fatalf("multibyte text beyond the byte bound: %+v", issues)
	}
	if validation.ValidateRule(validation.MaxBytes[string](-1)) == nil {
		t.Fatal("negative byte bound was accepted")
	}
}
