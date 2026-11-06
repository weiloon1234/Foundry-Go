package phone_test

import (
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/weiloon1234/Foundry-Go/countries"
	"github.com/weiloon1234/Foundry-Go/i18n"
	"github.com/weiloon1234/Foundry-Go/phone"
	"github.com/weiloon1234/Foundry-Go/validation"
	phonevalidation "github.com/weiloon1234/Foundry-Go/validation/phone"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestNormalizationAndValidationAgree(t *testing.T) {
	t.Parallel()
	for _, policy := range []phone.ExtensionPolicy{phone.RejectExtensions, phone.DiscardExtensions} {
		options := phone.Options{Extensions: policy}
		for _, region := range []countries.Code{"MY", "SG", "US", "ZZ"} {
			rule := phonevalidation.Number[string](region, options)
			for _, raw := range []string{"012-345 6789", "+60123456789", "9123 4567", "+6591234567", "650 253 0000", "+16502530000", "123", "", "  ", "+16502530000 ext 123", strings.Repeat(" ", 100) + "0123456789"} {
				normalized, parseErr := phone.Parse(region, raw, options)
				checkErr := validation.Check(rule, t.Context(), raw, validation.DefaultLimits())
				if (parseErr == nil) != (checkErr == nil) {
					t.Fatalf("parser/rule disagreement for %s %q: %v, %v", region, raw, parseErr, checkErr)
				}
				if parseErr == nil {
					if err := validation.Check(rule, t.Context(), normalized.String(), validation.DefaultLimits()); err != nil {
						t.Fatal("normalized number rejected", err)
					}
				}
			}
		}
	}
}

type contact struct {
	Country countries.Code
	Number  string
}

func contactRule() validation.Rule[contact] {
	country := validation.DefineField("country", func(in contact) countries.Code { return in.Country })
	number := validation.WithLabel(validation.DefineField("number", func(in contact) string { return in.Number }), "Contact phone")
	return phonevalidation.For(country, number, phone.Options{})
}

func TestSelectedRegionPathsMessagesAndPresence(t *testing.T) {
	t.Parallel()
	rule := contactRule()
	for _, in := range []contact{{"MY", "0123456789"}, {"SG", "91234567"}, {"US", "+16502530000"}} {
		if err := validation.Check(rule, t.Context(), in, validation.DefaultLimits()); err != nil {
			t.Fatal(err)
		}
	}
	for _, in := range []contact{{"MY", "+6591234567"}, {"MY", ""}, {"MY", "0123456789 ext 7"}, {"ZZ", "+16502530000"}} {
		var rejected *validation.Errors
		if err := validation.Check(rule, t.Context(), in, validation.DefaultLimits()); !errors.As(err, &rejected) {
			t.Fatal("expected validation rejection", err)
		}
		issue := rejected.Issues()[0]
		if issue.Path != "/number" || issue.Label != "Contact phone" || string(issue.Code) != string(phonevalidation.NumberRuleID) || issue.Message != "Enter a valid phone number for the selected country, without an extension." {
			t.Fatalf("issue = %+v", issue)
		}
	}
	optional := validation.Optional(validation.Nullable(rule))
	for _, input := range []value.Optional[value.Nullable[contact]]{{}, value.Set(value.Null[contact]()), value.Set(value.Of(contact{"MY", "0123456789"}))} {
		if err := validation.Check(optional, t.Context(), input, validation.DefaultLimits()); err != nil {
			t.Fatal("valid presence state rejected", err)
		}
	}
	if err := validation.Check(optional, t.Context(), value.Set(value.Of(contact{"MY", ""})), validation.DefaultLimits()); err == nil {
		t.Fatal("supplied empty number treated as absent")
	}
	// Separate declarations of the same rule ID must compose, including policies.
	if err := validation.ValidateRule(validation.All(contactRule(), contactRule())); err != nil {
		t.Fatal("repeated phone fields cannot compose", err)
	}
	if err := validation.ValidateRule(validation.All(phonevalidation.Number[string]("MY", phone.Options{}), phonevalidation.Number[string]("SG", phone.Options{Extensions: phone.DiscardExtensions}))); err != nil {
		t.Fatal("mixed policy declarations cannot compose", err)
	}
}

func phoneLeaf(t *testing.T, description validation.Description) validation.Description {
	t.Helper()
	if description.Spec != nil {
		return description
	}
	if len(description.Children) != 1 {
		t.Fatal("expected one phone leaf")
	}
	return phoneLeaf(t, description.Children[0])
}

func TestTranslationAndOwnedMetadata(t *testing.T) {
	t.Parallel()
	rule := phonevalidation.Number[string]("MY", phone.Options{})
	description, err := validation.DescribeRule(rule)
	if err != nil {
		t.Fatal(err)
	}
	leaf := phoneLeaf(t, description)
	if !description.ServerOnly || leaf.Spec.Translation == nil || leaf.Spec.Translation.Definition.Key != "validation.phone_without_extension" {
		t.Fatalf("metadata = %+v", description)
	}
	parameters := map[string]string{}
	for _, p := range leaf.Spec.Parameters {
		parameters[p.Name] = string(p.Value)
	}
	if parameters["max_bytes"] != "100" || parameters["extensions"] != "0" {
		t.Fatal("missing parser policy metadata", parameters)
	}
	leaf.Spec.Translation.Fallback[i18n.Other] = "changed"
	leaf.Spec.Parameters[0].Value[0] = '9'
	again, err := validation.DescribeRule(rule)
	if err != nil || phoneLeaf(t, again).Spec.Translation.Fallback[i18n.Other] == "changed" || string(phoneLeaf(t, again).Spec.Parameters[0].Value) != "100" {
		t.Fatal("metadata is not owned", err)
	}
	locales, err := i18n.NewLocaleSet("ms", "ms")
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := i18n.NewCatalog(t.Context(), locales, i18n.CatalogOptions{}, phonevalidation.MessageDefinitions(), map[i18n.LocaleID]map[i18n.MessageKey]i18n.Template{
		"ms": {"validation.phone_without_extension": {Text: "{{attribute}}: nombor telefon tidak sah."}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := validation.ValidateMessages(again, catalog); err != nil {
		t.Fatal(err)
	}
	var rejected *validation.Errors
	if err := validation.Check(contactRule(), t.Context(), contact{"MY", "+6591234567"}, validation.DefaultLimits()); !errors.As(err, &rejected) {
		t.Fatal("expected rejection", err)
	}
	localized, err := rejected.Localize(t.Context(), catalog, "ms")
	if err != nil || localized.Issues()[0].Message != "Contact phone: nombor telefon tidak sah." {
		t.Fatal("translation or label lost", err)
	}
	encoded, err := json.Marshal(again)
	if err != nil || strings.Contains(string(encoded), "+6591234567") {
		t.Fatal("metadata disclosed input", err)
	}
	definitions := phonevalidation.MessageDefinitions()
	definitions[0].Parameters[0].Name = "changed"
	if phonevalidation.MessageDefinitions()[0].Parameters[0].Name != "attribute" {
		t.Fatal("definitions are not owned")
	}
}

func TestConcurrentRegionsAndNamedText(t *testing.T) {
	t.Parallel()
	rule := contactRule()
	var work sync.WaitGroup
	for range 4 {
		for _, input := range []contact{{"MY", "0123456789"}, {"SG", "91234567"}, {"US", "+16502530000"}, {"MY", "+6591234567"}} {
			work.Go(func() {
				err := validation.Check(rule, t.Context(), input, validation.DefaultLimits())
				wantValid := input.Country != "MY" || input.Number == "0123456789"
				if (err == nil) != wantValid {
					t.Errorf("region leaked between checks: %s %q: %v", input.Country, input.Number, err)
				}
			})
		}
	}
	work.Wait()
	type phoneText string
	named := phonevalidation.Number[phoneText]("SG", phone.Options{})
	if err := validation.Check(named, t.Context(), phoneText("91234567"), validation.DefaultLimits()); err != nil {
		t.Fatal("named string rejected", err)
	}
}

func TestInvalidDeclarations(t *testing.T) {
	t.Parallel()
	for _, rule := range []validation.Rule[string]{
		phonevalidation.Number[string]("my", phone.Options{}),
		phonevalidation.Number[string]("MY", phone.Options{Extensions: phone.ExtensionPolicy(255)}),
	} {
		if validation.ValidateRule(rule) == nil {
			t.Fatal("invalid declaration accepted")
		}
	}
	var country validation.Field[contact, countries.Code]
	var number validation.Field[contact, string]
	if validation.ValidateRule(phonevalidation.For(country, number, phone.Options{})) == nil {
		t.Fatal("undefined fields accepted")
	}
}
