package clientcontracts_test

import (
	"bytes"
	"context"
	"encoding/json"
	stdhttp "net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"foundry.test/consumer/clientcontracts"
	"foundry.test/consumer/internal/clientfixture"
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/contract/manifest"
	"github.com/weiloon1234/Foundry-Go/decimal"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/i18n"
	"github.com/weiloon1234/Foundry-Go/model"
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
			{Name: "owner_id", Type: "client.OwnerID", Required: true, Presentation: label("fields.prefs_owner_id")},
		}},
		contract.Type{ID: "client.OwnerID", Kind: contract.StringKind, Format: contract.UUIDFormat},
		contract.Type{ID: "client.OptionalOwnerID", Kind: contract.AliasKind, Element: "client.OwnerID"},
		contract.Type{ID: "client.NullableOwnerID", Kind: contract.AliasKind, Element: "client.OwnerID", Nullable: true},
		contract.Type{ID: "client.PointerOwnerID", Kind: contract.AliasKind, Element: "client.OwnerID", Nullable: true},
		contract.Type{ID: "client.CalendarDate", Kind: contract.StringKind, Format: contract.DateFormat},
		contract.Type{ID: "client.NullableDate", Kind: contract.AliasKind, Element: "client.CalendarDate", Nullable: true},
		contract.Type{ID: "client.WallTime", Kind: contract.StringKind, Format: contract.TimeFormat},
		contract.Type{ID: "client.LocalInstant", Kind: contract.StringKind, Format: contract.LocalDateTimeFormat},
		contract.Type{ID: "client.Duration", Kind: contract.StringKind, Format: contract.IntervalFormat},
		contract.Type{ID: "client.Payload", Kind: contract.StringKind, Format: contract.Base64Format},
		contract.Type{ID: "client.NullableDecimal", Kind: contract.AliasKind, Element: "github.com/weiloon1234/Foundry-Go/decimal.Decimal", Nullable: true},
		contract.Type{ID: "client.Slot", Kind: contract.ObjectKind, Properties: []contract.Property{
			{Name: "opens", Type: "string", Required: true, Presentation: label("fields.opens")},
		}},
		contract.Type{ID: "client.Slots", Kind: contract.ArrayKind, Element: "client.Slot"},
		contract.Type{ID: "client.Row", Kind: contract.ObjectKind, Properties: []contract.Property{
			{Name: "name", Type: "string", Required: true, Presentation: label("fields.row_name")},
			{Name: "slots", Type: "client.Slots", Required: true, Presentation: label("fields.slots")},
		}},
		contract.Type{ID: "client.Rows", Kind: contract.ArrayKind, Element: "client.Row"},
		contract.Type{ID: "client.WalletDetail", Kind: contract.ObjectKind, Properties: []contract.Property{
			{Name: "leaf", Type: "string", Required: true, Presentation: label("fields.wallet_leaf")},
		}},
		contract.Type{ID: "client.Wallet", Kind: contract.ObjectKind, Properties: []contract.Property{
			{Name: "address", Type: "string", Required: true, Presentation: label("fields.address")},
			{Name: "note", Type: "string", Required: true},
			{Name: "detail", Type: "client.WalletDetail"},
			{Name: "labels", Type: "client.Names", Presentation: label("fields.wallet_labels")},
			{Name: "toString", Type: "string", Presentation: label("fields.wallet_to_string")},
		}},
		contract.Type{ID: "client.RowMeta", Kind: contract.ObjectKind, Properties: []contract.Property{
			{Name: "note", Type: "string", Required: true, Presentation: label("fields.row_meta_note")},
		}},
		contract.Type{ID: "client.Link", Kind: contract.ObjectKind, Properties: []contract.Property{
			{Name: "role", Type: "string", Required: true, Presentation: label("fields.role")},
			{Name: "meta", Type: "client.RowMeta"},
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
			{Name: "owner_id", Type: "client.OwnerID", Required: true, Presentation: label("fields.owner_id")},
			{Name: "optional_owner_id", Type: "client.OptionalOwnerID", Presentation: label("fields.optional_owner_id")},
			{Name: "nullable_owner_id", Type: "client.NullableOwnerID", Required: true, Presentation: label("fields.nullable_owner_id")},
			{Name: "pointer_owner_id", Type: "client.PointerOwnerID", Presentation: label("fields.pointer_owner_id")},
			{Name: "required_owner_id", Type: "client.OptionalOwnerID", Presentation: label("fields.required_owner_id")},
			{Name: "birth_date", Type: "client.CalendarDate", Required: true, Presentation: label("fields.birth_date")},
			{Name: "nullable_date", Type: "client.NullableDate", Required: true, Presentation: label("fields.nullable_date")},
			{Name: "opens_at", Type: "client.WallTime", Required: true, Presentation: label("fields.opens_at")},
			{Name: "local_at", Type: "client.LocalInstant", Required: true, Presentation: label("fields.local_at")},
			{Name: "span", Type: "client.Duration", Required: true, Presentation: label("fields.span")},
			{Name: "raw_bytes", Type: "client.Payload", Required: true, Presentation: label("fields.raw_bytes")},
			{Name: "plain_decimal", Type: "github.com/weiloon1234/Foundry-Go/decimal.Decimal", Required: true, Presentation: label("fields.plain_decimal")},
			{Name: "optional_price", Type: "github.com/weiloon1234/Foundry-Go/decimal.Decimal", Presentation: label("fields.optional_price")},
			{Name: "nullable_price", Type: "client.NullableDecimal", Required: true, Presentation: label("fields.nullable_price")},
			{Name: "step_price", Type: "github.com/weiloon1234/Foundry-Go/decimal.Decimal", Required: true, Presentation: label("fields.step_price")},
			{Name: "tight_price", Type: "github.com/weiloon1234/Foundry-Go/decimal.Decimal", Required: true, Presentation: label("fields.tight_price")},
			{Name: "whole_price", Type: "github.com/weiloon1234/Foundry-Go/decimal.Decimal", Required: true, Presentation: label("fields.whole_price")},
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
		limitField("pointer_owner_id", wrap(validation.PointerKind, limitLeaf("foundry.non_empty", map[string]any{"empty_kind": "value"}))),
		limitField("required_owner_id", limitLeaf("foundry.required", map[string]any{"empty_kind": "value"})),
		limitField("optional_price", wrap(validation.OptionalKind, limitLeaf("foundry.decimal_places", map[string]any{"places": 3}))),
		limitField("step_price", limitLeaf("foundry.multiple_of", map[string]any{"divisor": "0.25"})),
		limitField("tight_price", limitLeaf("foundry.decimal_places", map[string]any{"places": 4}), limitLeaf("foundry.multiple_of", map[string]any{"divisor": "0.25"})),
		limitField("whole_price", limitLeaf("foundry.decimal_places", map[string]any{"places": 0})),
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

type decimalCodecGoldenEntry struct {
	Input  string `json:"input"`
	Valid  bool   `json:"valid"`
	Places *int   `json:"places,omitempty"`
}

// TestDecimalCodecGolden runs a table covering Go's decimal codec grammar
// (signs, leading and trailing zeros, bare points, exponents, whitespace,
// non-digits, and MaxDigits boundaries) through decimal.Parse and
// Decimal.UnmarshalJSON, keeping the golden table checked by the TypeScript
// fixture in lockstep with Go. Set FOUNDRY_UPDATE_DECIMAL_GOLDEN=1 to refresh.
func TestDecimalCodecGolden(t *testing.T) {
	inputs := []string{
		"",
		"+", "-", "++1", "+-1", "--1", "-+0", "1-2", "1+2",
		".", ".0", ".5", "+.5", "-.5",
		"0.", "1.", "+1.", "-1.", "1.2.3", "1..2", "..",
		"0", "+0", "-0", "000", "+000", "-000",
		"0.0", "+0.0", "-0.0", "000.000", "+000.000", "-000.000",
		"01", "00042", "+00042", "-00042", "00042.1000", "-000.0010",
		"1.0", "1.00", "1.50", "1.5000", "-12.3400", "0.0100", "0.001000",
		"1", "42", "-42", "+42", "0.1", "0.01", "3.001", "-0.0001",
		"123456789012345678901234567890.12345678901234567890",
		"1e0", "1e3", "1E3", "1e+3", "1e-3", "1.5e2", "0e0", "-1.0E-1",
		" ", "   ", " 1", "1 ", " 1 ", "\t1", "1\n", "1\r", "1 2", "1 .2", "1. 2", "\u00a01",
		"abc", "1a", "a1", "1.2a",
		"NaN", "nan", "Infinity", "-Infinity", "+Infinity", "inf",
		"1_000", "1,000", "1,5", "$10", "10%", "１２", "1.２", "٠", "1\x00", "1.2\x00",
		strings.Repeat("9", decimal.MaxDigits),
		strings.Repeat("9", decimal.MaxDigits+1),
		strings.Repeat("0", decimal.MaxDigits),
		strings.Repeat("0", decimal.MaxDigits+1),
		"+" + strings.Repeat("9", decimal.MaxDigits),
		"-" + strings.Repeat("9", decimal.MaxDigits),
		"+" + strings.Repeat("9", decimal.MaxDigits+1),
		"0." + strings.Repeat("0", decimal.MaxDigits-1),
		"0." + strings.Repeat("0", decimal.MaxDigits-2) + "1",
		"+0." + strings.Repeat("0", decimal.MaxDigits-2) + "1",
		"-0." + strings.Repeat("0", decimal.MaxDigits-2) + "1",
		"0." + strings.Repeat("0", decimal.MaxDigits-1) + "1",
		"+0." + strings.Repeat("0", decimal.MaxDigits-1) + "1",
		"0000." + strings.Repeat("0", decimal.MaxDigits-4),
		"0000." + strings.Repeat("0", decimal.MaxDigits-3),
		"1." + strings.Repeat("0", decimal.MaxDigits-2) + "5",
		"1." + strings.Repeat("0", decimal.MaxDigits-1) + "0",
		"123." + strings.Repeat("4", decimal.MaxDigits-3),
		"123." + strings.Repeat("4", decimal.MaxDigits-2),
	}
	entries := make([]decimalCodecGoldenEntry, 0, len(inputs))
	for _, input := range inputs {
		parsed, parseErr := decimal.Parse(input)
		wire, err := json.Marshal(input)
		if err != nil {
			t.Fatal(err)
		}
		var unmarshaled decimal.Decimal
		unmarshalErr := json.Unmarshal(wire, &unmarshaled)
		if (parseErr == nil) != (unmarshalErr == nil) {
			t.Fatalf("Parse vs UnmarshalJSON disagreed for %q: %v vs %v", input, parseErr, unmarshalErr)
		}
		entry := decimalCodecGoldenEntry{Input: input, Valid: parseErr == nil}
		if parseErr == nil {
			if parsed != unmarshaled {
				t.Fatalf("Parse vs UnmarshalJSON value disagreed for %q: %v vs %v", input, parsed, unmarshaled)
			}
			scale := parsed.Scale()
			entry.Places = &scale
		}
		entries = append(entries, entry)
	}
	got, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	goldenPath := filepath.Join("testdata", "limits", "decimal_codec.golden.json")
	if os.Getenv("FOUNDRY_UPDATE_DECIMAL_GOLDEN") == "1" {
		if err := os.WriteFile(goldenPath, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden %s: %v (run with FOUNDRY_UPDATE_DECIMAL_GOLDEN=1 to write)", goldenPath, err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("decimal codec golden %s drifted from Go codec; run with FOUNDRY_UPDATE_DECIMAL_GOLDEN=1 to refresh", goldenPath)
	}
}

// TestClientNonEmptyContractEndToEnd exercises real generated DTOs, a real
// generated query descriptor, a real HTTP router/server, manifest export,
// TypeScript generation, strict tsc compilation, and Node runtime execution for
// Go-declared NonEmpty codecs: omitzero ID, required TextQuery decimal sent
// empty, list rows (non-nullable value.List vs nullable slice), PATCH fields
// (including an Optional field with an unconditional Required rule),
// password.Plaintext with no rule, all newly covered framework non-empty
// codecs, and an application custom codec declaring NonEmpty vs an opaque one.
func TestClientNonEmptyContractEndToEnd(t *testing.T) {
	tools := clientfixture.Load(t)

	createRoute := foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "codec.create", Method: foundryhttp.POST, Access: foundryhttp.Public}, foundryhttp.StaticPath("/codec/create"))
	createEndpoint := foundryhttp.DefineEndpoint(createRoute, foundryhttp.EmptyQuery(), clientcontracts.CodecFormCreateBody(), foundryhttp.EmptyResponse(204))

	patchRoute := foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "codec.patch", Method: foundryhttp.PATCH, Access: foundryhttp.Public}, foundryhttp.StaticPath("/codec/patch"))
	patchBase := foundryhttp.DefineEndpoint(patchRoute, foundryhttp.EmptyQuery(), clientcontracts.CodecFormPatchBody(), foundryhttp.EmptyResponse(204))
	patchEndpoint := foundryhttp.ValidateBody(patchBase, validation.Rules(clientcontracts.CodecFormPatchValidationFields().ForcedOptionalID, validation.Required[model.ID[models.User]]()))

	quoteRoute := foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "codec.quote", Method: foundryhttp.GET, Access: foundryhttp.Public}, foundryhttp.StaticPath("/codec/quote"))
	quoteEndpoint := foundryhttp.DefineEndpoint(quoteRoute, clientcontracts.DecimalAmountQueryDescriptor(), foundryhttp.EmptyBody(), foundryhttp.EmptyResponse(204))

	router, err := foundryhttp.NewRouter(
		createEndpoint.Handle(func(context.Context, foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, clientcontracts.CodecFormCreate]) (foundryhttp.NoContent, error) {
			return foundryhttp.NoContent{}, nil
		}),
		patchEndpoint.Handle(func(context.Context, foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, clientcontracts.CodecFormPatch]) (foundryhttp.NoContent, error) {
			return foundryhttp.NoContent{}, nil
		}),
		quoteEndpoint.Handle(func(context.Context, foundryhttp.Input[foundryhttp.NoPath, clientcontracts.DecimalAmountQuery, foundryhttp.NoBody]) (foundryhttp.NoContent, error) {
			return foundryhttp.NoContent{}, nil
		}),
	)
	if err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(router)
	t.Cleanup(server.Close)

	// Confirm the real Go server enforces the exact wire contract:
	// 1. Required TextQuery decimal sent as an empty query value (?amount=&sku=SKU-1) -> 400.
	resp, err := server.Client().Get(server.URL + "/codec/quote?amount=&sku=SKU-1")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != stdhttp.StatusBadRequest {
		t.Fatalf("empty required TextQuery decimal status = %d, want 400", resp.StatusCode)
	}
	// 2. Valid TextQuery decimal and AppSKU -> 204.
	resp, err = server.Client().Get(server.URL + "/codec/quote?amount=12.50&sku=SKU-1")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != stdhttp.StatusNoContent {
		t.Fatalf("valid TextQuery decimal status = %d, want 204", resp.StatusCode)
	}
	// 3. POST omitting omitzero_id succeeds (204); POST with empty password or empty sku rejects (400).
	validCreate := `{
		"owner_id":"0193fd8c-2075-7000-8000-000000000001",
		"password":"correct horse battery staple",
		"totp":"123456",
		"recovery":"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
		"reset_token":"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
		"contact":"alice@example.com",
		"asset_key":"uploads/report.pdf",
		"currency":"USD",
		"sku":"SKU-100",
		"opaque_note":"",
		"required_ids":["0193fd8c-2075-7000-8000-000000000002"],
		"nullable_ids":null,
		"lines":[{"item_id":"0193fd8c-2075-7000-8000-000000000003","amount":"9.95"}]
	}`
	resp, err = server.Client().Post(server.URL+"/codec/create", "application/json", strings.NewReader(validCreate))
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != stdhttp.StatusNoContent {
		t.Fatalf("valid create omitting omitzero_id status = %d, want 204", resp.StatusCode)
	}
	for name, badBody := range map[string]string{
		"empty_password": strings.Replace(validCreate, `"correct horse battery staple"`, `""`, 1),
		"empty_sku":      strings.Replace(validCreate, `"SKU-100"`, `""`, 1),
		"empty_row_id":   strings.Replace(validCreate, `"0193fd8c-2075-7000-8000-000000000002"`, `""`, 1),
	} {
		resp, err = server.Client().Post(server.URL+"/codec/create", "application/json", strings.NewReader(badBody))
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != stdhttp.StatusBadRequest {
			t.Fatalf("%s status = %d, want 400", name, resp.StatusCode)
		}
	}
	// 4. PATCH with empty owner_id rejects (400); PATCH omitting forced_optional_id rejects (422).
	patchReq := func(payload string) int {
		req, err := stdhttp.NewRequest(stdhttp.MethodPatch, server.URL+"/codec/patch", strings.NewReader(payload))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		r, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = r.Body.Close()
		return r.StatusCode
	}
	if code := patchReq(`{"owner_id":"","pointer_owner_id":null,"forced_optional_id":"0193fd8c-2075-7000-8000-000000000001"}`); code != stdhttp.StatusBadRequest {
		t.Fatalf("PATCH empty owner_id status = %d, want 400", code)
	}
	if code := patchReq(`{"owner_id":"0193fd8c-2075-7000-8000-000000000001","pointer_owner_id":null}`); code != stdhttp.StatusUnprocessableEntity {
		t.Fatalf("PATCH missing forced_optional_id status = %d, want 422", code)
	}

	source, err := manifest.Build(t.Context(), manifest.Sources{HTTP: router})
	if err != nil {
		t.Fatal(err)
	}
	tools.Check(t, source, server.URL, filepath.Join("testdata", "codec-required"))
}
