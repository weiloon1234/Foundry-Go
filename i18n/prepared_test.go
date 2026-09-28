package i18n

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"strings"
	"testing"
)

func TestPreparedMessageOwnsValidatedFallbackAndArguments(t *testing.T) {
	d := MessageDefinition{Key: "test.items", Parameters: []Parameter{{"attribute", TextParameter}, {"count", NumberParameter}}, Plural: "count", Kind: Cardinal}
	number, _ := decimal.Parse("2")
	args := map[string]Argument{"attribute": Text("Items"), "count": Number(number)}
	fallback := Template{Forms: map[PluralForm]string{One: "{{attribute}} {{count}} item", Other: "{{attribute}} {{count}} items"}}
	prepared, err := PrepareMessage(d, args, fallback)
	if err != nil {
		t.Fatal(err)
	}
	args["attribute"] = Text("changed")
	fallback.Forms[Other] = "changed"
	d.Parameters[0].Name = "changed"
	recipe := prepared.Description()
	restored, err := recipe.Prepare()
	if err != nil {
		t.Fatal(err)
	}
	recipe.Fallback[Other] = "changed"
	for _, m := range []PreparedMessage{prepared, restored} {
		result, err := m.Format(t.Context(), nil, "")
		if err != nil || result.Text != "Items 2 items" {
			t.Fatal(result, err)
		}
	}
	replacement, err := prepared.WithText("attribute", "{{count}}")
	if err != nil {
		t.Fatal(err)
	}
	result, err := replacement.Format(t.Context(), nil, "")
	if err != nil || result.Text != "{{count}} 2 items" {
		t.Fatal("recursive substitution", result, err)
	}
	if _, err := prepared.WithText("count", "bad"); err == nil {
		t.Fatal("numeric argument replaced by text")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := prepared.Format(ctx, nil, ""); err == nil {
		t.Fatal("cancellation ignored")
	}
}
func TestPreparedMessageRejectsMalformedAndOversizedRecipes(t *testing.T) {
	d := MessageDefinition{Key: "test.text", Parameters: []Parameter{{"name", TextParameter}}}
	for _, text := range []string{"{{unknown}}", "{{name", strings.Repeat("x", MaxTextBytes+1)} {
		if _, err := PrepareMessage(d, map[string]Argument{"name": Text("N")}, Template{Text: text}); err == nil {
			t.Fatal("bad template accepted")
		}
	}
	literal, err := PrepareLiteralMessage(d, map[string]Argument{"name": Text("N")}, "{{unknown}}")
	if err != nil {
		t.Fatal(err)
	}
	restored, err := literal.Description().Prepare()
	if err != nil {
		t.Fatal(err)
	}
	result, err := restored.Format(t.Context(), nil, "")
	if err != nil || result.Text != "{{unknown}}" {
		t.Fatal(result, err)
	}
	recipe := literal.Description()
	recipe.Arguments = append(recipe.Arguments, recipe.Arguments[0])
	if _, err := recipe.Prepare(); err == nil {
		t.Fatal("duplicate argument accepted")
	}
}
