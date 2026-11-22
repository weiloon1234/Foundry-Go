package clientcontracts_test

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"foundry.test/consumer/internal/clientfixture"
	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/contract/manifest"
	"github.com/weiloon1234/Foundry-Go/i18n"
	"github.com/weiloon1234/Foundry-Go/validation"
)

// limitLeaf is a built-in rule leaf as the manifest carries it.
func limitLeaf(id string, parameters map[string]any) validation.Description {
	spec := validation.Spec{ID: validation.RuleID(id), Message: "Invalid."}
	for name, value := range parameters {
		data, _ := json.Marshal(value)
		spec.Parameters = append(spec.Parameters, validation.Parameter{Name: name, Value: data})
	}
	return validation.Description{Kind: validation.LeafKind, Spec: &spec}
}

func limitField(name string, children ...validation.Description) validation.Description {
	return validation.Description{Kind: validation.FieldKind, Field: name, Children: []validation.Description{{Kind: validation.AllKind, Children: children}}}
}

func wrap(kind validation.Kind, child validation.Description) validation.Description {
	return validation.Description{Kind: kind, Children: []validation.Description{child}}
}

// TestClientFieldLimitsAndBindings runs the generated limits view and field
// binder against one request whose rule tree holds every built-in limit kind,
// presence wrappers, a transformed value, a conditional rule, booleans that Go
// requires to be true (directly and under Required) or only allows to be
// false, a bounded float, a fixed-length code, an instant, translated maps,
// an enum-keyed map, an array, a nested object, an optional nullable nested
// object, rows of fields with a list inside each row, a second list of the
// same rows, a nested object and a list of objects without labels of their
// own, a self-referencing object and each rule that rejects an empty value,
// sent by an operation without and one with a preparation step, an update
// whose body is another shape, and a multipart body with a single upload,
// repeated uploads, repeated text and a JSON part: the published facts must
// match what the Go rules check.
func TestClientFieldLimitsAndBindings(t *testing.T) {
	tools := clientfixture.Load(t)
	source, err := fixture(t).Manifest(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	doc, err := source.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	label := func(key string) contract.Presentation {
		return contract.Presentation{LabelKey: i18n.MessageKey(key)}
	}
	help := contract.Presentation{LabelKey: "fields.text", HelpKey: "help.text"}
	doc.Types = append(doc.Types,
		contract.Type{ID: "client.Count", Kind: contract.IntegerKind, Bits: 32, Signed: true},
		contract.Type{ID: "client.Ratio", Kind: contract.NumberKind, Bits: 64},
		contract.Type{ID: "client.Flag", Kind: contract.BooleanKind},
		contract.Type{ID: "client.Instant", Kind: contract.StringKind, Format: contract.DateTimeFormat},
		contract.Type{ID: "client.NullableText", Kind: contract.StringKind, Nullable: true},
		// Nullable, as a Go map is: the locale requirement still requires it.
		contract.Type{ID: "client.Names", Kind: contract.MapKind, Element: "string", Nullable: true},
		contract.Type{ID: "client.Codes", Kind: contract.ArrayKind, Element: "string"},
		contract.Type{ID: "client.Account", Kind: contract.ObjectKind, Properties: []contract.Property{
			{Name: "username", Type: "string", Required: true, Presentation: label("fields.username")},
			{Name: "email", Type: "client.NullableText", Required: true, Presentation: label("fields.email")},
			{Name: "internal", Type: "string", Required: true},
		}},
		contract.Type{ID: "client.Prefs", Kind: contract.ObjectKind, Nullable: true, Properties: []contract.Property{
			{Name: "color", Type: "string", Required: true, Presentation: label("fields.color")},
		}},
		contract.Type{ID: "client.Slot", Kind: contract.ObjectKind, Properties: []contract.Property{
			{Name: "opens", Type: "string", Required: true, Presentation: label("fields.opens")},
		}},
		contract.Type{ID: "client.Slots", Kind: contract.ArrayKind, Element: "client.Slot"},
		contract.Type{ID: "client.Row", Kind: contract.ObjectKind, Properties: []contract.Property{
			{Name: "name", Type: "string", Required: true, Presentation: label("fields.row_name")},
			{Name: "slots", Type: "client.Slots", Required: true, Presentation: label("fields.slots")},
		}},
		contract.Type{ID: "client.Rows", Kind: contract.ArrayKind, Element: "client.Row"},
		contract.Type{ID: "client.Wallet", Kind: contract.ObjectKind, Properties: []contract.Property{
			{Name: "address", Type: "string", Required: true, Presentation: label("fields.address")},
			{Name: "note", Type: "string", Required: true},
		}},
		contract.Type{ID: "client.Link", Kind: contract.ObjectKind, Properties: []contract.Property{
			{Name: "role", Type: "string", Required: true, Presentation: label("fields.role")},
		}},
		contract.Type{ID: "client.Links", Kind: contract.ArrayKind, Element: "client.Link"},
		// A self-reference through an alias, as value.Optional wraps one.
		contract.Type{ID: "client.Filter", Kind: contract.ObjectKind, Properties: []contract.Property{
			{Name: "name", Type: "string", Required: true, Presentation: label("fields.filter_name")},
			{Name: "not", Type: "client.OptionalFilter", Presentation: label("fields.not")},
		}},
		contract.Type{ID: "client.OptionalFilter", Kind: contract.AliasKind, Element: "client.Filter"},
		contract.Type{ID: "client.States", Kind: contract.MapKind, Element: "string", Nullable: true, Key: &contract.JSONKeyInfo{
			Value: contract.Type{ID: "client.State", Kind: contract.StringKind, Cases: []json.RawMessage{json.RawMessage(`"open"`), json.RawMessage(`"shut"`)}}, Syntax: contract.EnumJSONKeySyntax, ServerOnly: true,
		}},
		contract.Type{ID: "client.LimitsForm", Kind: contract.ObjectKind, Properties: []contract.Property{
			{Name: "text", Type: "string", Required: true, Presentation: help},
			{Name: "optional_text", Type: "string", Presentation: label("fields.optional_text")},
			{Name: "nullable_text", Type: "client.NullableText", Required: true, Presentation: label("fields.nullable_text")},
			{Name: "transformed", Type: "string", Required: true, Presentation: label("fields.transformed")},
			{Name: "guarded", Type: "string", Required: true, Presentation: label("fields.guarded")},
			{Name: "count", Type: "client.Count", Required: true, Presentation: label("fields.count")},
			{Name: "ratio", Type: "client.Ratio", Required: true, Presentation: label("fields.ratio")},
			{Name: "price", Type: "github.com/weiloon1234/Foundry-Go/decimal.Decimal", Required: true, Presentation: label("fields.price")},
			{Name: "names", Type: "client.Names", Required: true, Presentation: label("fields.names")},
			{Name: "codes", Type: "client.Codes", Required: true, Presentation: label("fields.codes")},
			{Name: "min_text", Type: "string", Required: true, Presentation: label("fields.min_text")},
			{Name: "secret", Type: "string", Required: true, Presentation: contract.Presentation{LabelKey: "fields.secret", Kind: contract.PasswordPresentation}},
			{Name: "choice", Type: "string", Required: true, Presentation: label("fields.choice")},
			{Name: "supplied", Type: "string", Presentation: label("fields.supplied")},
			{Name: "optional_names", Type: "client.Names", Presentation: label("fields.optional_names")},
			{Name: "account", Type: "client.Account", Required: true, Presentation: label("fields.account")},
			{Name: "prefs", Type: "client.Prefs", Presentation: label("fields.prefs")},
			{Name: "rows", Type: "client.Rows", Required: true, Presentation: label("fields.rows")},
			{Name: "more_rows", Type: "client.Rows", Presentation: label("fields.more_rows")},
			{Name: "wallet", Type: "client.Wallet"},
			{Name: "links", Type: "client.Links"},
			{Name: "filter", Type: "client.Filter", Presentation: label("fields.filter")},
			{Name: "states", Type: "client.States", Required: true, Presentation: label("fields.states")},
			{Name: "unlabelled", Type: "string", Required: true},
			{Name: "consent", Type: "client.Flag", Presentation: label("fields.consent")},
			{Name: "agreed", Type: "client.Flag", Required: true, Presentation: label("fields.agreed")},
			{Name: "refused", Type: "client.Flag", Required: true, Presentation: label("fields.refused")},
			{Name: "maybe", Type: "client.Flag", Presentation: label("fields.maybe")},
			{Name: "share", Type: "client.Ratio", Required: true, Presentation: label("fields.share")},
			{Name: "pin", Type: "string", Required: true, Presentation: label("fields.pin")},
			{Name: "starts", Type: "client.Instant", Required: true, Presentation: label("fields.starts")},
		}},
	)
	doc.Roots = append(doc.Roots, "client.LimitsForm")
	transformed := limitLeaf("foundry.max_length", map[string]any{"max": 5})
	transformed.ServerOnly, transformed.Transformed = true, true
	bytes := limitLeaf("foundry.max_bytes", map[string]any{"max": 30})
	// Accepted is server-only in Go: the browser validator does not run it.
	accepted := limitLeaf("foundry.accepted", nil)
	accepted.ServerOnly = true
	body := validation.Description{Kind: validation.AllKind, Children: []validation.Description{
		limitField("text", limitLeaf("foundry.non_blank", nil), limitLeaf("foundry.min_length", map[string]any{"min": 3}), limitLeaf("foundry.max_length", map[string]any{"max": 10}), limitLeaf("foundry.max_length", map[string]any{"max": 12}), bytes),
		limitField("optional_text", wrap(validation.OptionalKind, validation.Description{Kind: validation.BailKind, Children: []validation.Description{limitLeaf("foundry.non_blank", nil), limitLeaf("foundry.min_length", map[string]any{"min": 2})}})),
		limitField("nullable_text", wrap(validation.NullableKind, limitLeaf("foundry.non_blank", nil))),
		limitField("transformed", transformed, limitLeaf("foundry.non_empty", map[string]any{"empty_kind": "value"})),
		limitField("guarded", validation.Description{Kind: validation.WhenKind, Children: []validation.Description{limitLeaf("foundry.non_blank", nil), limitLeaf("foundry.max_length", map[string]any{"max": 2})}}),
		limitField("count", limitLeaf("foundry.min", map[string]any{"min": 1}), limitLeaf("foundry.max", map[string]any{"max": 10})),
		limitField("ratio", limitLeaf("foundry.min", map[string]any{"min": 0.5})),
		limitField("price", limitLeaf("foundry.decimal_min", map[string]any{"min": "0.01"}), limitLeaf("foundry.decimal_places", map[string]any{"places": 2})),
		limitField("names", limitLeaf("foundry.required", map[string]any{"empty_kind": "text", "locales": "default"}), validation.Description{Kind: validation.EachValueKind, ServerOnly: true, Children: []validation.Description{limitLeaf("foundry.max_bytes", map[string]any{"max": 12})}}),
		limitField("codes", limitLeaf("foundry.max_items", map[string]any{"max": 3}), wrap(validation.EachKind, limitLeaf("foundry.max_length", map[string]any{"max": 4}))),
		limitField("min_text", limitLeaf("foundry.min_length", map[string]any{"min": 2})),
		limitField("secret", limitLeaf("foundry.password", map[string]any{"min": 1, "max": 64})),
		limitField("choice", limitLeaf("foundry.one_of", map[string]any{"values": []string{"a", "b"}})),
		limitField("supplied", validation.Description{Kind: validation.BailKind, Children: []validation.Description{limitLeaf("foundry.required", map[string]any{"empty_kind": "text"}), wrap(validation.OptionalKind, limitLeaf("foundry.max_length", map[string]any{"max": 8}))}}),
		limitField("optional_names", wrap(validation.OptionalKind, limitLeaf("foundry.required", map[string]any{"empty_kind": "text", "locales": "default"}))),
		limitField("account", limitField("username", limitLeaf("foundry.non_blank", nil), limitLeaf("foundry.max_length", map[string]any{"max": 8}))),
		limitField("prefs", wrap(validation.OptionalKind, wrap(validation.NullableKind, limitField("color", limitLeaf("foundry.non_blank", nil))))),
		// Required(Accepted()) on an optional boolean, Accepted on a required
		// one, OneOf(false), and Optional(Accepted()) alone.
		limitField("consent", validation.Description{Kind: validation.BailKind, Children: []validation.Description{limitLeaf("foundry.required", map[string]any{"empty_kind": "value"}), wrap(validation.OptionalKind, validation.Description{Kind: validation.BailKind, Children: []validation.Description{accepted}})}}),
		limitField("agreed", accepted),
		limitField("refused", limitLeaf("foundry.one_of", map[string]any{"values": []bool{false}})),
		limitField("maybe", wrap(validation.OptionalKind, accepted)),
		limitField("share", limitLeaf("foundry.min", map[string]any{"min": 0}), limitLeaf("foundry.max", map[string]any{"max": 1})),
		limitField("pin", limitLeaf("foundry.min_length", map[string]any{"min": 6}), limitLeaf("foundry.max_length", map[string]any{"max": 6})),
		limitField("rows", limitLeaf("foundry.max_items", map[string]any{"max": 3}), wrap(validation.EachKind, validation.Description{Kind: validation.AllKind, Children: []validation.Description{
			limitField("name", limitLeaf("foundry.non_blank", nil), limitLeaf("foundry.max_length", map[string]any{"max": 6})),
			limitField("slots", wrap(validation.EachKind, limitField("opens", limitLeaf("foundry.min_length", map[string]any{"min": 4})))),
		}})),
	}}
	tree := validation.Description{Kind: validation.AllKind, Children: []validation.Description{limitField("body", body)}}
	// genericEcho takes the same request with a preparation step: Go judges
	// it only after application code has changed it.
	found := 0
	// A JSON part's object binds as its labelled fields.
	for i := range doc.Types {
		if doc.Types[i].ID == "foundry.test/consumer/httpuploads.Settings" {
			for j := range doc.Types[i].Properties {
				if doc.Types[i].Properties[j].Name == "caption" {
					doc.Types[i].Properties[j].Presentation = label("fields.caption")
					found++
				}
			}
		}
	}
	for i := range doc.HTTP {
		// An upload part a form binds before a file is chosen, repeated
		// uploads, repeated text and a JSON part.
		if doc.HTTP[i].Name == "uploadsProfile" && doc.HTTP[i].Body != nil {
			for j := range doc.HTTP[i].Body.Parts {
				switch name := doc.HTTP[i].Body.Parts[j].Name; name {
				case "document", "photos", "settings":
					doc.HTTP[i].Body.Parts[j].Presentation = label("fields." + name)
					found++
				case "tags[]":
					doc.HTTP[i].Body.Parts[j].Presentation = label("fields.tags")
					found++
				}
			}
		}
		// An update whose body has another shape than the create's: the
		// create holds the account nested, the update sends it flat.
		if doc.HTTP[i].Name == "membersUpsert" && doc.HTTP[i].Body != nil {
			doc.HTTP[i].Body.Type = "client.Account"
			doc.HTTP[i].Body.Example = nil
			doc.HTTP[i].Validation = nil
			found++
		}
		if name := doc.HTTP[i].Name; name == "itemsEcho" || name == "genericEcho" {
			doc.HTTP[i].Body.Type = "client.LimitsForm"
			doc.HTTP[i].Body.Example = nil
			doc.HTTP[i].Validation = &tree
			doc.HTTP[i].Preparation = name == "genericEcho"
			found++
		}
	}
	if found != 8 {
		t.Fatal("fixture operations itemsEcho, genericEcho and membersUpsert, the uploadsProfile parts or its settings caption are missing")
	}
	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	source, err = manifest.Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	tools.Check(t, source, "", filepath.Join("testdata", "limits"))
}
