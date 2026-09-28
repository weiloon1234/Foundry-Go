// Package validation provides typed rules and owned validation diagnostics.
// Generated declarations compose these same rules; transport adapters own
// decoding, presence discovery and HTTP error mapping.
package validation

import (
	"encoding/json"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/i18n"
	"github.com/weiloon1234/Foundry-Go/internal/identifier"
	"github.com/weiloon1234/Foundry-Go/internal/jsonwire"
)

// RuleID identifies a rule independently of its public message. Built-in IDs
// use the reserved foundry namespace; application rules use semantic IDs.
type RuleID string

type Kind string

const (
	LeafKind     Kind = "rule"
	AllKind      Kind = "all"
	BailKind     Kind = "bail"
	FieldKind    Kind = "field"
	CompareKind  Kind = "compare"
	OptionalKind Kind = "optional"
	NullableKind Kind = "nullable"
	EachKind     Kind = "each"
	WhenKind     Kind = "when"
	UnlessKind   Kind = "unless"
	PointerKind  Kind = "pointer"
)

// Parameter is declaration metadata, never a received field value. Values are
// bounded, valid JSON and copied at construction and inspection boundaries.
type Parameter struct {
	Name  string          `json:"name"`
	Value json.RawMessage `json:"value"`
}

// Spec is a custom rule's public declaration. Message contains approved static
// public text; an execution error's text is never used as a validation message.
type Spec struct {
	ID          RuleID              `json:"id"`
	Message     string              `json:"message"`
	Parameters  []Parameter         `json:"parameters,omitempty"`
	Translation *i18n.MessageRecipe `json:"translation,omitempty"`
}

// Description is an owned rule tree. Kind defines composition; Spec describes
// a leaf. Field carries a declared wire name; Label is its optional static
// display name. OtherField/OtherLabel describe a comparison target. ServerOnly
// marks rules whose
// behavior cannot be promised by a browser validator.
type Description struct {
	Kind          Kind            `json:"kind"`
	Field         string          `json:"field,omitempty"`
	OtherField    string          `json:"other_field,omitempty"`
	Label         string          `json:"label,omitempty"`
	OtherLabel    string          `json:"other_label,omitempty"`
	LabelKey      i18n.MessageKey `json:"label_key,omitempty"`
	OtherLabelKey i18n.MessageKey `json:"other_label_key,omitempty"`
	Spec          *Spec           `json:"spec,omitempty"`
	ServerOnly    bool            `json:"server_only,omitempty"`
	Children      []Description   `json:"children,omitempty"`
}

const maxDeclarationText = 16384
const maxParameters = 64
const maxRuleNodes = 4096
const maxDescriptionBytes = 1 << 20

func infoBytes(info Description) int {
	size := len(info.Kind) + len(info.Field) + len(info.OtherField) + len(info.Label) + len(info.OtherLabel) + len(info.LabelKey) + len(info.OtherLabelKey)
	if info.Spec != nil {
		size += len(info.Spec.ID) + len(info.Spec.Message)
		if info.Spec.Translation != nil {
			data, _ := json.Marshal(info.Spec.Translation)
			size += len(data)
		}
		for _, p := range info.Spec.Parameters {
			size += len(p.Name) + len(p.Value)
		}
	}
	return size
}

func validText(text string, empty bool) bool {
	return (empty || text != "") && len(text) <= maxDeclarationText && utf8.ValidString(text) && !strings.ContainsRune(text, 0)
}

func copySpec(spec Spec) (Spec, error) {
	if !identifier.Semantic(string(spec.ID)) || !validText(spec.Message, false) || len(spec.Parameters) > maxParameters {
		return Spec{}, invalid("invalid validation rule declaration")
	}
	result := spec
	result.Parameters = slices.Clone(spec.Parameters)
	names := make(map[string]bool, len(spec.Parameters))
	for i, parameter := range spec.Parameters {
		if !identifier.Semantic(parameter.Name) || names[parameter.Name] {
			return Spec{}, invalid("invalid or duplicate validation rule parameter")
		}
		names[parameter.Name] = true
		if _, err := jsonwire.Decode(parameter.Value, jsonwire.Limits{Bytes: maxDeclarationText, Depth: jsonwire.MaxDepth, Nodes: maxRuleNodes}); err != nil {
			return Spec{}, invalid("invalid validation rule parameter value")
		}
		result.Parameters[i].Value = slices.Clone(parameter.Value)
	}
	if spec.Translation != nil {
		prepared, err := spec.Translation.Prepare()
		if err != nil {
			return Spec{}, err
		}
		if err := validateLabelParameters(prepared.Description().Definition); err != nil {
			return Spec{}, err
		}
		owned := prepared.Description()
		result.Translation = &owned
	}
	return result, nil
}

func cloneDescription(info Description) Description {
	if info.Spec != nil {
		spec := *info.Spec
		spec.Parameters = slices.Clone(spec.Parameters)
		for i := range spec.Parameters {
			spec.Parameters[i].Value = slices.Clone(spec.Parameters[i].Value)
		}
		if spec.Translation != nil {
			recipe := cloneRecipe(*spec.Translation)
			spec.Translation = &recipe
		}
		info.Spec = &spec
	}
	info.Children = slices.Clone(info.Children)
	for i := range info.Children {
		info.Children[i] = cloneDescription(info.Children[i])
	}
	return info
}

func invalid(message string) error { return fault.New(fault.Invalid, message) }

func cloneRecipe(recipe i18n.MessageRecipe) i18n.MessageRecipe {
	recipe.Definition.Parameters = slices.Clone(recipe.Definition.Parameters)
	recipe.Arguments = slices.Clone(recipe.Arguments)
	owned := make(map[i18n.PluralForm]string, len(recipe.Fallback))
	for form, text := range recipe.Fallback {
		owned[form] = text
	}
	recipe.Fallback = owned
	return recipe
}
