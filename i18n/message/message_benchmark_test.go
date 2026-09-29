package message_test

import (
	"reflect"
	"testing"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/i18n"
	"github.com/weiloon1234/Foundry-Go/i18n/message"
)

type GreetingArgs struct {
	Name  string `json:"name"`
	Count int64  `json:"count"`
}

func greetingMessage(t testing.TB) (message.Message[GreetingArgs], *i18n.Catalog) {
	t.Helper()
	typ := reflect.TypeFor[GreetingArgs]()
	root := contract.TypeID(typ.PkgPath() + "." + typ.Name())
	descriptor := contract.DefineJSON[GreetingArgs](contract.Schema{Root: root, Types: []contract.Type{
		{ID: root, Kind: contract.ObjectKind, Properties: []contract.Property{{Name: "count", Type: "integer", Required: true}, {Name: "name", Type: "text", Required: true}}},
		{ID: "integer", Kind: contract.IntegerKind},
		{ID: "text", Kind: contract.StringKind},
	}})
	m := message.Define("greeting", descriptor, message.Options{Plural: "count"})
	definition, err := m.Definition()
	if err != nil {
		t.Fatal(err)
	}
	set, err := i18n.NewLocaleSet("en", "en", "ms")
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := i18n.NewCatalog(t.Context(), set, i18n.CatalogOptions{}, []i18n.MessageDefinition{definition}, map[i18n.LocaleID]map[i18n.MessageKey]i18n.Template{
		"en": {"greeting": {Forms: map[i18n.PluralForm]string{i18n.One: "Hello {{name}}, one message", i18n.Other: "Hello {{name}}, {{count}} messages"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return m, catalog
}

func TestTypedMessageFormatUsesDeclaredArguments(t *testing.T) {
	m, catalog := greetingMessage(t)
	for _, test := range []struct {
		locale i18n.LocaleID
		count  int64
		want   string
	}{{"en", 1, "Hello Ada, one message"}, {"ms", 3, "Hello Ada, 3 messages"}} {
		result, err := m.Format(t.Context(), catalog, test.locale, GreetingArgs{Name: "Ada", Count: test.count})
		if err != nil || result.Text != test.want {
			t.Fatal(test, result, err)
		}
	}
}

func BenchmarkTypedMessageFormat(b *testing.B) {
	m, catalog := greetingMessage(b)
	ctx := b.Context()
	args := GreetingArgs{Name: "Ada", Count: 3}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := m.Format(ctx, catalog, "ms", args); err != nil {
			b.Fatal(err)
		}
	}
}
