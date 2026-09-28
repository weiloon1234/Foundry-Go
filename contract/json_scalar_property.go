package contract

// ScalarProperty is a snapshot of one declared top-level property and the
// underlying scalar used by its Go value. Quoted distinguishes JSON's string
// representation of a scalar from its typed value. Value.Nullable includes
// nullable aliases; the full wire graph remains available through Description.
type ScalarProperty struct {
	Property Property `json:"property"`
	Value    Type     `json:"value"`
	Quoted   bool     `json:"quoted"`
}

// DescribeScalarProperty resolves the existing compiled graph, without
// rediscovering Go fields or calling a value's codec. Structured/dynamic fields
// are not scalar columns. Returned metadata owns its enum case buffers.
func (d JSON[T]) DescribeScalarProperty(name string) (ScalarProperty, error) {
	if err := d.Validate(); err != nil {
		return ScalarProperty{}, err
	}
	return d.schema.scalarProperty(name)
}

// ScalarProperties validates inspection metadata and describes selected
// top-level scalar properties in the supplied order. With no names it describes
// every property in normalized order. It shares alias/nullability/quoted
// resolution with typed JSON descriptors and rejects selected structured fields.
func (s Schema) ScalarProperties(names ...string) ([]ScalarProperty, error) {
	compiled, err := compileSchemaMode(s, true)
	if err != nil {
		return nil, err
	}
	root := compiled.types[compiled.description.Root]
	if root.Kind != ObjectKind || root.Nullable {
		return nil, invalidSchema()
	}
	if len(names) == 0 {
		names = make([]string, 0, len(root.Properties))
		for _, property := range root.Properties {
			names = append(names, property.Name)
		}
	}
	result := make([]ScalarProperty, 0, len(names))
	seen := make(map[string]bool, len(names))
	for _, name := range names {
		if seen[name] {
			return nil, invalidSchema()
		}
		seen[name] = true
		info, err := compiled.scalarProperty(name)
		if err != nil {
			return nil, err
		}
		result = append(result, info)
	}
	return result, nil
}

func (s *compiledSchema) scalarProperty(name string) (ScalarProperty, error) {
	property, ok := s.properties[s.description.Root][name]
	if !ok {
		return ScalarProperty{}, invalidSchema()
	}
	result := ScalarProperty{Property: property}
	id, nullable := property.Type, false
	for steps := 0; steps < len(s.types); steps++ {
		typ, ok := s.types[id]
		if !ok {
			return ScalarProperty{}, invalidSchema()
		}
		nullable = nullable || typ.Nullable
		switch typ.Kind {
		case AliasKind:
			id = typ.Element
		case QuotedKind:
			result.Quoted = true
			id = typ.Element
		case StringKind, IntegerKind, NumberKind, BooleanKind:
			typ.Nullable = nullable
			result.Value = cloneType(typ)
			return result, nil
		default:
			return ScalarProperty{}, invalidSchema()
		}
	}
	return ScalarProperty{}, invalidSchema()
}
