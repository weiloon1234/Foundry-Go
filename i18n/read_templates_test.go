package i18n

import (
	"errors"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/fault"
)

// An application reads its JSON catalog without definitions, declares its own
// keys, and compiles both through NewCatalog with Load's rules.
func TestReadTemplatesLetsTheApplicationDeclareKeysBeforeCompiling(t *testing.T) {
	set, _ := NewLocaleSet("en", "en", "ms")
	files := fstest.MapFS{
		"en/auth.json":   {Data: []byte(`{"auth":{"failed":"Wrong {{identifier}} or password.","lockout":{"$plural":{"one":"Try again in {{minutes}} minute.","other":"Try again in {{minutes}} minutes."}}}}`)},
		"en/labels.json": {Data: []byte(`{"projects":{"status":{"draft":"Draft"}}}`)},
		"ms/labels.json": {Data: []byte(`{"projects":{"status":{"draft":"Draf"}}}`)},
		"ms/notes.txt":   {Data: []byte("ignored")},
	}
	templates, err := ReadTemplates(t.Context(), files, set)
	if err != nil {
		t.Fatal(err)
	}
	if templates["en"]["auth.failed"].Text != "Wrong {{identifier}} or password." || templates["ms"]["projects.status.draft"].Text != "Draf" || len(templates["ms"]) != 1 {
		t.Fatal("flattened templates", templates)
	}
	if forms := templates["en"]["auth.lockout"].Forms; forms["one"] != "Try again in {{minutes}} minute." || len(forms) != 2 {
		t.Fatal("plural forms", forms)
	}
	definitions := []MessageDefinition{
		{Key: "auth.failed", Parameters: []Parameter{{Name: "identifier", Kind: TextParameter}}},
		{Key: "auth.lockout", Parameters: []Parameter{{Name: "minutes", Kind: NumberParameter}}, Plural: "minutes", Kind: Cardinal},
		{Key: "projects.status.draft"},
	}
	catalog, err := NewCatalog(t.Context(), set, CatalogOptions{}, definitions, templates)
	if err != nil {
		t.Fatal(err)
	}
	result, err := catalog.FormatDynamic(t.Context(), "en", "auth.lockout", map[string]Argument{"minutes": Number(decimal.FromInt64(5))})
	if err != nil || result.Text != "Try again in 5 minutes." {
		t.Fatal(result, err)
	}
	// The result is the caller's: changing it does not reach another read.
	templates["en"]["auth.failed"] = Template{Text: "changed"}
	again, err := ReadTemplates(t.Context(), files, set)
	if err != nil || again["en"]["auth.failed"].Text != "Wrong {{identifier}} or password." {
		t.Fatal("read shares state", err)
	}
}

// NewCatalog keeps rejecting a template that disagrees with its declaration, so
// an application declaring JSON keys itself cannot accept a mistyped template.
func TestCompilingReadTemplatesChecksTheirDeclarations(t *testing.T) {
	set, _ := NewLocaleSet("en", "en")
	for name, test := range map[string]struct {
		file       string
		definition MessageDefinition
		reason     string
	}{
		"undeclared placeholder": {`{"auth":{"failed":"Wrong {{identifier}}."}}`, MessageDefinition{Key: "auth.failed"}, "placeholder"},
		"plural without plural":  {`{"auth":{"failed":{"$plural":{"other":"Wrong."}}}}`, MessageDefinition{Key: "auth.failed"}, "nonplural message cannot declare plural forms"},
		"text for plural":        {`{"auth":{"failed":"Wrong."}}`, MessageDefinition{Key: "auth.failed", Parameters: []Parameter{{Name: "count", Kind: NumberParameter}}, Plural: "count", Kind: Cardinal}, "plural"},
	} {
		templates, err := ReadTemplates(t.Context(), fstest.MapFS{"en/auth.json": {Data: []byte(test.file)}}, set)
		if err != nil {
			t.Fatal(name, err)
		}
		_, err = NewCatalog(t.Context(), set, CatalogOptions{}, []MessageDefinition{test.definition}, templates)
		if !errors.Is(err, fault.Invalid) || !strings.Contains(err.Error(), "en") || !strings.Contains(err.Error(), "auth.failed") || !strings.Contains(err.Error(), test.reason) {
			t.Fatalf("%s: %v", name, err)
		}
	}
}

func TestReadTemplatesSharesLoadsRejections(t *testing.T) {
	set, _ := NewLocaleSet("en", "en")
	for name, test := range map[string]struct {
		files fstest.MapFS
		names []string
	}{
		"repeated across files": {fstest.MapFS{"en/a.json": {Data: []byte(`{"auth":{"failed":"A"}}`)}, "en/b.json": {Data: []byte(`{"auth.failed":"B"}`)}}, []string{"en", "auth.failed", "en/a.json"}},
		"unsupported locale":    {fstest.MapFS{"fr/a.json": {Data: []byte(`{"x":"y"}`)}}, []string{"fr"}},
		"duplicate JSON name":   {fstest.MapFS{"en/a.json": {Data: []byte(`{"x":"y","x":"z"}`)}}, []string{"en/a.json"}},
		"malformed leaf":        {fstest.MapFS{"en/a.json": {Data: []byte(`{"x":1}`)}}, []string{"en/a.json", "x"}},
		"root file":             {fstest.MapFS{"messages.json": {Data: []byte(`{}`)}}, []string{"messages.json"}},
		"invalid key":           {fstest.MapFS{"en/a.json": {Data: []byte(`{"Upper":"y"}`)}}, []string{"Upper"}},
	} {
		_, err := ReadTemplates(t.Context(), test.files, set)
		if !errors.Is(err, fault.Invalid) {
			t.Fatalf("%s accepted: %v", name, err)
		}
		for _, want := range test.names {
			if !strings.Contains(err.Error(), want) {
				t.Fatalf("%s: %v does not name %s", name, err, want)
			}
		}
	}
	if _, err := ReadTemplates(t.Context(), nil, set); err == nil {
		t.Fatal("nil source accepted")
	}
}
