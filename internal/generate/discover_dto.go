package generate

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"reflect"
	"sort"
	"strings"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/internal/gotype"
	"github.com/weiloon1234/Foundry-Go/internal/jsonshape"
	"github.com/weiloon1234/Foundry-Go/internal/jsonwire"
	"github.com/weiloon1234/Foundry-Go/value"
)

type dtoDeclaration struct {
	name      string
	typ       *types.Named
	position  token.Position
	projected bool
	message   *messageOptions
	dtoGraph
}

// dtoGraph is shared by complete JSON DTOs and explicit JSON multipart parts.
type dtoGraph struct {
	nodes      []*dtoNode
	properties []jsonshape.Property[types.Type]
}

type dtoNode struct {
	typ      types.Type
	wire     contract.Type
	enum     *types.Named
	codec    *types.Named
	key      *dtoMapKey
	position token.Pos
}

func discoverDTO(p *packageInput, spec *ast.TypeSpec, named *types.Named, args map[string]string) (dtoDeclaration, error) {
	if named.TypeParams().Len() != 0 {
		arguments := make([]types.Type, named.TypeParams().Len())
		for i := range arguments {
			arguments[i] = named.TypeParams().At(i)
		}
		instantiated, err := types.Instantiate(nil, named, arguments, false)
		if err != nil {
			return dtoDeclaration{}, p.diagnostic(spec.Pos(), "invalid generic DTO declaration")
		}
		named = instantiated.(*types.Named)
	}
	declaration := dtoDeclaration{name: spec.Name.Name, typ: named, position: p.fset.Position(spec.Pos())}
	if len(args) != 0 {
		return declaration, p.diagnostic(spec.Pos(), "DTO declarations do not accept options")
	}
	if _, ok := named.Underlying().(*types.Struct); !ok {
		return declaration, p.diagnostic(spec.Pos(), "DTO declaration must be a struct")
	}
	return declaration, nil
}

func dtoTypeID(typ types.Type) contract.TypeID {
	typ = types.Unalias(typ)
	if named, ok := typ.(*types.Named); ok {
		if id, ok := dtoNamedIdentity(named); ok {
			return contract.TypeID(id)
		}
	}
	text := types.TypeString(typ, func(pkg *types.Package) string { return pkg.Path() })
	switch types.Unalias(typ).(type) {
	case *types.Named, *types.Basic:
		if !strings.ContainsAny(text, " \t\r\n") {
			return contract.TypeID(text)
		}
	}
	// Anonymous structs and instantiated composite types can contain tag text,
	// whitespace and punctuation. Stable identities need no platform path escape.
	return contract.TypeID(gotype.Identity(text, false))
}

func resolveDTOSchema(p *packageInput, declaration *dtoDeclaration, models map[*types.Named]bool) error {
	return resolveJSONGraph(p, &declaration.dtoGraph, declaration.typ, declaration.typ.Obj().Pos(), models, false)
}

// resolveJSONGraph owns all JSON shape discovery. Only a complete DTO root is
// required to retain native struct fields; explicit JSON part values may use
// their existing typed JSONContract, including a declared scalar or collection.
func resolveJSONGraph(p *packageInput, graph *dtoGraph, root types.Type, position token.Pos, models map[*types.Named]bool, allowRootCodec bool) error {
	seen := make(map[contract.TypeID]*dtoNode)
	add := func(typ types.Type, position token.Pos) (contract.TypeID, error) {
		typ = canonicalDTOInstantiation(types.Unalias(typ))
		id := dtoTypeID(typ)
		if seen[id] != nil {
			return id, nil
		}
		if len(graph.nodes) >= jsonwire.MaxNodes {
			return "", fmt.Errorf("DTO graph exceeds its resource bound")
		}
		node := &dtoNode{typ: typ, wire: contract.Type{ID: id}, position: position}
		seen[id] = node
		graph.nodes = append(graph.nodes, node)
		return id, nil
	}
	if _, err := add(root, position); err != nil {
		return err
	}
	propertyCount := 0
	for i := 0; i < len(graph.nodes); i++ {
		node := graph.nodes[i]
		if node.typ == nil {
			continue
		} // Complete synthetic quoted field node.
		typ := types.Unalias(node.typ)
		if _, parameter := typ.(*types.TypeParam); parameter {
			continue
		}
		fail := func(message string) error { return p.diagnostic(node.position, message) }
		if pointer, ok := typ.(*types.Pointer); ok {
			id, err := add(pointer.Elem(), node.position)
			if err != nil {
				return fail(err.Error())
			}
			node.wire.Kind, node.wire.Element, node.wire.Nullable = contract.AliasKind, id, true
			continue
		}
		if declaration, ok := p.unionNode(typ); ok {
			node.wire.Kind = contract.UnionKind
			node.wire.Discriminator = declaration.discriminator
			for _, variant := range declaration.variants {
				id, err := add(variant.typ, variant.position)
				if err != nil {
					return fail(err.Error())
				}
				node.wire.Variants = append(node.wire.Variants, contract.Variant{Tag: variant.tag, Type: id})
			}
			continue
		}
		if named, ok := typ.(*types.Named); ok {
			if models[named] || hasDTOModelIdentity(named) {
				return fail("persistence models cannot become DTO fields; map stored or getter values into an explicit response struct")
			}
			if isNamed(named, framework+"/value", "Optional") || isNamed(named, framework+"/value", "Nullable") {
				inner := named.TypeArgs().At(0)
				id, err := add(inner, node.position)
				if err != nil {
					return fail(err.Error())
				}
				node.wire.Kind, node.wire.Element = contract.AliasKind, id
				_, nullable := jsonWrapper(inner, "Nullable")
				node.wire.Nullable = named.Obj().Name() == "Nullable" || nullable
				continue
			}
			if isNamed(named, framework+"/model", "ID") {
				node.wire.Kind, node.wire.Format = contract.StringKind, contract.UUIDFormat
				continue
			}
			if isNamed(named, "encoding/json", "Number") {
				node.wire.Kind = contract.NumberKind
				continue
			}
			if isNamed(named, "encoding/json", "RawMessage") {
				node.wire.Kind, node.wire.Nullable = contract.DynamicKind, true
				continue
			}
			if format := dtoScalarFormat(named); format != "" {
				node.wire.Kind, node.wire.Format = contract.StringKind, format
				continue
			}
			if explicit, diagnostic := customDTOContract(named); diagnostic != "" {
				return fail(diagnostic)
			} else if explicit {
				if types.Identical(typ, root) && !allowRootCodec {
					return fail("DTO roots retain ordinary struct fields; declare custom JSON values as typed fields or use their explicit descriptor")
				}
				node.codec = named
				continue
			}
			if p.enumTypes[named] || hasEnumDescriptor(named) {
				node.enum = named
				continue
			}
		}
		if customJSONShape(typ) {
			return fail("custom JSON/text codecs require an explicit transport contract; their wire shape cannot be inferred from Go fields")
		}
		switch shape := typ.Underlying().(type) {
		case *types.Basic:
			if !dtoBasicType(shape, &node.wire) {
				return fail("unsupported DTO scalar")
			}
		case *types.Struct:
			embeddedModel, err := dtoEmbedsModel(typ, models)
			if err != nil {
				return fail(err.Error())
			}
			if embeddedModel {
				return fail("persistence models cannot be embedded into DTOs; declare response fields explicitly")
			}
			node.wire.Kind = contract.ObjectKind
			for fieldIndex := 0; fieldIndex < shape.NumFields(); fieldIndex++ {
				field := shape.Field(fieldIndex)
				tags, err := parseTags(shape.Tag(fieldIndex))
				if err != nil {
					return fail(err.Error())
				}
				if _, present := tags["foundry"]; present {
					return fail("DTO fields must not declare persistence tags")
				}
				if field.Embedded() && !field.Exported() && tags["json"] != "-" {
					if _, pointer := types.Unalias(field.Type()).(*types.Pointer); pointer {
						return fail("unexported embedded pointers cannot be allocated by JSON decoding")
					}
				}
			}
			properties, err := jsonProperties(typ)
			if err != nil {
				return fail(err.Error())
			}
			if types.Identical(typ, root) {
				graph.properties = properties
			}
			if len(properties) > jsonwire.MaxNodes-propertyCount {
				return fail("DTO fields exceed their resource bound")
			}
			propertyCount += len(properties)
			for _, property := range properties {
				id, err := add(property.Type, node.position)
				if err != nil {
					return fail(err.Error())
				}
				if property.Quoted {
					quotedType := types.Unalias(property.Type)
					_, pointer := quotedType.(*types.Pointer)
					if pointer {
						quotedType = quotedType.(*types.Pointer).Elem()
					}
					enum := false
					if named, ok := types.Unalias(quotedType).(*types.Named); ok {
						enum = p.enumTypes[named] || hasEnumDescriptor(named)
					}
					if enum || customJSONShape(quotedType) {
						return fail("json string options cannot override a custom codec")
					}
					base := id
					id = "quoted:" + base
					if seen[id] == nil {
						if len(graph.nodes) >= jsonwire.MaxNodes {
							return fail("DTO graph exceeds its resource bound")
						}
						quoted := &dtoNode{wire: contract.Type{ID: id, Kind: contract.QuotedKind, Element: base, Nullable: pointer}}
						seen[id] = quoted
						graph.nodes = append(graph.nodes, quoted)
					}
				}
				node.wire.Properties = append(node.wire.Properties, contract.Property{Name: property.Name, Type: id, Required: !property.Optional})
			}
		case *types.Array:
			if shape.Len() > int64(int(^uint(0)>>1)) {
				return fail("DTO array length exceeds native bounds")
			}
			id, err := add(shape.Elem(), node.position)
			if err != nil {
				return fail(err.Error())
			}
			node.wire.Kind, node.wire.Element, node.wire.Length = contract.ArrayKind, id, value.Set(int(shape.Len()))
		case *types.Slice:
			node.wire.Nullable = true
			basic, bytes := types.Unalias(shape.Elem()).Underlying().(*types.Basic)
			enumElement := false
			if named, ok := types.Unalias(shape.Elem()).(*types.Named); ok {
				enumElement = p.enumTypes[named] || hasEnumDescriptor(named)
			}
			if bytes && basic.Kind() == types.Uint8 && !customJSONShape(shape.Elem()) && !enumElement {
				node.wire.Kind, node.wire.Format = contract.StringKind, contract.Base64Format
				continue
			}
			id, err := add(shape.Elem(), node.position)
			if err != nil {
				return fail(err.Error())
			}
			node.wire.Kind, node.wire.Element = contract.ArrayKind, id
		case *types.Map:
			key, err := resolveDTOMapKey(p, shape.Key(), node.position, models)
			if err != nil {
				return err
			}
			node.key = &key
			id, err := add(shape.Elem(), node.position)
			if err != nil {
				return fail(err.Error())
			}
			node.wire.Kind, node.wire.Element, node.wire.Nullable = contract.MapKind, id, true
		case *types.Interface:
			if shape.NumMethods() != 0 {
				return fail("non-empty interface DTO fields have no declared JSON implementation")
			}
			node.wire.Kind, node.wire.Nullable = contract.DynamicKind, true
		default:
			return fail("unsupported DTO field type")
		}
	}
	sort.Slice(graph.nodes, func(i, j int) bool { return graph.nodes[i].wire.ID < graph.nodes[j].wire.ID })
	return validateUnionGraph(graph)
}

// Check embedded declarations before jsonshape promotes their fields. On a
// fresh checkout a same-package model's generated identity method does not
// exist yet, and flattening first would hide the model behind scalar fields.
func dtoEmbedsModel(root types.Type, models map[*types.Named]bool) (bool, error) {
	queue := []types.Type{root}
	seen := make(map[types.Type]bool)
	for i := 0; i < len(queue); i++ {
		typ := types.Unalias(queue[i])
		if pointer, ok := typ.(*types.Pointer); ok {
			typ = types.Unalias(pointer.Elem())
		}
		if seen[typ] {
			continue
		}
		seen[typ] = true
		if named, ok := typ.(*types.Named); ok && (models[named] || hasDTOModelIdentity(named)) {
			return true, nil
		}
		structure, ok := typ.Underlying().(*types.Struct)
		if !ok {
			continue
		}
		for field := 0; field < structure.NumFields(); field++ {
			if structure.Field(field).Embedded() && reflect.StructTag(structure.Tag(field)).Get("json") != "-" {
				if len(queue) >= jsonwire.MaxNodes {
					return false, fmt.Errorf("DTO embedding graph exceeds its resource bound")
				}
				queue = append(queue, structure.Field(field).Type())
			}
		}
	}
	return false, nil
}

func hasDTOModelIdentity(typ types.Type) bool {
	methods := types.NewMethodSet(types.NewPointer(typ))
	method := methods.Lookup(nil, "FoundryIdentity")
	if method == nil {
		return false
	}
	sig, ok := method.Obj().Type().(*types.Signature)
	if !ok || sig.Params().Len() != 0 || sig.Results().Len() != 2 {
		return false
	}
	identity, ok := types.Unalias(sig.Results().At(0).Type()).(*types.Named)
	return ok && isNamed(identity, framework+"/model", "Identity") && types.Identical(sig.Results().At(1).Type(), types.Universe.Lookup("error").Type())
}

func dtoScalarFormat(named *types.Named) contract.Format {
	if named.Obj().Pkg() == nil {
		return ""
	}
	return contract.Format(jsonshape.ScalarFormat(named.Obj().Pkg().Path(), named.Obj().Name()))
}

func dtoBasicType(basic *types.Basic, wire *contract.Type) bool {
	switch basic.Kind() {
	case types.Bool:
		wire.Kind = contract.BooleanKind
	case types.String:
		wire.Kind = contract.StringKind
	case types.Int, types.Int8, types.Int16, types.Int32, types.Int64, types.Uint, types.Uint8, types.Uint16, types.Uint32, types.Uint64:
		wire.Kind = contract.IntegerKind
		wire.Signed = basic.Info()&types.IsUnsigned == 0
		switch basic.Kind() {
		case types.Int8, types.Uint8:
			wire.Bits = 8
		case types.Int16, types.Uint16:
			wire.Bits = 16
		case types.Int32, types.Uint32:
			wire.Bits = 32
		case types.Int64, types.Uint64:
			wire.Bits = 64
		}
	case types.Float32:
		wire.Kind, wire.Bits = contract.NumberKind, 32
	case types.Float64:
		wire.Kind, wire.Bits = contract.NumberKind, 64
	default:
		return false
	}
	return true
}
