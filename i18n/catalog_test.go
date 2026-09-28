package i18n

import (
	"context"
	"errors"
	"io/fs"
	"runtime"
	"strings"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/weiloon1234/Foundry-Go/decimal"
)

func testCatalog(t *testing.T, definitions []MessageDefinition, messages map[LocaleID]map[MessageKey]Template) *Catalog {
	t.Helper()
	set, err := NewLocaleSet("en", "en", "ms", "ru", "ar", "pt-BR")
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewCatalog(t.Context(), set, CatalogOptions{}, definitions, messages)
	if err != nil {
		t.Fatal(err)
	}
	return c
}
func TestCatalogSinglePassFallbackAndOwnedMetadata(t *testing.T) {
	definitions := []MessageDefinition{{Key: "welcome", Parameters: []Parameter{{Name: "name", Kind: TextParameter}}}, {Key: "missing"}, {Key: "only.ar"}}
	messages := map[LocaleID]map[MessageKey]Template{"en": {"welcome": {Text: "Hello {{name}} {{name}}"}}, "ms": {"welcome": {Text: "Helo {{name}}"}}, "ar": {"only.ar": {Text: "Do not use arbitrary other locale"}}}
	c := testCatalog(t, definitions, messages)
	definitions[0].Parameters[0].Name = "changed"
	messages["ms"]["welcome"] = Template{Text: "changed"}
	copy := c.Definitions()
	copy[0].Key = "changed"
	for _, test := range []struct {
		locale   LocaleID
		want     string
		fallback bool
	}{{"en", "Hello {{name}} {{name}}", false}, {"ms", "Helo {{name}}", false}, {"ru", "Hello {{name}} {{name}}", true}} {
		result, err := c.FormatDynamic(t.Context(), test.locale, "welcome", map[string]Argument{"name": Text("{{name}}")})
		if err != nil || result.Text != test.want || result.Fallback != test.fallback || result.Missing {
			t.Fatal(result, err)
		}
	}
	for _, key := range []MessageKey{"missing", "only.ar"} {
		result, err := c.FormatDynamic(t.Context(), "ms", key, nil)
		if err != nil || !result.Missing || result.Text != string(key) || result.Locale != "" {
			t.Fatal(result, err)
		}
	}
	snapshot, err := c.Snapshot(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	content, _ := snapshot.Fallbacks("ms")
	if len(content) != 5 {
		t.Fatal("UI fallback changed model-content locales")
	}
	for _, args := range []map[string]Argument{nil, {"name": {}}, {"name": Boolean(true)}, {"name": Text("x"), "extra": Text("x")}, {"name": Text("bad\x00")}} {
		if _, err := c.FormatDynamic(t.Context(), "en", "welcome", args); err == nil {
			t.Fatal("invalid argument accepted")
		}
	}
	if _, err := c.FormatDynamic(t.Context(), "fr", "missing", nil); err == nil {
		t.Fatal("unsupported locale")
	}
	if _, err := c.FormatDynamic(t.Context(), "en", "unknown", nil); err == nil {
		t.Fatal("undeclared dynamic key")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := c.FormatDynamic(ctx, "en", "missing", nil); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestCatalogCLDRExactCardinalAndOrdinal(t *testing.T) {
	all := map[PluralForm]string{Zero: "zero", One: "one", Two: "two", Few: "few", Many: "many", Other: "other"}
	d := MessageDefinition{Key: "count", Parameters: []Parameter{{Name: "count", Kind: NumberParameter}}, Plural: "count", Kind: Cardinal}
	o := d
	o.Key = "ordinal"
	o.Kind = Ordinal
	messages := make(map[LocaleID]map[MessageKey]Template)
	for _, locale := range []LocaleID{"en", "ru", "ar"} {
		messages[locale] = map[MessageKey]Template{"count": {Forms: all}, "ordinal": {Forms: all}}
	}
	c := testCatalog(t, []MessageDefinition{d, o}, messages)
	for _, test := range []struct {
		locale       LocaleID
		key          MessageKey
		number, want string
	}{
		{"en", "count", "1.0", "one"}, {"en", "count", "2", "other"}, {"ru", "count", "1", "one"}, {"ru", "count", "2", "few"}, {"ru", "count", "5", "many"}, {"ru", "count", "1.2", "other"}, {"ru", "count", "900719925474099321", "one"},
		{"ar", "count", "0", "zero"}, {"ar", "count", "1", "one"}, {"ar", "count", "2", "two"}, {"ar", "count", "3", "few"}, {"ar", "count", "11", "many"}, {"ar", "count", "100", "other"},
		{"en", "ordinal", "1", "one"}, {"en", "ordinal", "2", "two"}, {"en", "ordinal", "3", "few"}, {"en", "ordinal", "11", "other"}, {"en", "ordinal", "21", "one"}, {"ms", "ordinal", "21", "one"},
		{"en", "count", "1000000000000000001", "other"}, {"en", "count", "0.000000001", "other"},
		{"en", "ordinal", "900719925474099321", "one"}, {"en", "ordinal", "900719925474099311", "other"},
		{"ru", "count", "-900719925474099321", "one"}, {"ru", "count", "900719925474099322", "few"},
		{"ru", "count", "900719925474099321.000000001", "other"},
		{"ru", "count", "0.00000000000000000001", "other"},
		{"ru", "count", "0.000000000000000000001", "other"},
		{"ru", "count", "-0.000000000000000000001", "other"},
		{"ar", "count", "1000000000000000003", "few"}, {"ar", "count", "1000000000000000011", "many"},
		{"ar", "count", "1000000000000000000", "other"},
	} {
		number, err := decimal.Parse(test.number)
		if err != nil {
			t.Fatal(err)
		}
		result, err := c.FormatDynamic(t.Context(), test.locale, test.key, map[string]Argument{"count": Number(number)})
		if err != nil || result.Text != test.want {
			t.Fatal(test, result, err)
		}
	}
	all[One] = "mutated"
	result, err := c.FormatDynamic(t.Context(), "en", "count", map[string]Argument{"count": Number(decimal.FromInt64(1))})
	if err != nil || result.Text != "one" {
		t.Fatal(result, err)
	}
}

func TestCatalogRequestResolutionAndConcurrentContexts(t *testing.T) {
	c := testCatalog(t, []MessageDefinition{{Key: "label"}}, map[LocaleID]map[MessageKey]Template{"en": {"label": {Text: "English"}}, "ms": {"label": {Text: "Melayu"}}})
	for _, test := range []struct {
		preference, header string
		want               LocaleID
	}{
		{"ms", "en", "ms"}, {"bad!", "ms;q=0.8,en;q=0.2", "ms"}, {"fr", "pt-BR-x-private;q=0.9,en;q=0.1", "pt-BR"}, {"", "ru;q=0,ms;q=0.2", "ms"}, {"", "ms;q=1.001,en;q=0.2", "en"}, {"", "ru;q=0.3,ms;q=0.3", "ru"}, {"", "fr,*;q=0.2", "en"}, {"", "en;q=-1", "en"}, {"", "ms;q=0.0001", "en"}, {"", "PT-br", "pt-BR"}, {"", "ms;q=NaN,en;q=0.1", "en"}, {"", strings.Repeat("x", 8193), "en"},
	} {
		got, err := c.Resolve(test.preference, test.header)
		if err != nil || got != test.want {
			t.Fatal(test, got, err)
		}
	}
	var wg sync.WaitGroup
	for i := range 100 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			locale := LocaleID("en")
			want := "English"
			if i%2 == 1 {
				locale = "ms"
				want = "Melayu"
			}
			ctx, err := WithLocale(t.Context(), c, locale)
			if err != nil {
				t.Error(err)
				return
			}
			selected, ok := RequestLocale(ctx)
			if !ok || selected != locale {
				t.Error("locale context crossed requests")
			}
			result, err := c.Label(ctx, selected, "label")
			if err != nil || result != want {
				t.Error(result, err)
			}
		}()
	}
	wg.Wait()
	if _, ok := RequestLocale(t.Context()); ok {
		t.Fatal("parent context mutated")
	}
	if _, err := WithLocale(t.Context(), c, "fr"); err == nil {
		t.Fatal("context admitted unsupported locale")
	}
}

func TestCatalogRejectsMalformedDefinitionsTemplatesAndBounds(t *testing.T) {
	set, _ := NewLocaleSet("en", "en")
	good := MessageDefinition{Key: "hello", Parameters: []Parameter{{Name: "name", Kind: TextParameter}}}
	for _, d := range []MessageDefinition{{}, {Key: "Upper"}, {Key: "hello", Parameters: []Parameter{{Name: "a.b", Kind: TextParameter}}}, {Key: "hello", Parameters: []Parameter{{Name: "name", Kind: "unknown"}}}, {Key: "hello", Parameters: []Parameter{{Name: "x", Kind: TextParameter}, {Name: "x", Kind: TextParameter}}}, {Key: "hello", Plural: "name", Kind: Cardinal}, {Key: "hello", Kind: Ordinal}} {
		if d.Validate() == nil {
			t.Fatal("malformed declaration accepted")
		}
	}
	for _, template := range []Template{{Text: "{{unknown}}"}, {Text: "{{name"}, {Text: "name}}"}, {Text: "{{{name}}"}, {Text: "bad\x00"}, {Text: strings.Repeat("x", MaxTextBytes+1)}, {Forms: map[PluralForm]string{Other: "x"}}} {
		if _, err := NewCatalog(t.Context(), set, CatalogOptions{}, []MessageDefinition{good}, map[LocaleID]map[MessageKey]Template{"en": {"hello": template}}); err == nil {
			t.Fatal("bad template accepted")
		}
	}
	if _, err := NewCatalog(t.Context(), set, CatalogOptions{}, []MessageDefinition{good, good}, nil); err == nil {
		t.Fatal("duplicate key")
	}
	if _, err := NewCatalog(t.Context(), set, CatalogOptions{Fallback: "ms"}, []MessageDefinition{good}, nil); err == nil {
		t.Fatal("unsupported fallback")
	}
	if _, err := NewCatalog(t.Context(), set, CatalogOptions{}, nil, map[LocaleID]map[MessageKey]Template{"en": {"undeclared": {Text: "x"}}}); err == nil {
		t.Fatal("unknown template")
	}
	c, err := NewCatalog(t.Context(), set, CatalogOptions{}, []MessageDefinition{good}, map[LocaleID]map[MessageKey]Template{"en": {"hello": {Text: "{{name}}{{name}}"}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.FormatDynamic(t.Context(), "en", "hello", map[string]Argument{"name": Text(strings.Repeat("x", MaxTextBytes))}); err == nil {
		t.Fatal("expanded text escaped its bound")
	}
	d := MessageDefinition{Key: "n", Plural: "n", Kind: Cardinal, Parameters: []Parameter{{Name: "n", Kind: NumberParameter}}}
	for _, forms := range []map[PluralForm]string{{One: "one"}, {Other: "other", "guessed": "bad"}} {
		if _, err := NewCatalog(t.Context(), set, CatalogOptions{}, []MessageDefinition{d}, map[LocaleID]map[MessageKey]Template{"en": {"n": {Forms: forms}}}); err == nil {
			t.Fatal("invalid plural forms")
		}
	}
}

func TestLoadCatalogStrictFilesAndNestedKeys(t *testing.T) {
	set, _ := NewLocaleSet("en", "en", "ms")
	definitions := []MessageDefinition{{Key: "welcome.name", Parameters: []Parameter{{Name: "name", Kind: TextParameter}}}, {Key: "items", Plural: "count", Kind: Cardinal, Parameters: []Parameter{{Name: "count", Kind: NumberParameter}}}}
	base := fstest.MapFS{"en/main.json": {Data: []byte(`{"welcome":{"name":"Hello {{name}}"}}`)}, "en/count.json": {Data: []byte(`{"items":{"$plural":{"one":"One item","other":"{{count}} items"}}}`)}, "ms/main.json": {Data: []byte(`{"welcome.name":"Helo {{name}}"}`)}}
	c, err := Load(t.Context(), base, set, CatalogOptions{}, definitions...)
	if err != nil {
		t.Fatal(err)
	}
	r, err := c.FormatDynamic(t.Context(), "ms", "items", map[string]Argument{"count": Number(decimal.FromInt64(2))})
	if err != nil || r.Text != "2 items" || !r.Fallback {
		t.Fatal(r, err)
	}
	for name, bad := range map[string]fstest.MapFS{
		"duplicate JSON key":       {"en/x.json": {Data: []byte(`{"welcome.name":"a","welcome.name":"b"}`)}},
		"duplicate flattened key":  {"en/x.json": {Data: []byte(`{"welcome":{"name":"a"},"welcome.name":"b"}`)}},
		"duplicate files":          {"en/a.json": {Data: []byte(`{"welcome.name":"a"}`)}, "en/b.json": {Data: []byte(`{"welcome.name":"b"}`)}},
		"duplicate locale aliases": {"en/a.json": {Data: []byte(`{}`)}, "EN/a.json": {Data: []byte(`{}`)}},
		"unknown locale":           {"fr/a.json": {Data: []byte(`{}`)}},
		"unknown message":          {"en/a.json": {Data: []byte(`{"unknown":"x"}`)}},
		"invalid leaf":             {"en/a.json": {Data: []byte(`{"welcome.name":42}`)}},
		"invalid plural":           {"en/a.json": {Data: []byte(`{"items":{"$plural":{"one":"one"}}}`)}},
		"trailing JSON":            {"en/a.json": {Data: []byte(`{} {}`)}},
		"root file":                {"a.json": {Data: []byte(`{}`)}},
		"symlink":                  {"en/a.json": {Mode: fs.ModeSymlink, Data: []byte("private")}},
		"oversize":                 {"en/a.json": {Data: []byte(strings.Repeat(" ", (1<<20)+1))}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Load(t.Context(), bad, set, CatalogOptions{}, definitions...); err == nil {
				t.Fatal("invalid catalog loaded")
			}
		})
	}
}

type hostileCatalogFS struct{ exit bool }

func (f hostileCatalogFS) Open(string) (fs.File, error) {
	if f.exit {
		runtime.Goexit()
	}
	panic("private filesystem diagnostic")
}
func TestLoadCatalogIsolatesExtensionFailure(t *testing.T) {
	set, _ := NewLocaleSet("en", "en")
	for _, exit := range []bool{false, true} {
		if _, err := Load(t.Context(), hostileCatalogFS{exit}, set, CatalogOptions{}); err == nil || strings.Contains(err.Error(), "private") {
			t.Fatal(err)
		}
	}
}

func FuzzCatalogLiteralArguments(f *testing.F) {
	for _, seed := range []string{"text", "{{name}}", "}} {{name}}", "<script>", "こんにちは", "bad\x00"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, text string) {
		if len(text) > MaxTextBytes {
			return
		}
		c := testCatalog(t, []MessageDefinition{{Key: "m", Parameters: []Parameter{{Name: "x", Kind: TextParameter}}}}, map[LocaleID]map[MessageKey]Template{"en": {"m": {Text: "[{{x}}]"}}})
		result, err := c.FormatDynamic(t.Context(), "en", "m", map[string]Argument{"x": Text(text)})
		if validText(text) && len(text)+2 <= MaxTextBytes {
			if err != nil || result.Text != "["+text+"]" {
				t.Fatal("argument was reinterpreted", err)
			}
		} else if err == nil {
			t.Fatal("invalid text escaped bound")
		}
	})
}
