package typescript

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/contract/manifest"
)

// requestFields walks every request input of this entry: path, query and
// header parameters, form and multipart fields, multipart JSON parts and the
// JSON body's type graph. It returns the label and help keys a bound control
// can show, sorted and unique, and per operation the label tree of the body
// fields a binder binds (see labelObject).
func (r *renderer) requestFields() (keys []string, labelled map[string]string) {
	types := make(map[contract.TypeID]contract.Type, len(r.document.Types))
	for _, typ := range r.document.Types {
		types[typ.ID] = typ
	}
	seen := make(map[string]bool)
	add := func(p contract.Presentation) {
		for _, key := range []string{string(p.LabelKey), string(p.HelpKey)} {
			if key != "" {
				seen[key] = true
			}
		}
	}
	labelled = make(map[string]string)
	for _, op := range r.document.HTTP {
		for _, parameters := range [][]manifest.Parameter{op.Path, op.Query, op.Headers} {
			for _, p := range parameters {
				add(p.Presentation)
			}
		}
		if op.Body == nil {
			continue
		}
		// A type already on the current path is not entered again, so a
		// self-referencing DTO stays finite. The runtime binder follows the
		// same rule (typescript/runtime/descriptors.ts, binds and bindValue).
		active := make(map[contract.TypeID]bool)
		var node func(id contract.TypeID, labelled bool) string
		node = func(id contract.TypeID, labelled bool) string {
			// A value a control shows needs its own Go label (a row's is its
			// list's); a nested object or a list binds without one when
			// something below it binds. "" is a value that does not bind.
			leaf := ""
			if labelled {
				leaf = "true"
			}
			typ, ok := resolvedType(types, id)
			if !ok {
				return leaf
			}
			if active[typ.ID] {
				// The binder still binds a labelled nested object as a group;
				// its fields below the repeat are not offered.
				if typ.Kind == contract.ObjectKind && labelled {
					return "{}"
				}
				return leaf
			}
			active[typ.ID] = true
			defer delete(active, typ.ID)
			switch typ.Kind {
			case contract.ArrayKind:
				return listNode(node(typ.Element, labelled), labelled)
			case contract.MapKind:
				// A map's entries are bound whole and show the map's label.
				if entry := node(typ.Element, true); labelled {
					return "readonly [" + entry + "]"
				}
				return ""
			case contract.UnionKind:
				for _, variant := range typ.Variants {
					node(variant.Type, false)
				}
			case contract.ObjectKind:
				fields := make(map[string]string)
				for _, property := range typ.Properties {
					add(property.Presentation)
					if below := node(property.Type, property.Presentation.LabelKey != ""); below != "" {
						fields[property.Name] = below
					}
				}
				if len(fields) == 0 && !labelled {
					return ""
				}
				return labelObject(fields)
			}
			return leaf
		}
		fields := make(map[string]string)
		parameter := func(p manifest.Parameter) {
			add(p.Presentation)
			labelled := p.Presentation.LabelKey != ""
			below := ""
			if labelled {
				below = "true"
			}
			if p.Type != "" {
				below = node(p.Type, labelled)
			}
			if p.Repeated {
				below = listNode(below, labelled)
			}
			if below != "" {
				fields[p.Name] = below
			}
		}
		for _, p := range op.Body.Fields {
			parameter(p)
		}
		for _, part := range op.Body.Parts {
			parameter(part.Parameter)
		}
		if op.Body.Type != "" {
			if tree := node(op.Body.Type, false); strings.HasPrefix(tree, "{") {
				labelled[op.Name] = tree
			}
		} else {
			labelled[op.Name] = labelObject(fields)
		}
	}
	keys = make([]string, 0, len(seen))
	for key := range seen {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys, labelled
}

// resolvedType follows aliases and quoted values to the type that decides how
// a value binds.
func resolvedType(types map[contract.TypeID]contract.Type, id contract.TypeID) (contract.Type, bool) {
	typ, ok := types[id]
	for range len(types) {
		if !ok || typ.Kind != contract.AliasKind && typ.Kind != contract.QuotedKind {
			break
		}
		typ, ok = types[typ.Element]
	}
	return typ, ok
}

// listNode renders a list from its entry: "readonly [entry]" for a labelled
// list, bound whole and row by row, and "readonly (entry)[]" for a list
// without a label of its own, bound by row only when its rows bind.
func listNode(entry string, labelled bool) string {
	switch {
	case labelled:
		return "readonly [" + entry + "]"
	case entry == "":
		return ""
	}
	return "readonly (" + entry + ")[]"
}

// labelObject renders the bound fields of an object, sorted by name. Each
// field's node is what Go declares below that value, so the binder's types
// follow Go's structure: "true" for a value bound whole, an object of the
// bound fields of a nested object, "readonly [entry]" for a labelled list or
// map whose entries are bound one by one, and "readonly (entry)[]" for a list
// without a label of its own, bound by row.
func labelObject(fields map[string]string) string {
	if len(fields) == 0 {
		return "{}"
	}
	var out strings.Builder
	out.WriteString("{")
	for _, name := range slices.Sorted(maps.Keys(fields)) {
		fmt.Fprintf(&out, " readonly %s: %s;", quote(name), fields[name])
	}
	out.WriteString(" }")
	return out.String()
}

func unionOf(values []string) string {
	if len(values) == 0 {
		return "never"
	}
	quoted := make([]string, len(values))
	for i, value := range values {
		quoted[i] = quote(value)
	}
	return strings.Join(quoted, " | ")
}

// OperationInputs reuses request rendering: descriptors never maintain a second
// payload type map. Raw binary bodies have no implicitly discoverable fields.
func (r *renderer) descriptors() {
	// Typing presentation keys by what Go declared lets an application route
	// them into its catalog-typed translator: a key without catalog text then
	// fails that one line instead of rendering the raw key. Only request
	// inputs count: a response label cannot break a form.
	keys, labelled := r.requestFields()
	fmt.Fprintf(&r.out, "\n/** Every label and help key a request field of this entry declares. */\nexport type FieldMessageKey = %s;\n", unionOf(keys))
	// The field binder binds a value a control shows only when it has a Go
	// label; such a field without one is absent from its type, so binding it
	// fails to compile. A nested object or a list needs no label of its own.
	r.out.WriteString("/**\n * Each operation's bound body fields as Go declares them: true for a labelled\n * value bound whole, the bound fields of a nested object, [entry] for a\n * labelled list or map whose entries are also bound one by one, and (entry)[]\n * for a list without a label of its own, bound by row only.\n */\nexport interface OperationBodyLabels {\n")
	for _, op := range r.document.HTTP {
		if tree := labelled[op.Name]; tree != "" && tree != "{}" {
			fmt.Fprintf(&r.out, " readonly %s: %s;\n", quote(op.Name), tree)
		}
	}
	r.out.WriteString("}\n")
	r.out.WriteString("\nexport interface OperationInputs {\n")
	for _, op := range r.document.HTTP {
		fmt.Fprintf(&r.out, " readonly %s: {", quote(op.Name))
		for _, location := range []struct {
			name    string
			present bool
		}{
			{"path", len(op.Path) > 0}, {"query", len(op.Query) > 0}, {"headers", len(op.Headers) > 0}, {"body", op.Body != nil && op.Body.Raw == nil},
		} {
			if location.present {
				fmt.Fprintf(&r.out, " readonly %s: NonNullable<Operations[%s][\"request\"][%s]>;", location.name, quote(op.Name), quote(location.name))
			}
		}
		r.out.WriteString(" };\n")
	}
	r.out.WriteString("}\n")
	// JSON bodies, whatever their root (object, union, array or map), can be
	// described from the root; form and multipart bodies are named fields.
	r.out.WriteString("export interface OperationJSONBodies {\n")
	for _, op := range r.document.HTTP {
		if op.Body != nil && op.Body.Raw == nil && op.Body.Type != "" {
			fmt.Fprintf(&r.out, " readonly %s: OperationInputs[%s][\"body\"];\n", quote(op.Name), quote(op.Name))
		}
	}
	r.out.WriteString("}\n")
	// The generated names are already collision-checked by the manifest owner.
	r.out.WriteString(`/** Inspect a registered operation without making a request. */
export function operation<K extends keyof Operations>(name: K): OperationDescriptor<K> {
  return describeOperation(name) as OperationDescriptor<K>;
}
/** Inspect an explicitly public DTO. A DTO descriptor has no route or submit method. */
export function schema<K extends keyof ContractTypes>(id: K): FieldDescriptor<ContractTypes[K], readonly ["schema", K]> {
  return describeField(id, true, "", undefined) as FieldDescriptor<ContractTypes[K], readonly ["schema", K]>;
}
`)
}
