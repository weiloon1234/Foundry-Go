package i18n

import (
	"context"
	"runtime"
	"testing"
)

type catalogFunc func(context.Context) (LocaleSet, error)

func (f catalogFunc) Snapshot(ctx context.Context) (LocaleSet, error) { return f(ctx) }
func TestLocaleNormalizationAndImmutableCatalog(t *testing.T) {
	id, err := ParseLocale("en-us")
	if err != nil || id != "en-US" {
		t.Fatal(id, err)
	}
	if LocaleID("en-us").Validate() == nil {
		t.Fatal("noncanonical ID accepted")
	}
	for _, text := range []string{"", "bad!tag", "en\x00US"} {
		if _, err := ParseLocale(text); err == nil {
			t.Fatal("invalid locale accepted")
		}
	}
	ids := []LocaleID{"zh", "en", "ms"}
	set, err := NewLocaleSet("en", ids...)
	if err != nil {
		t.Fatal(err)
	}
	ids[1] = "fr"
	snapshot := set.Locales()
	snapshot[0] = "fr"
	if !set.Contains("en") || set.Contains("fr") {
		t.Fatal("catalog retained mutable slice")
	}
	for _, ids := range [][]LocaleID{nil, {"en", "en"}, {"ms"}, {"en-us", "en"}} {
		if _, err := NewLocaleSet("en", ids...); err == nil {
			t.Fatal("invalid locale set accepted")
		}
	}
	if _, err := SnapshotLocales(t.Context(), set); err != nil {
		t.Fatal(err)
	}
	for _, c := range []LocaleCatalog{nil, catalogFunc(nil), catalogFunc(func(context.Context) (LocaleSet, error) { panic("private") }), catalogFunc(func(context.Context) (LocaleSet, error) { runtime.Goexit(); return LocaleSet{}, nil })} {
		if _, err := SnapshotLocales(t.Context(), c); err == nil {
			t.Fatal("invalid or failed catalog accepted")
		}
	}
}
