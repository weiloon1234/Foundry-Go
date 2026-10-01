package typescript

import "fmt"

// OperationInputs reuses request rendering: descriptors never maintain a second
// payload type map. Raw binary bodies have no implicitly discoverable fields.
func (r *renderer) descriptors() {
	r.out.WriteString("\nexport interface OperationInputs {\n")
	for _, op := range r.document.HTTP {
		fmt.Fprintf(&r.out, " readonly %s: {", quote(op.Name))
		for _, location := range []struct {
			name    string
			present bool
		}{
			{"path", len(op.Path) > 0}, {"query", len(op.Query) > 0}, {"body", op.Body != nil && op.Body.Raw == nil},
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
