package i18n

import (
	"errors"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/weiloon1234/Foundry-Go/fault"
)

func TestLoadCatalogIgnoresDotEntriesAndNonJSONFiles(t *testing.T) {
	set, _ := NewLocaleSet("en", "en", "ms")
	definitions := []MessageDefinition{{Key: "greeting"}}
	files := fstest.MapFS{
		".DS_Store":         {Data: []byte("\x00\x00binary")},
		".git/config":       {Data: []byte("[core]")},
		"README.md":         {Data: []byte("# Catalog")},
		"en/.DS_Store":      {Data: []byte("\x00binary")},
		"en/.gitkeep":       {Data: []byte{}},
		"en/.hidden.json":   {Data: []byte(`not json`)},
		"en/.cache/x.json":  {Data: []byte(`not json`)},
		"en/notes.txt":      {Data: []byte("draft")},
		"en/messages.json":  {Data: []byte(`{"greeting":"Hello"}`)},
		"ms/messages.json":  {Data: []byte(`{"greeting":"Helo"}`)},
		"ms/messages.json~": {Data: []byte(`{"greeting":`)},
	}
	c, err := Load(t.Context(), files, set, CatalogOptions{}, definitions...)
	if err != nil {
		t.Fatal(err)
	}
	if text, err := c.Label(t.Context(), "ms", "greeting"); err != nil || text != "Helo" {
		t.Fatal(text, err)
	}
}

func TestLoadCatalogErrorsNameLocaleFileAndKey(t *testing.T) {
	set, _ := NewLocaleSet("en", "en", "ms")
	definitions := []MessageDefinition{{Key: "welcome.name", Parameters: []Parameter{{Name: "name", Kind: TextParameter}}}, {Key: "items", Plural: "count", Kind: Cardinal, Parameters: []Parameter{{Name: "count", Kind: NumberParameter}}}}
	for name, test := range map[string]struct {
		files fstest.MapFS
		want  []string
	}{
		"unknown message":      {fstest.MapFS{"ms/a.json": {Data: []byte(`{"nested":{"unknown":"x"}}`)}}, []string{`locale "ms"`, `file "ms/a.json"`, `key "nested.unknown"`, "not declared"}},
		"undeclared parameter": {fstest.MapFS{"en/a.json": {Data: []byte(`{"welcome":{"name":"Hi {{nmae}}"}}`)}}, []string{`locale "en"`, `file "en/a.json"`, `key "welcome.name"`, `placeholder "nmae"`}},
		"duplicate file key":   {fstest.MapFS{"en/a.json": {Data: []byte(`{"welcome.name":"a"}`)}, "en/b.json": {Data: []byte(`{"welcome":{"name":"b"}}`)}}, []string{`file "en/b.json"`, `key "welcome.name"`, `already defined in "en/a.json"`}},
		"missing other form":   {fstest.MapFS{"en/a.json": {Data: []byte(`{"items":{"$plural":{"one":"one"}}}`)}}, []string{`locale "en"`, `key "items"`, "other form"}},
		"invalid leaf":         {fstest.MapFS{"en/a.json": {Data: []byte(`{"welcome":{"name":42}}`)}}, []string{`file "en/a.json"`, `key "welcome.name"`}},
		"malformed JSON":       {fstest.MapFS{"ms/a.json": {Data: []byte(`{"welcome.name":`)}}, []string{`locale "ms"`, `file "ms/a.json"`}},
		"unsupported locale":   {fstest.MapFS{"fr/a.json": {Data: []byte(`{}`)}}, []string{`locale "fr"`, `file "fr"`}},
		"root JSON file":       {fstest.MapFS{"en.json": {Data: []byte(`{}`)}}, []string{`file "en.json"`, "locale directories"}},
		"nested directory":     {fstest.MapFS{"en/extra/a.json": {Data: []byte(`{}`)}}, []string{`file "en/extra"`}},
		"symlink":              {fstest.MapFS{"en/a.json": {Mode: fs.ModeSymlink, Data: []byte("private target")}}, []string{`file "en/a.json"`, "symlink"}},
		"hostile key":          {fstest.MapFS{"en/a.json": {Data: []byte(`{"Bad Key\u0007` + strings.Repeat("x", 300) + `":"x"}`)}}, []string{`key "Bad Key\a`, `"...`}},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Load(t.Context(), test.files, set, CatalogOptions{}, definitions...)
			if !errors.Is(err, fault.Invalid) {
				t.Fatal(err)
			}
			for _, want := range test.want {
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("%q does not contain %q", err.Error(), want)
				}
			}
			if strings.Contains(err.Error(), "private target") || strings.Contains(err.Error(), "Hi {{") {
				t.Fatal("catalog error exposed file content", err)
			}
		})
	}
}

func TestDeclarationAndArgumentErrorsNameKeyAndParameter(t *testing.T) {
	err := MessageDefinition{Key: "cart.items", Plural: "count", Kind: Cardinal, Parameters: []Parameter{{Name: "count", Kind: TextParameter}}}.Validate()
	if err == nil || !strings.Contains(err.Error(), `"cart.items"`) || !strings.Contains(err.Error(), `"count"`) {
		t.Fatal(err)
	}
	c := testCatalog(t, []MessageDefinition{{Key: "welcome", Parameters: []Parameter{{Name: "name", Kind: TextParameter}}}}, nil)
	_, err = c.FormatDynamic(t.Context(), "en", "welcome", map[string]Argument{"name": Boolean(true)})
	if err == nil || !strings.Contains(err.Error(), `"welcome"`) || !strings.Contains(err.Error(), `parameter "name"`) || strings.Contains(err.Error(), "true") {
		t.Fatal(err)
	}
	if err := c.Accepts(MessageDefinition{Key: "welcome"}); err == nil || !strings.Contains(err.Error(), "signature differs") {
		t.Fatal(err)
	}
}
