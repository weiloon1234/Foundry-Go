package contract

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/jsonwire"
)

type compiledSchema struct {
	description Schema
	types       map[TypeID]Type
	properties  map[TypeID]map[string]Property
	cases       map[TypeID]map[string]bool
	variants    map[TypeID]map[string]TypeID
}

func invalidSchema() error { return fault.New(fault.Invalid, "invalid JSON contract declaration") }

func declarationText(text string) bool {
	return text != "" && utf8.ValidString(text) && !strings.ContainsFunc(text, func(r rune) bool { return r < ' ' || r == 127 })
}

func compileSchema(input Schema) (*compiledSchema, error) {
	return compileSchemaMode(input, false)
}

func compileSchemaMode(input Schema, metadataOnly bool) (*compiledSchema, error) {
	return compileSchemaGraph(input, metadataOnly, false)
}

func compileSchemaGraph(input Schema, metadataOnly, shared bool) (*compiledSchema, error) {
	if !declarationText(string(input.Root)) || len(input.Types) == 0 || len(input.Types) > jsonwire.MaxNodes {
		return nil, invalidSchema()
	}
	types, err := expandJSONTypes(input.Types, shared)
	if err != nil {
		return nil, err
	}
	result := &compiledSchema{types: make(map[TypeID]Type), properties: make(map[TypeID]map[string]Property), cases: make(map[TypeID]map[string]bool), variants: make(map[TypeID]map[string]TypeID)}
	var byteElements []TypeID
	fields, remaining := 0, jsonwire.MaxBytes
	account := func(text string) bool {
		if len(text) > remaining {
			return false
		}
		remaining -= len(text)
		return true
	}
	if !account(string(input.Root)) {
		return nil, invalidSchema()
	}
	for _, typ := range types {
		if typ.byteElement != "" {
			if metadataOnly || typ.Kind != StringKind || typ.Format != Base64Format || !account(string(typ.byteElement)) {
				return nil, invalidSchema()
			}
			byteElements = append(byteElements, typ.byteElement)
			typ.byteElement = ""
		}
		if metadataOnly && typ.Key == nil && typ.jsonKey != nil {
			return nil, invalidSchema()
		}
		if metadataOnly && typ.Key != nil {
			key, err := normalizeKeyMetadata(*typ.Key)
			if err != nil || typ.Kind != MapKind {
				return nil, invalidSchema()
			}
			typ.Key, typ.jsonKey = &key, nil
		}
		if !declarationText(string(typ.ID)) || !account(string(typ.ID)) || !account(string(typ.Element)) || !account(string(typ.Format)) {
			return nil, invalidSchema()
		}
		if typ.enumCases != nil {
			if len(typ.Cases) != 0 {
				return nil, invalidSchema()
			}
			var cases []json.RawMessage
			err := callback.Isolated("resolve JSON enum contract", func() error {
				var err error
				cases, err = typ.enumCases()
				return err
			})
			if err != nil {
				return nil, fault.Wrap(fault.Invalid, "invalid JSON enum contract", err)
			}
			typ.Cases, typ.enumCases = cases, nil
		}
		if !account(typ.Discriminator) || len(typ.Variants) > jsonwire.MaxNodes-fields {
			return nil, invalidSchema()
		}
		fields += len(typ.Variants)
		variants := make(map[string]TypeID, len(typ.Variants))
		for _, variant := range typ.Variants {
			if !declarationText(variant.Tag) || !account(variant.Tag) || !declarationText(string(variant.Type)) || !account(string(variant.Type)) || variants[variant.Tag] != "" {
				return nil, invalidSchema()
			}
			variants[variant.Tag] = variant.Type
		}
		typ.Variants = slices.Clone(typ.Variants)
		slices.SortFunc(typ.Variants, func(a, b Variant) int { return strings.Compare(a.Tag, b.Tag) })
		if len(typ.Properties) > jsonwire.MaxNodes-fields {
			return nil, invalidSchema()
		}
		fields += len(typ.Properties)

		if typ.Key != nil {
			key := typ.Key.Value
			if !account(string(key.ID)) || !account(string(key.Format)) || !account(string(typ.Key.Syntax)) ||
				len(key.Cases) >= jsonwire.MaxNodes-fields {
				return nil, invalidSchema()
			}
			fields += len(key.Cases) + 1
			for _, literal := range key.Cases {
				if !account(string(literal)) {
					return nil, invalidSchema()
				}
			}
		}
		if !typeOptionsValid(typ, metadataOnly) {
			return nil, invalidSchema()
		}
		if typ.Kind == IntegerKind && typ.Bits == 0 {
			typ.Bits = uint8(strconv.IntSize)
		}
		properties := make(map[string]Property, len(typ.Properties))
		for _, property := range typ.Properties {
			if !account(string(property.Presentation.Kind)) || !account(string(property.Presentation.LabelKey)) || !account(string(property.Presentation.HelpKey)) || property.Presentation.Validate() != nil {
				return nil, invalidSchema()
			}
			if !utf8.ValidString(property.Name) || strings.ContainsRune(property.Name, 0) || !account(property.Name) || !account(string(property.Type)) || property.Type == "" {
				return nil, invalidSchema()
			}
			if _, duplicate := properties[property.Name]; duplicate {
				return nil, invalidSchema()
			}
			properties[property.Name] = property
		}
		if len(typ.Cases) > jsonwire.MaxNodes-fields {
			return nil, invalidSchema()
		}
		fields += len(typ.Cases)
		cases := make(map[string]bool, len(typ.Cases))
		ownedCases := make([]json.RawMessage, 0, len(typ.Cases))
		for _, literal := range typ.Cases {
			if !account(string(literal)) {
				return nil, invalidSchema()
			}
			node, err := jsonwire.Decode(literal, jsonwire.Limits{Bytes: jsonwire.MaxBytes, Depth: 0, Nodes: 1})
			if err != nil || !scalarValid(typ, node) {
				return nil, invalidSchema()
			}
			canonical, err := scalarCase(typ, node)
			if err != nil {
				return nil, invalidSchema()
			}
			if cases[string(canonical)] {
				return nil, invalidSchema()
			}
			cases[string(canonical)] = true
			ownedCases = append(ownedCases, canonical)
		}
		typ.Properties = slices.Clone(typ.Properties)
		if typ.Key != nil {
			key := cloneJSONKeyInfo(*typ.Key)
			typ.Key = &key
		}
		slices.SortFunc(typ.Properties, func(a, b Property) int { return strings.Compare(a.Name, b.Name) })
		typ.Cases = ownedCases
		slices.SortFunc(typ.Cases, func(a, b json.RawMessage) int { return bytes.Compare(a, b) })
		if previous, duplicate := result.types[typ.ID]; duplicate {
			if !sameNormalizedType(previous, typ) {
				return nil, invalidSchema()
			}
			continue
		}
		result.types[typ.ID] = typ
		result.properties[typ.ID] = properties
		result.cases[typ.ID] = cases
		result.variants[typ.ID] = variants
	}
	if _, exists := result.types[input.Root]; !exists {
		return nil, invalidSchema()
	}
	for _, typ := range result.types {
		for _, variant := range typ.Variants {
			target, exists := result.types[variant.Type]
			if !exists || target.Kind != ObjectKind || target.Nullable {
				return nil, invalidSchema()
			}
			if _, collision := result.properties[variant.Type][typ.Discriminator]; collision {
				return nil, invalidSchema()
			}
		}
		for _, property := range typ.Properties {
			if _, exists := result.types[property.Type]; !exists {
				return nil, invalidSchema()
			}
		}
		if typ.Element != "" {
			if _, exists := result.types[typ.Element]; !exists {
				return nil, invalidSchema()
			}
		}
	}
	// Aliases/quoted wrappers must eventually reach a structural/scalar node.
	// Recursive objects and arrays are legal; cycles with no such progress are not.
	state := make(map[TypeID]uint8)
	for id := range result.types {
		var chain []TypeID
		for state[id] != 2 {
			if state[id] == 1 {
				return nil, invalidSchema()
			}
			state[id] = 1
			chain = append(chain, id)
			typ := result.types[id]
			if typ.Kind != AliasKind && typ.Kind != QuotedKind {
				break
			}
			id = typ.Element
		}
		for _, visited := range chain {
			state[visited] = 2
		}
	}
	// Resolve alias-only paths once. Many quoted fields may share the same long
	// alias chain; validating each chain independently would be quadratic.
	aliasTargets := make(map[TypeID]TypeID)
	aliasNullable := make(map[TypeID]bool)
	for id := range result.types {
		var chain []TypeID
		for aliasTargets[id] == "" {
			chain = append(chain, id)
			typ := result.types[id]
			if typ.Kind != AliasKind {
				aliasTargets[id] = id
				aliasNullable[id] = typ.Nullable
				break
			}
			id = typ.Element
		}
		for i := len(chain) - 1; i >= 0; i-- {
			visited := chain[i]
			aliasTargets[visited] = aliasTargets[id]
			node := result.types[visited]
			aliasNullable[visited] = node.Nullable || aliasNullable[node.Element]
		}
	}
	for _, element := range byteElements {
		target := result.types[aliasTargets[element]]
		if target.Kind != IntegerKind || target.Bits != 8 || target.Signed || aliasNullable[element] || len(target.Cases) != 0 {
			return nil, invalidSchema()
		}
	}
	for _, typ := range result.types {
		for _, property := range typ.Properties {
			if err := property.Presentation.ValidateType(result.types[aliasTargets[property.Type]]); err != nil {
				// Name the public declaration; generation cannot check custom codecs.
				return nil, fault.New(fault.Invalid, fmt.Sprintf("client presentation on %s.%s contradicts its codec", typ.ID, property.Name))
			}
		}
	}
	for _, typ := range result.types {
		if typ.Kind != QuotedKind {
			continue
		}
		target := result.types[aliasTargets[typ.Element]]
		switch target.Kind {
		case BooleanKind, StringKind, IntegerKind, NumberKind:
		default:
			return nil, invalidSchema()
		}
	}
	result.description.Root = input.Root
	for _, typ := range result.types {
		result.description.Types = append(result.description.Types, typ)
	}
	slices.SortFunc(result.description.Types, func(a, b Type) int { return strings.Compare(string(a.ID), string(b.ID)) })
	return result, nil
}

func typeOptionsValid(typ Type, metadataOnly bool) bool {
	if typ.Kind == UnionKind {
		if !declarationText(typ.Discriminator) || len(typ.Variants) == 0 {
			return false
		}
	} else if typ.Discriminator != "" || len(typ.Variants) != 0 {
		return false
	}
	if !metadataOnly && !validJSONMapKeyDeclaration(typ) {
		return false
	}
	if typ.Kind != ObjectKind && len(typ.Properties) != 0 {
		return false
	}
	if typ.Kind != ArrayKind && typ.Length.IsSet() {
		return false
	}
	if n, set := typ.Length.Get(); set && n < 0 {
		return false
	}
	if typ.Kind != IntegerKind && typ.Signed {
		return false
	}
	if typ.Kind != IntegerKind && typ.Kind != NumberKind && typ.Bits != 0 {
		return false
	}
	if typ.Kind != StringKind && typ.Format != "" {
		return false
	}
	if typ.Kind != StringKind && typ.Kind != IntegerKind && len(typ.Cases) != 0 {
		return false
	}
	needsElement := typ.Kind == ArrayKind || typ.Kind == MapKind || typ.Kind == AliasKind || typ.Kind == QuotedKind
	if needsElement != (typ.Element != "") {
		return false
	}
	switch typ.Kind {
	case BooleanKind, ObjectKind, ArrayKind, MapKind, AliasKind, QuotedKind, DynamicKind, UnionKind:
		return true
	case StringKind:
		return formatValid(typ.Format)
	case IntegerKind:
		return typ.Bits == 0 || typ.Bits == 8 || typ.Bits == 16 || typ.Bits == 32 || typ.Bits == 64
	case NumberKind:
		return typ.Bits == 0 || typ.Bits == 32 || typ.Bits == 64
	default:
		return false
	}
}

func (s *compiledSchema) snapshot() Schema {
	result := Schema{Root: s.description.Root, Types: slices.Clone(s.description.Types)}
	for i := range result.Types {
		result.Types[i] = cloneType(result.Types[i])
	}
	return result
}

func cloneType(typ Type) Type {
	typ.Variants = slices.Clone(typ.Variants)
	typ.Properties = slices.Clone(typ.Properties)
	if typ.Key != nil {
		key := cloneJSONKeyInfo(*typ.Key)
		typ.Key = &key
	}
	typ.Cases = slices.Clone(typ.Cases)
	for j := range typ.Cases {
		typ.Cases[j] = bytes.Clone(typ.Cases[j])
	}
	return typ
}

func integerBits(typ Type) int {
	if typ.Bits == 0 {
		return strconv.IntSize
	}
	return int(typ.Bits)
}
