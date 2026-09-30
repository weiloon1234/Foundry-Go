package contract

import "github.com/weiloon1234/Foundry-Go/i18n"

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

// Presentation contains public metadata only. LabelKey and HelpKey reference
// locale messages; there are deliberately no values, defaults or examples.
// Declare separate DTOs when input and output need different presentation.
type Presentation struct {
	Kind     PresentationKind `json:"kind,omitempty"`
	LabelKey i18n.MessageKey  `json:"label_key,omitempty"`
	HelpKey  i18n.MessageKey  `json:"help_key,omitempty"`
}

// MaxPresentationKeyBytes bounds each public translation key independently.
const MaxPresentationKeyBytes = 256

// Validate checks the closed kind set and bounded translation keys.
func (p Presentation) Validate() error {
	switch p.Kind {
	case "", TextPresentation, MultilinePresentation, PasswordPresentation, EmailPresentation, URLPresentation, MoneyPresentation, FilePresentation:
	default:
		return invalidSchema()
	}
	for _, key := range []i18n.MessageKey{p.LabelKey, p.HelpKey} {
		if key != "" && (len(key) > MaxPresentationKeyBytes || key.Validate() != nil) {
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
