package i18n

import (
	"encoding/json"
	"testing"
)

func FuzzPreparedMessageRoundTrip(f *testing.F) {
	f.Add("{{attribute}} is required.", "Name", false)
	f.Add("Literal {{unknown}}", "{{attribute}}", true)
	f.Add("{{attribute}}{{attribute}}", "Nama 用户", false)
	f.Add("", "", false)
	f.Fuzz(func(t *testing.T, fallback, attribute string, literal bool) {
		definition := MessageDefinition{Key: "test.fuzz", Parameters: []Parameter{{Name: "attribute", Kind: TextParameter}}}
		args := map[string]Argument{"attribute": Text(attribute)}
		var original PreparedMessage
		var err error
		if literal {
			original, err = PrepareLiteralMessage(definition, args, fallback)
		} else {
			original, err = PrepareMessage(definition, args, Template{Text: fallback})
		}
		if err != nil {
			return
		}
		wire, err := json.Marshal(original.Description())
		if err != nil {
			t.Fatal(err)
		}
		var recipe MessageRecipe
		if err := json.Unmarshal(wire, &recipe); err != nil {
			t.Fatal(err)
		}
		restored, err := recipe.Prepare()
		if err != nil {
			t.Fatal("accepted message could not be restored", err)
		}
		before, beforeErr := original.Format(t.Context(), nil, "")
		after, afterErr := restored.Format(t.Context(), nil, "")
		if (beforeErr == nil) != (afterErr == nil) || before != after {
			t.Fatal("message recipe changed rendering or output limits")
		}
		if afterErr == nil && (!validText(after.Text) || literal && after.Text != fallback) {
			t.Fatal("message output violated text/literal contract")
		}
	})
}
