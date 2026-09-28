package generate

import (
	"fmt"
	"go/ast"
	"go/types"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/i18n"
	"github.com/weiloon1234/Foundry-Go/i18n/message"
)

type messageOptions struct {
	key    string
	plural string
	kind   i18n.PluralKind
}

func discoverMessage(p *packageInput, spec *ast.TypeSpec, named *types.Named, args map[string]string) (dtoDeclaration, error) {
	d, err := discoverDTO(p, spec, named, nil)
	if err != nil {
		return d, err
	}
	for key := range args {
		if key != "key" && key != "plural" && key != "kind" {
			return d, p.diagnostic(spec.Pos(), "unknown message declaration option")
		}
	}
	options := &messageOptions{key: args["key"], plural: args["plural"], kind: i18n.PluralKind(args["kind"])}
	if i18n.MessageKey(options.key).Validate() != nil || options.plural != "" && !i18n.ParameterName(options.plural) {
		return d, p.diagnostic(spec.Pos(), "message requires a semantic key and valid plural parameter")
	}
	if options.plural != "" && options.kind == "" {
		options.kind = i18n.Cardinal
	}
	if options.plural == "" && options.kind != "" || options.kind != "" && options.kind != i18n.Cardinal && options.kind != i18n.Ordinal {
		return d, p.diagnostic(spec.Pos(), "message plural kind requires a parameter and cardinal or ordinal")
	}
	d.message = options
	return d, nil
}
func validateMessageSchema(p *packageInput, d dtoDeclaration) error {
	schema := contract.Schema{Root: dtoTypeID(d.typ)}
	for _, node := range d.nodes {
		if node.codec != nil {
			return p.diagnostic(node.position, "message parameters cannot use custom JSON contracts")
		}
		wire := node.wire
		if node.enum != nil {
			base, ok := node.enum.Underlying().(*types.Basic)
			if !ok || !dtoBasicType(base, &wire) {
				return p.diagnostic(node.position, "message enum must have a scalar wire type")
			}
		}
		schema.Types = append(schema.Types, wire)
	}
	_, err := message.Describe(i18n.MessageKey(d.message.key), schema, message.Options{Plural: d.message.plural, Kind: d.message.kind})
	if err != nil {
		return fmt.Errorf("%s: message requires required nonnullable text, boolean, integer or decimal parameters and a numeric plural argument", d.position)
	}
	return nil
}
func validateEnumLabels(prefix string) error {
	if prefix != "" && i18n.MessageKey(prefix).Validate() != nil {
		return fmt.Errorf("enum labels require a semantic message prefix")
	}
	return nil
}

func validateEnumCaseLabels(e enum) error {
	if e.labels == "" {
		return nil
	}
	seen := make(map[i18n.MessageKey]bool, len(e.values))
	for _, value := range e.values {
		key := i18n.MessageKey(e.labels + "." + snake(value.name))
		if key.Validate() != nil || seen[key] {
			return fmt.Errorf("enum label keys are invalid or repeated")
		}
		seen[key] = true
	}
	return nil
}
