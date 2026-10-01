package contract

import (
	"slices"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/i18n"
	"github.com/weiloon1234/Foundry-Go/internal/identifier"
)

// PresentationKind is an optional public display hint. It never changes codecs,
// validation, authorization or storage. An empty kind leaves control selection
// to the consumer using the existing type, enum and rule descriptions.
type PresentationKind string

const (
	TextPresentation      PresentationKind = "text"
	MultilinePresentation PresentationKind = "multiline"
	PasswordPresentation  PresentationKind = "password"
	EmailPresentation     PresentationKind = "email"
	URLPresentation       PresentationKind = "url"
	MoneyPresentation     PresentationKind = "money"
	FilePresentation      PresentationKind = "file"
)

var presentationKinds = []PresentationKind{TextPresentation, MultilinePresentation, PasswordPresentation, EmailPresentation, URLPresentation, MoneyPresentation, FilePresentation}

// PresentationKinds returns the closed kind set in declaration order. Generated
// clients derive their kind type from it.
func PresentationKinds() []PresentationKind { return slices.Clone(presentationKinds) }

// Presentation contains public metadata only. LabelKey and HelpKey reference
// locale messages; there are deliberately no values, defaults or examples.
// Declare separate DTOs when input and output need different presentation.
type Presentation struct {
	Kind     PresentationKind `json:"kind,omitempty"`
	LabelKey i18n.MessageKey  `json:"label_key,omitempty"`
	HelpKey  i18n.MessageKey  `json:"help_key,omitempty"`
}

// MaxPresentationKeyBytes bounds each public translation key independently. It
// is the semantic message-key bound every key must also satisfy.
const MaxPresentationKeyBytes = identifier.MaxSemanticBytes

// Validate checks the closed kind set and bounded translation keys.
func (p Presentation) Validate() error {
	if p.Kind != "" && !slices.Contains(presentationKinds, p.Kind) {
		return invalidSchema()
	}
	for _, key := range []i18n.MessageKey{p.LabelKey, p.HelpKey} {
		if key != "" && key.Validate() != nil {
			return invalidSchema()
		}
	}
	return nil
}

// ValidateType checks a resolved JSON/URL value shape. Schema normalization
// resolves aliases before calling this method. Files use ValidateFile instead.
func (p Presentation) ValidateType(typ Type) error {
	if err := p.Validate(); err != nil {
		return err
	}
	switch p.Kind {
	case "":
		return nil
	case MoneyPresentation:
		if typ.Kind == StringKind && typ.Format == DecimalFormat {
			return nil
		}
	case TextPresentation, MultilinePresentation, PasswordPresentation, EmailPresentation, URLPresentation:
		if typ.Kind == StringKind && typ.Format == "" && len(typ.Cases) == 0 {
			return nil
		}
	}
	return invalidSchema()
}

// ValidateFile checks metadata on an actual multipart upload part.
func (p Presentation) ValidateFile() error {
	if err := p.Validate(); err != nil {
		return err
	}
	if p.Kind != "" && p.Kind != FilePresentation {
		return invalidSchema()
	}
	return nil
}

// ValidateSchema checks presentation of a complete JSON value, resolving its
// root aliases through the same schema compiler used by runtime codecs.
func (p Presentation) ValidateSchema(schema Schema) error {
	if p == (Presentation{}) {
		return nil
	}
	compiled, err := compileSchemaMode(schema, true)
	if err != nil {
		return err
	}
	typ := compiled.types[schema.Root]
	for typ.Kind == AliasKind {
		typ = compiled.types[typ.Element]
	}
	return p.ValidateType(typ)
}

// RejectPasswordOutput refuses an output schema whose root value can contain a
// property hinted as a password. Every output owner (HTTP responses and event
// streams, table rows, notification payloads, presence members and server
// events) calls it when a declaration registers, whether or not clients are
// exported; output names the owner's kind in the error.
func RejectPasswordOutput(schema Schema, output string) error {
	if PasswordTypes(schema.Types)[schema.Root] {
		return fault.New(fault.Invalid, "password presentation is input-only; it cannot be "+output+" output")
	}
	return nil
}

// PasswordTypes returns the types whose values can contain a property hinted as
// a password, following properties, elements, aliases and union variants,
// including recursive graphs. Only explicit password hints count; property
// names and persistence models are never scanned. Password hints are
// input-only, so output owners reject these types.
func PasswordTypes(types []Type) map[TypeID]bool {
	parents := make(map[TypeID][]TypeID)
	sensitive := make(map[TypeID]bool)
	var pending []TypeID
	for _, typ := range types {
		for _, p := range typ.Properties {
			parents[p.Type] = append(parents[p.Type], typ.ID)
			if p.Presentation.Kind == PasswordPresentation && !sensitive[typ.ID] {
				sensitive[typ.ID] = true
				pending = append(pending, typ.ID)
			}
		}
		if typ.Element != "" {
			parents[typ.Element] = append(parents[typ.Element], typ.ID)
		}
		for _, v := range typ.Variants {
			parents[v.Type] = append(parents[v.Type], typ.ID)
		}
	}
	for i := 0; i < len(pending); i++ {
		for _, parent := range parents[pending[i]] {
			if !sensitive[parent] {
				sensitive[parent] = true
				pending = append(pending, parent)
			}
		}
	}
	return sensitive
}
