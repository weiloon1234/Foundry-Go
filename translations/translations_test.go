package translations

import (
	"testing"

	"github.com/weiloon1234/Foundry-Go/i18n"
	"github.com/weiloon1234/Foundry-Go/internal/extensiontest"
)

func testLocales(t testing.TB) i18n.LocaleSet {
	t.Helper()
	set, err := i18n.NewLocaleSet("en", "en", "ms", "zh")
	if err != nil {
		t.Fatal(err)
	}
	return set
}
func TestFallbackIsStableAndEmptyTextIsPresent(t *testing.T) {
	locales := testLocales(t)
	for _, test := range []struct {
		values          map[i18n.LocaleID]string
		requested, want i18n.LocaleID
		text            string
	}{{map[i18n.LocaleID]string{"en": "English", "ms": "Melayu"}, "ms", "ms", "Melayu"}, {map[i18n.LocaleID]string{"en": "English", "ms": "Melayu"}, "zh", "en", "English"}, {map[i18n.LocaleID]string{"zh": "中文", "ms": "Melayu"}, "en", "ms", "Melayu"}, {map[i18n.LocaleID]string{"zh": "", "en": "English"}, "zh", "zh", ""}} {
		values := Values{locales: locales, values: test.values}
		result, err := values.Resolve(test.requested)
		got, ok := result.Get()
		if err != nil || !ok || got.Locale != test.want || got.Text != test.text {
			t.Fatal("fallback", got, err)
		}
		exported := values.Entries()
		exported[test.want] = "changed"
		again, _ := values.Exact(test.want)
		text, _ := again.Get()
		if text != test.text {
			t.Fatal("mutable values export")
		}
	}
	empty := Values{locales: locales, values: map[i18n.LocaleID]string{}}
	if result, err := empty.Resolve("en"); err != nil || result.IsSet() {
		t.Fatal("missing translation", err)
	}
	if _, err := empty.Resolve("fr"); err == nil {
		t.Fatal("unsupported locale used fallback")
	}
	for _, f := range []Field[extensiontest.Member, int64]{Define(extensiontest.Members, "bad name", Options{}), Define(extensiontest.Members, "title", Options{MaxBytes: -1}), Define(extensiontest.Members, "title", Options{MaxBytes: MaxValueBytes + 1})} {
		if f.Validate() == nil {
			t.Fatal("invalid translated field")
		}
	}
}
