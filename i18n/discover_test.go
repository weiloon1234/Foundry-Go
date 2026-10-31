package i18n_test

import (
	"errors"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/i18n"
)

func TestDiscoverDefinitionsRejectsAmbiguousPluralArguments(t *testing.T) {
	for name, files := range map[string]fstest.MapFS{
		"user before files": {
			"en/uploads.json": {Data: []byte(`{"uploads":{"$plural":{"one":"{{user}} uploaded {{files}} file","other":"{{user}} uploaded {{files}} files"}}}`)},
		},
		"different form order": {
			"en/uploads.json": {Data: []byte(`{"uploads":{"$plural":{"one":"{{user}} uploaded {{files}} file","other":"{{files}} files uploaded by {{user}}"}}}`)},
		},
		"preferred name is not a declaration": {
			"en/uploads.json": {Data: []byte(`{"uploads":{"$plural":{"other":"{{user}} uploaded {{count}} files"}}}`)},
		},
		"different placeholders across forms": {
			"en/uploads.json": {Data: []byte(`{"uploads":{"$plural":{"one":"{{user}} uploaded one file","other":"{{files}} files uploaded"}}}`)},
		},
		"different placeholders across locales": {
			"en/uploads.json": {Data: []byte(`{"uploads":{"$plural":{"other":"{{files}} files"}}}`)},
			"ms/uploads.json": {Data: []byte(`{"uploads":{"$plural":{"other":"{{jumlah}} fail"}}}`)},
		},
		"no placeholder": {
			"en/uploads.json": {Data: []byte(`{"uploads":{"$plural":{"other":"Files uploaded"}}}`)},
		},
	} {
		t.Run(name, func(t *testing.T) {
			locales := []i18n.LocaleID{"en"}
			if _, exists := files["ms/uploads.json"]; exists {
				locales = append(locales, "ms")
			}
			set, err := i18n.NewLocaleSet("en", locales...)
			if err != nil {
				t.Fatal(err)
			}
			defs, err := i18n.DiscoverDefinitions(t.Context(), files, set)
			if !errors.Is(err, fault.Invalid) || defs != nil {
				t.Fatalf("expected atomic rejection of ambiguous plural arguments, got definitions %+v, error %v", defs, err)
			}
			for _, want := range []string{`"uploads"`, "exactly one placeholder", "MessageDefinition.Plural"} {
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("diagnostic %q does not contain %q", err.Error(), want)
				}
			}
		})
	}
}

func TestDiscoverDefinitionsInfersParametersAndPlurals(t *testing.T) {
	files := fstest.MapFS{
		"en/auth.json": {
			Data: []byte(`{
				"auth.sign_in.welcome": "Welcome back.",
				"auth.sign_in.locked_minutes": {
					"$plural": {
						"one": "Try again in {{minutes}} minute.",
						"other": "Try again in {{minutes}} minutes."
					}
				}
			}`),
		},
		"en/merchants.json": {
			Data: []byte(`{
				"merchants.results_for": "Results for “{{query}}”",
				"merchants.count_in": {
					"$plural": {
						"one": "{{count}} merchant",
						"other": "{{count}} merchants"
					}
				}
			}`),
		},
	}

	set, err := i18n.NewLocaleSet("en", "en")
	if err != nil {
		t.Fatal(err)
	}

	defs, err := i18n.DiscoverDefinitions(t.Context(), files, set)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(defs) != 4 {
		t.Fatalf("expected 4 definitions, got %d", len(defs))
	}

	// 1. auth.sign_in.locked_minutes (plural = minutes)
	if defs[0].Key != "auth.sign_in.locked_minutes" || defs[0].Plural != "minutes" || defs[0].Kind != i18n.Cardinal {
		t.Fatalf("unexpected def 0: %+v", defs[0])
	}
	if len(defs[0].Parameters) != 1 || defs[0].Parameters[0].Name != "minutes" || defs[0].Parameters[0].Kind != i18n.NumberParameter {
		t.Fatalf("unexpected def 0 params: %+v", defs[0].Parameters)
	}

	// 2. auth.sign_in.welcome (no params)
	if defs[1].Key != "auth.sign_in.welcome" || defs[1].Plural != "" || len(defs[1].Parameters) != 0 {
		t.Fatalf("unexpected def 1: %+v", defs[1])
	}

	// 3. merchants.count_in (plural = count)
	if defs[2].Key != "merchants.count_in" || defs[2].Plural != "count" || defs[2].Kind != i18n.Cardinal {
		t.Fatalf("unexpected def 2: %+v", defs[2])
	}
	if len(defs[2].Parameters) != 1 {
		t.Fatalf("unexpected def 2 params len: %+v", defs[2].Parameters)
	}
	if defs[2].Parameters[0].Name != "count" || defs[2].Parameters[0].Kind != i18n.NumberParameter {
		t.Fatalf("unexpected count param: %+v", defs[2].Parameters[0])
	}

	// 4. merchants.results_for (query = text)
	if defs[3].Key != "merchants.results_for" || defs[3].Plural != "" || len(defs[3].Parameters) != 1 {
		t.Fatalf("unexpected def 3: %+v", defs[3])
	}
	if defs[3].Parameters[0].Name != "query" || defs[3].Parameters[0].Kind != i18n.TextParameter {
		t.Fatalf("unexpected query param: %+v", defs[3].Parameters[0])
	}
}

func TestDiscoverDefinitionsUsesSolePluralPlaceholderAcrossLocalesAndForms(t *testing.T) {
	files := fstest.MapFS{
		"en/uploads.json": {Data: []byte(`{"uploads":{"$plural":{"one":"One file uploaded","other":"{{files}} files uploaded ({{files}} total)"}}}`)},
		"ms/uploads.json": {Data: []byte(`{"uploads":{"$plural":{"other":"{{files}} fail dimuat naik"}}}`)},
	}
	set, err := i18n.NewLocaleSet("en", "en", "ms")
	if err != nil {
		t.Fatal(err)
	}
	defs, err := i18n.DiscoverDefinitions(t.Context(), files, set)
	if err != nil {
		t.Fatal(err)
	}
	if len(defs) != 1 || defs[0].Key != "uploads" || defs[0].Plural != "files" || defs[0].Kind != i18n.Cardinal {
		t.Fatalf("unexpected plural definition: %+v", defs)
	}
	if len(defs[0].Parameters) != 1 || defs[0].Parameters[0] != (i18n.Parameter{Name: "files", Kind: i18n.NumberParameter}) {
		t.Fatalf("unexpected plural parameters: %+v", defs[0].Parameters)
	}
	if _, err := i18n.Load(t.Context(), files, set, i18n.CatalogOptions{}, defs...); err != nil {
		t.Fatalf("discovered definitions must compile with their templates: %v", err)
	}
}

func TestDiscoverDefinitionsRejectsMalformedPlaceholders(t *testing.T) {
	for name, file := range map[string]string{
		"unmatched right": `{"test": "hello }}"}`,
		"unterminated":    `{"test": "hello {{name"}`,
		"bad param name":  `{"test": "hello {{bad-name}}"`,
	} {
		t.Run(name, func(t *testing.T) {
			files := fstest.MapFS{
				"en/test.json": {Data: []byte(file)},
			}
			set, _ := i18n.NewLocaleSet("en", "en")
			if _, err := i18n.DiscoverDefinitions(t.Context(), files, set); err == nil {
				t.Fatalf("expected error for %s", name)
			}
		})
	}
}

func TestDiscoverDefinitionsValidatesLocalesStrictly(t *testing.T) {
	files := fstest.MapFS{
		"en/test.json": {Data: []byte(`{"test": "hello"}`)},
	}
	// 1. nil locales fails clearly
	if _, err := i18n.DiscoverDefinitions(t.Context(), files, nil); err == nil {
		t.Fatal("expected error for nil locales")
	}

	// 2. missing declared supported locale fails clearly
	setMulti, err := i18n.NewLocaleSet("en", "en", "ms")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := i18n.DiscoverDefinitions(t.Context(), files, setMulti); err == nil {
		t.Fatal("expected error for missing declared supported locale ms")
	}

	// 3. directory outside declared supported locale set fails clearly
	filesExtra := fstest.MapFS{
		"en/test.json": {Data: []byte(`{"test": "hello"}`)},
		"fr/test.json": {Data: []byte(`{"test": "bonjour"}`)},
	}
	setEn, err := i18n.NewLocaleSet("en", "en")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := i18n.DiscoverDefinitions(t.Context(), filesExtra, setEn); err == nil {
		t.Fatal("expected error for directory outside supported set fr")
	}
}
