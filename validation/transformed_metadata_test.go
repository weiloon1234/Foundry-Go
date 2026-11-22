package validation_test

import (
	"encoding/json"
	"testing"

	"github.com/weiloon1234/Foundry-Go/auth/password"
	"github.com/weiloon1234/Foundry-Go/i18n"
	"github.com/weiloon1234/Foundry-Go/validation"
)

func requireTransformed[T any](t *testing.T, rule validation.Rule[T], serverOnly, transformed bool) {
	t.Helper()
	info, err := validation.DescribeRule(rule)
	if err != nil || info.ServerOnly != serverOnly || info.Transformed != transformed {
		t.Fatalf("metadata: server_only=%v transformed=%v, want %v %v, error %v", info.ServerOnly, info.Transformed, serverOnly, transformed, err)
	}
}

// A bound on a value whose Go form differs from its wire form is marked
// transformed, so clients never apply it to the wire text; a rule the browser
// simply does not run (a password policy) is server-only without being
// transformed, and its bounds stay publishable.
func TestTransformedRuleMetadata(t *testing.T) {
	t.Parallel()
	requireTransformed(t, validation.MaxLength[string](5), false, false)
	requireTransformed(t, validation.MaxBytes[string](5), false, false)
	requireTransformed(t, validation.MaxLength[decodedText](5), true, true)
	requireTransformed(t, validation.MinLength[json.Number](1), true, true)
	requireTransformed(t, validation.MaxItems[[]byte](4), true, true)
	requireTransformed(t, validation.MaxItems[encodedItems](4), true, true)
	requireTransformed(t, validation.MaxItems[[]string](4), false, false)
	requireTransformed(t, validation.Password[string](validation.DefaultPasswordOptions()), true, false)

	// Normalized metadata cannot claim a transformed value a browser may check.
	forged, err := validation.DescribeRule(validation.MaxLength[string](5))
	if err != nil {
		t.Fatal(err)
	}
	forged.Transformed = true
	if _, err := forged.Normalize(); err == nil {
		t.Fatal("a transformed leaf without server_only was accepted")
	}
}

type decodedTexts map[i18n.LocaleID]decodedText

type decodedArray [2]decodedText

func (*decodedArray) UnmarshalJSON([]byte) error { panic("metadata invoked decoder") }

type decodedMap map[string]string

func (*decodedMap) UnmarshalJSON([]byte) error { panic("metadata invoked decoder") }

// A leaf on a carrier of a value (Optional, Nullable or a Pair of the value)
// judges the carried value, so that value decides whether the leaf is
// transformed: a requirement on a decoded text publishes no browser
// requirement, since Go judges the decoded value, not the wire text.
func TestWrappedRulesTakeTheCarriedValuesTransformation(t *testing.T) {
	t.Parallel()
	requireTransformed(t, validation.Required[decodedText](), true, true)
	requireTransformed(t, validation.RequiredNullable[decodedText](), true, true)
	requireTransformed(t, validation.Prohibited[decodedText](), true, true)
	requireTransformed(t, validation.ProhibitedNullable[decodedText](), true, true)
	requireTransformed(t, validation.Same[decodedText](), true, true)
	requireTransformed(t, validation.Different[decodedText](), true, true)
	requireTransformed(t, validation.Required[decodedArray](), true, true)
	requireTransformed(t, validation.NonEmpty[decodedArray](), true, true)
	requireTransformed(t, validation.Required[decodedMap](), true, true)
	requireTransformed(t, validation.NonEmpty[decodedMap](), true, true)
	requireTransformed(t, validation.Required[[2]decodedText](), false, false)
	requireTransformed(t, validation.Required[[2]string](), false, false)
	requireTransformed(t, validation.Required[map[string]string](), false, false)
	requireTransformed(t, validation.Required[string](), false, false)
	requireTransformed(t, validation.RequiredNullable[string](), false, false)
	requireTransformed(t, validation.Same[string](), false, false)
	requireTransformed(t, validation.Required[bool](), false, false)
	requireTransformed(t, validation.PasswordValue[password.Plaintext](validation.DefaultPasswordOptions()), true, false)

	// The rules after Required keep their own value's transformation.
	info, err := validation.DescribeRule(validation.Required[decodedText](validation.MaxLength[decodedText](3)))
	if err != nil {
		t.Fatal(err)
	}
	if !info.ServerOnly || len(info.Children) != 2 || !info.Children[0].Transformed {
		t.Fatalf("Required with rules: %+v", info)
	}

	// A locale requirement judges the map's text.
	catalog, err := i18n.NewLocaleSet("en", "en", "ms")
	if err != nil {
		t.Fatal(err)
	}
	if required := requiredLocaleLeaf(t, validation.Locales[decodedTexts](catalog, i18n.DefaultLocale)); !required.Transformed || !required.ServerOnly {
		t.Fatalf("locale requirement on decoded text: %+v", required)
	}
	if required := requiredLocaleLeaf(t, validation.Locales[translated](catalog, i18n.DefaultLocale)); required.Transformed {
		t.Fatalf("locale requirement on plain text is transformed: %+v", required)
	}
}

func requiredLocaleLeaf[T any](t *testing.T, rule validation.Rule[T]) validation.Description {
	t.Helper()
	info, err := validation.DescribeRule(rule)
	if err != nil {
		t.Fatal(err)
	}
	var found []validation.Description
	var walk func(validation.Description)
	walk = func(node validation.Description) {
		if node.Kind == validation.LeafKind && node.Spec != nil && node.Spec.ID == "foundry.required" {
			found = append(found, node)
		}
		for _, child := range node.Children {
			walk(child)
		}
	}
	walk(info)
	if len(found) != 1 {
		t.Fatalf("locale rule has %d requirement leaves", len(found))
	}
	return found[0]
}
