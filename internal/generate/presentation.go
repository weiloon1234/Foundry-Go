package generate

import (
	"fmt"
	"go/types"
	"strings"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/i18n"
)

// Client tags add public presentation to the existing winning transport field;
// they never discover fields or infer semantics from a Go/JSON name.
func parsePresentation(raw string) (contract.Presentation, error) {
	var result contract.Presentation
	seen := map[string]bool{}
	if raw == "" {
		return result, nil
	}
	for _, entry := range strings.Split(raw, ",") {
		key, value, ok := strings.Cut(entry, "=")
		if !ok || value == "" || seen[key] {
			return result, fmt.Errorf("invalid or repeated client presentation option")
		}
		seen[key] = true
		switch key {
		case "kind":
			result.Kind = contract.PresentationKind(value)
		case "label":
			result.LabelKey = i18n.MessageKey(value)
		case "help":
			result.HelpKey = i18n.MessageKey(value)
		default:
			return result, fmt.Errorf("unknown client presentation option")
		}
	}
	if result.Validate() != nil {
		return result, fmt.Errorf("invalid client presentation kind or message key")
	}
	return result, nil
}

func propertyPresentation(root types.Type, index []int) (contract.Presentation, error) {
	for i, at := range index {
		if pointer, ok := types.Unalias(root).(*types.Pointer); ok {
			root = pointer.Elem()
		}
		structure := root.Underlying().(*types.Struct)
		if i == len(index)-1 {
			tags, err := parseTags(structure.Tag(at))
			if err != nil {
				return contract.Presentation{}, err
			}
			presentation, err := parsePresentation(tags["client"])
			if err != nil {
				return presentation, err
			}
			// A password input must never render as visible text, so its kind
			// is declared, not defaulted.
			if presentation.Kind != contract.PasswordPresentation && plaintextPassword(structure.Field(at).Type()) {
				return presentation, fmt.Errorf("a password.Plaintext field requires client:\"kind=password\"")
			}
			return presentation, nil
		}
		root = structure.Field(at).Type()
	}
	return contract.Presentation{}, nil
}

func validatePresentationGraph(graph *dtoGraph) error {
	nodes := make(map[contract.TypeID]*dtoNode, len(graph.nodes))
	for _, node := range graph.nodes {
		nodes[node.wire.ID] = node
	}
	for _, node := range graph.nodes {
		for _, property := range node.wire.Properties {
			if property.Presentation.Kind == "" {
				continue
			}
			target := nodes[property.Type]
			for steps := 0; target != nil && target.wire.Kind == contract.AliasKind && steps < len(nodes); steps++ {
				target = nodes[target.wire.Element]
			}
			// Enum values always carry cases, and every kind requires a plain
			// string, decimal or file, so any kind contradicts an enum. Custom
			// codecs and generic parameters are checked by the concrete schema
			// compiler; their representations are not guessed here.
			if target != nil && (target.enum != nil || target.wire.Kind != "" && property.Presentation.ValidateType(target.wire) != nil) {
				return fmt.Errorf("client presentation on %s.%s contradicts its field codec", node.wire.ID, property.Name)
			}
		}
	}
	return nil
}

// transportPresentation checks a path, query, form or multipart text field's
// hint when generation knows its representation: plain scalars (or their
// repeated elements) and declared enums. Other codecs, including files, are
// checked at registration.
func (p *packageInput) transportPresentation(presentation contract.Presentation, typ types.Type) error {
	if presentation.Kind == "" {
		return nil
	}
	if slice, ok := types.Unalias(typ).(*types.Slice); ok {
		typ = slice.Elem()
	}
	var wire contract.Type
	switch t := types.Unalias(typ).(type) {
	case *types.Named:
		if p.enumTypes[t] || hasEnumDescriptor(t) {
			return fmt.Errorf("client presentation contradicts its enum codec")
		}
		return nil
	case *types.Basic:
		switch info := t.Info(); {
		case info&types.IsString != 0:
			wire.Kind = contract.StringKind
		case info&types.IsInteger != 0:
			wire.Kind = contract.IntegerKind
		case info&types.IsFloat != 0:
			wire.Kind = contract.NumberKind
		case info&types.IsBoolean != 0:
			wire.Kind = contract.BooleanKind
		default:
			return nil
		}
	default:
		return nil
	}
	if presentation.ValidateType(wire) != nil {
		return fmt.Errorf("client presentation contradicts its field codec")
	}
	return nil
}

func (e *emitter) presentation(value contract.Presentation) string {
	c := e.useNamed(framework+"/contract", "foundrycontract")
	return fmt.Sprintf("%s.Presentation{Kind:%q,LabelKey:%q,HelpKey:%q}", c, value.Kind, value.LabelKey, value.HelpKey)
}

func (e *emitter) presentationMethod(value contract.Presentation) string {
	if value == (contract.Presentation{}) {
		return ""
	}
	return ".WithPresentation(" + e.presentation(value) + ")"
}

// plaintextPassword reports whether a field holds password input: Foundry's
// password.Plaintext, possibly behind a pointer or value.Optional/Nullable.
func plaintextPassword(typ types.Type) bool {
	for depth := 0; depth < 8; depth++ {
		if pointer, ok := types.Unalias(typ).(*types.Pointer); ok {
			typ = pointer.Elem()
			continue
		}
		named, ok := types.Unalias(typ).(*types.Named)
		if !ok || named.Obj().Pkg() == nil {
			return false
		}
		switch named.Obj().Pkg().Path() + "." + named.Obj().Name() {
		case "github.com/weiloon1234/Foundry-Go/auth/password.Plaintext":
			return true
		case "github.com/weiloon1234/Foundry-Go/value.Optional", "github.com/weiloon1234/Foundry-Go/value.Nullable":
			if named.TypeArgs().Len() != 1 {
				return false
			}
			typ = named.TypeArgs().At(0)
		default:
			return false
		}
	}
	return false
}
