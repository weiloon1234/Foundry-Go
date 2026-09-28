package generate

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"reflect"
	"strings"
	"unicode/utf8"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/internal/jsonwire"
)

type unionVariant struct {
	name, tag string
	typ       types.Type
	position  token.Pos
}
type unionDeclaration struct {
	name, discriminator string
	typ                 *types.Named
	variants            []unionVariant
	position            token.Position
	dtoGraph
}

func discoverUnion(p *packageInput, spec *ast.TypeSpec, marker *types.Named, args map[string]string) (unionDeclaration, error) {
	result := unionDeclaration{name: args["name"], discriminator: args["discriminator"], position: p.fset.Position(spec.Pos())}
	fail := func(message string) (unionDeclaration, error) { return result, p.diagnostic(spec.Pos(), message) }
	for option := range args {
		if option != "name" && option != "discriminator" {
			return fail("unsupported union option " + option)
		}
	}
	if !token.IsIdentifier(result.name) || !token.IsExported(result.name) {
		return fail("union name must be an exported Go identifier")
	}
	if !unionText(result.discriminator) {
		return fail("union requires a valid discriminator name")
	}
	structure, ok := marker.Underlying().(*types.Struct)
	if !ok || structure.NumFields() == 0 || structure.NumFields() > jsonwire.MaxNodes {
		return fail("union declaration requires a nonempty struct of variant payloads")
	}
	object, ok := p.types.Scope().Lookup(result.name).(*types.TypeName)
	if !ok {
		return fail("missing generated union declaration")
	}
	result.typ, ok = object.Type().(*types.Named)
	if !ok {
		return fail("invalid generated union declaration")
	}
	tags := make(map[string]bool)
	reserved := map[string]bool{"Kind": true, "IsZero": true, "MarshalJSON": true, "UnmarshalJSON": true, "JSONContract": true}
	for i := 0; i < structure.NumFields(); i++ {
		field := structure.Field(i)
		if !field.Exported() || field.Embedded() || reserved[field.Name()] {
			return fail("union variants require exported nonembedded fields with distinct accessor names")
		}
		tag, exists := reflect.StructTag(structure.Tag(i)).Lookup("union")
		if !exists || !unionText(tag) || tags[tag] {
			return fail("union variants require unique nonempty union tags")
		}
		tags[tag] = true
		named, ok := types.Unalias(field.Type()).(*types.Named)
		if !ok {
			return fail("union variant payload must be a named concrete DTO struct")
		}
		if _, ok := named.Underlying().(*types.Struct); !ok || hasTypeParameter(named) || !named.Obj().Exported() || customJSONShape(named) {
			return fail("union variant payload must be an ordinary concrete DTO struct")
		}
		result.variants = append(result.variants, unionVariant{name: field.Name(), tag: tag, typ: canonicalDTOInstantiation(named), position: field.Pos()})
	}
	return result, nil
}

func unionText(text string) bool {
	return text != "" && utf8.ValidString(text) && !strings.ContainsFunc(text, func(r rune) bool { return r < ' ' || r == 127 })
}

func unionSymbols(d unionDeclaration) []string {
	symbols := []string{d.name, d.name + "Kind", d.name + "JSON", "Match" + d.name, "foundry" + d.name + "Contract", "build" + d.name + "Contract"}
	for _, variant := range d.variants {
		symbols = append(symbols, d.name+"From"+variant.name, d.name+variant.name+"Kind")
	}
	return symbols
}

// Native union stubs enter the same graph discovery as normal DTO fields. Local
// recursion is represented by edges, never recursive descriptor-factory calls.
func (p *packageInput) unionNode(typ types.Type) (*unionDeclaration, bool) {
	named, ok := types.Unalias(typ).(*types.Named)
	if !ok {
		return nil, false
	}
	declaration, ok := p.unionTypes[named]
	return declaration, ok
}

func validateUnionGraph(graph *dtoGraph) error {
	nodes := make(map[contract.TypeID]*dtoNode, len(graph.nodes))
	for _, node := range graph.nodes {
		nodes[node.wire.ID] = node
	}
	for _, node := range graph.nodes {
		for _, variant := range node.wire.Variants {
			target := nodes[variant.Type]
			if target == nil || target.wire.Kind != contract.ObjectKind || target.wire.Nullable {
				return fmt.Errorf("union variant requires an ordinary nonnullable object payload")
			}
			for _, property := range target.wire.Properties {
				if property.Name == node.wire.Discriminator {
					return fmt.Errorf("union payload property collides with discriminator %q", node.wire.Discriminator)
				}
			}
		}
	}
	return nil
}
