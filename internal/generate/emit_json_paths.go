package generate

import (
	"fmt"
	"go/types"
	"strings"

	"github.com/weiloon1234/Foundry-Go/internal/jsonshape"
	"github.com/weiloon1234/Foundry-Go/internal/jsonwire"
)

type jsonPathProperty struct {
	jsonshape.Property[types.Type]
	child *jsonPathNode
}
type jsonPathNode struct {
	name       string
	payload    types.Type
	properties []jsonPathProperty
	child      *jsonPathNode
	key        types.Type // nil for an array index
	scalar     *field
	parent     *jsonPathNode
}

func (e *emitter) jsonPathNodes(name string, payload types.Type) ([]*jsonPathNode, error) {
	nodes := []*jsonPathNode{{name: name, payload: payload}}
	child := func(parent *jsonPathNode, segment string, t types.Type) *jsonPathNode {
		t = types.Unalias(jsonPayload(t))
		// Reuse recursive ancestors, not unrelated properties of the same
		// type: adding a sibling must not rename an existing public path type.
		for node := parent; node != nil; node = node.parent {
			if types.Identical(node.payload, t) {
				return node
			}
		}
		n := &jsonPathNode{name: parent.name + "_" + jsonPathName(segment), payload: t, parent: parent}
		nodes = append(nodes, n)
		return n
	}
	propertyCount := 0
	for i := 0; i < len(nodes); i++ {
		if len(nodes) > jsonwire.MaxNodes {
			return nil, fmt.Errorf("JSON property graph exceeds its resource bound")
		}
		n := nodes[i]
		base := jsonBase(n.payload)
		opaque := customJSONShape(base)
		scalarKind, err := fieldKind(base)
		// JSON byte slices are base64 text, not a PostgreSQL bytea scalar.
		// Persisted binary support must not add a bytea cast to JSON paths.
		if err == nil && scalarKind != "JSON" && scalarKind != "Binary" {
			f := field{base: base, typ: base, kind: scalarKind}
			fields := []field{f}
			markEnumFields(fields, e.pkg.enumTypes)
			f = fields[0]
			known := !opaque || f.enum
			if named, ok := base.(*types.Named); ok {
				known = known || isNamed(named, framework+"/decimal", "Decimal") || isNamed(named, framework+"/model", "ID") || isNamed(named, "time", "Time") || (named.Obj().Pkg() != nil && named.Obj().Pkg().Path() == framework+"/temporal")
				if isNamed(named, "encoding/json", "Number") {
					known = false
				}
			}
			if known {
				n.scalar = &f
			}
		}
		if opaque {
			continue
		}
		switch shape := base.Underlying().(type) {
		case *types.Struct:
			properties, err := jsonProperties(base)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", name, err)
			}
			names := make(map[string]bool)
			for _, p := range properties {
				propertyCount++
				if propertyCount > jsonwire.MaxNodes {
					return nil, fmt.Errorf("JSON properties exceed their resource bound")
				}
				if names[p.GoName] {
					return nil, fmt.Errorf("%s: promoted JSON properties require distinct Go field names", name)
				}
				names[p.GoName] = true
				n.properties = append(n.properties, jsonPathProperty{p, child(n, p.GoName, p.Type)})
			}
		case *types.Map:
			n.key = shape.Key()
			n.child = child(n, "Entry", shape.Elem())
		case *types.Slice:
			if b, ok := shape.Elem().Underlying().(*types.Basic); ok && b.Kind() == types.Uint8 {
				continue
			} // encoding/json encodes byte slices as base64 text.
			n.child = child(n, "Element", shape.Elem())
		case *types.Array:
			n.child = child(n, "Element", shape.Elem())
		}
	}
	return nodes, nil
}

func (e *emitter) emitJSONField(name string, f field) {
	payload, _ := jsonWrapper(f.base, "JSON")
	root := name + f.name
	pathRoot := jsonPathName(name) + "_" + jsonPathName(f.name)
	nodes, err := e.jsonPathNodes(pathRoot, payload)
	if err != nil {
		e.err = err
		return
	}
	query := e.use(framework + "/database/query")
	// Resolve every referenced type/codec before choosing locals. Both local
	// payload types and imported package names can otherwise shadow them.
	for _, node := range nodes {
		e.typeName(node.payload)
		if node.key != nil {
			e.typeName(node.key)
		}
		if node.scalar != nil {
			e.fieldCodec(*node.scalar, false)
		}
	}
	scope := e.localName("FoundryScope")
	fieldVar, keyVar := e.localName("field"), e.localName("key")
	for _, nullable := range []bool{false, true} {
		class := root + "JSONField"
		embedded, constructor := "JSONField", "JSONRoot"
		if nullable {
			class = root + "NullableJSONField"
			embedded = "NullableJSONField"
			constructor = "JSONNullableRoot"
		}
		receiver := fmt.Sprintf("%s %s[%s]", fieldVar, class, scope)
		e.line("// %s retains whole-document operators and generated payload access.", class)
		e.line("type %s[%s any] struct{%s.%s[%s,%s]}", class, scope, query, embedded, scope, e.typeName(f.base))
		e.line("// Path returns this field's typed JSON path.")
		e.line("func(%s)Path()%sJSONPath[%s]{return %sJSONPath[%s]{%s.%s[%s,%s](%s.%s)}}", receiver, pathRoot, scope, pathRoot, scope, query, constructor, scope, e.typeName(payload), fieldVar, embedded)
		if len(nodes[0].properties) > 0 {
			e.line("// Properties exposes payload fields without repeating JSON property names.")
			e.line("func(%s)Properties()%sJSONProperties[%s]{return %s.Path().Properties()}", receiver, pathRoot, scope, fieldVar)
		}
		if node := nodes[0]; node.child != nil {
			key := "int32"
			if node.key != nil {
				key = e.typeName(node.key)
			}
			e.line("// At selects a typed array element or map entry.")
			e.line("func(%s)At(%s %s)%sJSONPath[%s]{return %s.Path().At(%s)}", receiver, keyVar, key, node.child.name, scope, fieldVar, keyVar)
		}
	}
	for _, node := range nodes {
		e.emitJSONPathNode(node, scope)
	}
}

// An escaped separator preserves field boundaries: A.B and AB (or A_B) have
// distinct names. Names do not depend on sibling order or payload-type reuse.
func jsonPathName(segment string) string { return strings.ReplaceAll(segment, "_", "__") }

func (e *emitter) emitJSONPathNode(n *jsonPathNode, scope string) {
	query := e.use(framework + "/database/query")
	payload := e.typeName(n.payload)
	pathVar, keyVar := e.localName("path"), e.localName("key")
	receiver := fmt.Sprintf("%s %sJSONPath[%s]", pathVar, n.name, scope)
	e.line("// %sJSONPath retains the concrete payload and owning query scope.", n.name)
	e.line("type %sJSONPath[%s any] struct{%s.JSONPath[%s,%s]}", n.name, scope, query, scope, payload)
	if len(n.properties) > 0 {
		e.line("// %sJSONProperties describes the payload's declared properties.", n.name)
		e.line("type %sJSONProperties[%s any] struct{", n.name, scope)
		for _, p := range n.properties {
			e.line("%s %sJSONPath[%s]", p.GoName, p.child.name, scope)
		}
		e.line("}")
		e.line("// Properties returns independent typed property descriptors.")
		e.line("func(%s)Properties()%sJSONProperties[%s]{return %sJSONProperties[%s]{", receiver, n.name, scope, n.name, scope)
		for _, p := range n.properties {
			e.line("%s:%sJSONPath[%s]{%s.NewJSONProperty[%s,%s,%s](%s.JSONPath,%q,%t)},", p.GoName, p.child.name, scope, query, scope, payload, e.typeName(p.child.payload), pathVar, p.Name, p.Quoted)
		}
		e.line("}}")
	}
	if n.child != nil {
		key, constructor := "int32", "NewJSONArrayElement"
		if n.key != nil {
			key = e.typeName(n.key)
			constructor = "NewJSONMapEntry"
		}
		e.line("// At selects one entry. Missing entries remain absent; array indices may be negative.")
		e.line("func(%s)At(%s %s)%sJSONPath[%s]{return %sJSONPath[%s]{%s.%s[%s,%s,%s](%s.JSONPath,%s)}}", receiver, keyVar, key, n.child.name, scope, n.child.name, scope, query, constructor, scope, payload, e.typeName(n.child.payload), pathVar, keyVar)
	}
	if f := n.scalar; f != nil {
		class, constructor := "RowExpression", "JSONScalar"
		resultType := e.use(framework+"/value") + ".Nullable[" + e.typeName(f.base) + "]"
		if f.kind == "Text" {
			class, constructor = "NullableTextRowExpression", "JSONTextScalar"
			resultType = e.typeName(f.base)
		} else if f.kind != "Scalar" {
			class, constructor = "NullableOrderedRowExpression", "JSONOrderedScalar"
			resultType = e.typeName(f.base)
		}
		e.line("// Scalar extracts a nullable typed scalar. Missing paths and JSON null become SQL NULL.")
		e.line("func(%s)Scalar()%s.%s[%s,%s]{return %s.%s(%s.JSONPath,%s)}", receiver, query, class, scope, resultType, query, constructor, pathVar, e.fieldCodec(*f, false))
	}
}
