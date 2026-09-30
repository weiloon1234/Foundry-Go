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
			return parsePresentation(tags["client"])
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
			// Custom codecs and generic parameters are checked by the concrete
			// schema compiler; their representations are not guessed here.
			if target != nil && target.wire.Kind != "" && property.Presentation.ValidateType(target.wire) != nil {
				return fmt.Errorf("client presentation contradicts its field codec")
			}
		}
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
