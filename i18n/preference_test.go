package i18n

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/fault"
)

type recipient struct {
	locale   LocaleID
	stored   bool
	header   string
	failWith error
}

func storedLocale(_ context.Context, r recipient) (LocaleID, bool, error) {
	return r.locale, r.stored, r.failWith
}

func TestLocaleResolverAppliesOrderedTypedPreferences(t *testing.T) {
	set, err := NewLocaleSet("en", "en", "ms", "zh-Hant")
	if err != nil {
		t.Fatal(err)
	}
	resolver, err := NewLocaleResolver(set,
		ContextLocale[recipient](),
		Preferred("user", storedLocale),
		AcceptLanguage(func(r recipient) string { return r.header }),
	)
	if err != nil {
		t.Fatal(err)
	}
	selected, err := WithLocale(t.Context(), set, "zh-Hant")
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		ctx     context.Context
		subject recipient
		want    Resolution
	}{
		{selected, recipient{locale: "ms", stored: true}, Resolution{Locale: "zh-Hant", Source: ContextSource}},
		{t.Context(), recipient{locale: "ms", stored: true, header: "zh-Hant"}, Resolution{Locale: "ms", Source: "user"}},
		{t.Context(), recipient{locale: "zh-Hant-HK", stored: true}, Resolution{Locale: "zh-Hant", Source: "user"}},
		{t.Context(), recipient{locale: "fr", stored: true, header: "fr-CA, ms;q=0.5"}, Resolution{Locale: "ms", Source: AcceptLanguageSource}},
		{t.Context(), recipient{header: "*"}, Resolution{Locale: "en", Source: AcceptLanguageSource}},
		{t.Context(), recipient{header: "de"}, Resolution{Locale: "en", Source: DefaultSource}},
		{t.Context(), recipient{}, Resolution{Locale: "en", Source: DefaultSource}},
	} {
		got, err := resolver.Resolve(test.ctx, test.subject)
		if err != nil || got != test.want {
			t.Fatal(test.subject, got, err)
		}
	}
	ctx, resolution, err := resolver.WithResolvedLocale(t.Context(), recipient{locale: "ms", stored: true})
	if err != nil || resolution.Locale != "ms" {
		t.Fatal(resolution, err)
	}
	if locale, ok := RequestLocale(ctx); !ok || locale != "ms" {
		t.Fatal("resolved locale was not recorded", locale, ok)
	}
}

func TestLocaleResolverFailuresNeverSelectTheDefault(t *testing.T) {
	set, _ := NewLocaleSet("en", "en", "ms")
	lookupFailure := errors.New("profile store unavailable")
	resolver, err := NewLocaleResolver(set, Preferred("user", storedLocale))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := resolver.Resolve(t.Context(), recipient{failWith: lookupFailure}); !errors.Is(err, lookupFailure) {
		t.Fatal(err)
	}
	// A malformed stored value is no preference: resolution continues and
	// reports the ignored step instead of failing every request.
	for _, stored := range []LocaleID{"en-gb", "en_US", "bad!"} {
		got, err := resolver.Resolve(t.Context(), recipient{locale: stored, stored: true})
		if err != nil || got != (Resolution{Locale: "en", Source: DefaultSource, Ignored: "user"}) {
			t.Fatal("malformed stored locale was not skipped", stored, got, err)
		}
	}
	withHeader, err := NewLocaleResolver(set, Preferred("user", storedLocale), AcceptLanguage(func(r recipient) string { return r.header }))
	if err != nil {
		t.Fatal(err)
	}
	if got, err := withHeader.Resolve(t.Context(), recipient{locale: "ms_MY", stored: true, header: "ms"}); err != nil || got != (Resolution{Locale: "ms", Source: AcceptLanguageSource, Ignored: "user"}) {
		t.Fatal("malformed stored locale blocked later steps", got, err)
	}
	panicking, err := NewLocaleResolver(set, Preferred("user", func(context.Context, recipient) (LocaleID, bool, error) { panic("private profile") }))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := panicking.Resolve(t.Context(), recipient{}); !errors.Is(err, fault.Panicked) || strings.Contains(err.Error(), "private") {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := resolver.Resolve(ctx, recipient{}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	for _, steps := range [][]Preference[recipient]{
		{{}},
		{Preferred[recipient]("user", nil)},
		{Preferred("Bad Source", storedLocale)},
		{Preferred("default", storedLocale)},
		{AcceptLanguage[recipient](nil)},
		{Preferred("user", storedLocale), Preferred("user", storedLocale)},
	} {
		if _, err := NewLocaleResolver(set, steps...); err == nil {
			t.Fatal("invalid preference accepted")
		}
	}
	if _, err := NewLocaleResolver[recipient](nil); err == nil {
		t.Fatal("nil catalog accepted")
	}
	var zero *LocaleResolver[recipient]
	if _, err := zero.Resolve(t.Context(), recipient{}); err == nil {
		t.Fatal("nil resolver resolved")
	}
}

func BenchmarkLocaleResolver(b *testing.B) {
	c := benchmarkCatalog(b)
	resolver, err := NewLocaleResolver(c, ContextLocale[recipient](), Preferred("user", storedLocale), AcceptLanguage(func(r recipient) string { return r.header }))
	if err != nil {
		b.Fatal(err)
	}
	subject := recipient{header: "fr-CA,en-GB;q=0.8"}
	ctx := b.Context()
	b.ReportAllocs()
	for b.Loop() {
		if _, err := resolver.Resolve(ctx, subject); err != nil {
			b.Fatal(err)
		}
	}
}
