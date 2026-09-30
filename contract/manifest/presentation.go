package manifest

import (
	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/validation"
	"github.com/weiloon1234/Foundry-Go/websocket"
)

func (types typeIndex) resolved(id contract.TypeID) contract.Type {
	typ := types[id]
	for typ.Kind == contract.AliasKind {
		typ = types[typ.Element]
	}
	return typ
}

func (types typeIndex) presentation(id contract.TypeID, p contract.Presentation) error {
	return p.ValidateType(types.resolved(id))
}

// Compute sensitive reachability once, including recursive DTO graphs. Only an
// explicit password hint is used; names and persistence models are not scanned.
func (types typeIndex) passwordTypes() map[contract.TypeID]bool {
	parents := make(map[contract.TypeID][]contract.TypeID)
	sensitive := make(map[contract.TypeID]bool)
	var pending []contract.TypeID
	for id, typ := range types {
		for _, p := range typ.Properties {
			parents[p.Type] = append(parents[p.Type], id)
			if p.Presentation.Kind == contract.PasswordPresentation && !sensitive[id] {
				sensitive[id] = true
				pending = append(pending, id)
			}
		}
		if typ.Element != "" {
			parents[typ.Element] = append(parents[typ.Element], id)
		}
		for _, v := range typ.Variants {
			parents[v.Type] = append(parents[v.Type], id)
		}
	}
	for i := 0; i < len(pending); i++ {
		for _, parent := range parents[pending[i]] {
			if !sensitive[parent] {
				sensitive[parent] = true
				pending = append(pending, parent)
			}
		}
	}
	return sensitive
}

func (types typeIndex) outputPresentation(d *Document) error {
	passwords := types.passwordTypes()
	for _, table := range d.Tables {
		if passwords[table.Row] {
			return invalid("password presentation cannot be a table output")
		}
	}
	for _, notice := range d.Notifications {
		for _, channel := range notice.Channels {
			if passwords[channel.Payload] {
				return invalid("password presentation cannot be notification output")
			}
		}
	}
	if d.Realtime != nil {
		for _, channel := range d.Realtime.Channels {
			if passwords[channel.Presence] {
				return invalid("password presentation cannot be presence output")
			}
			for _, event := range channel.Events {
				if event.Direction == websocket.ServerToClient && passwords[event.Payload] {
					return invalid("password presentation cannot be event output")
				}
			}
		}
	}
	return nil
}

func (types typeIndex) operationPresentation(op Operation, passwords map[contract.TypeID]bool) error {
	if op.Response != nil && passwords[op.Response.Type] {
		return invalid("password presentation is input-only; declare an explicit response view")
	}
	if op.Body != nil && passwords[op.Body.Type] && len(op.Body.Example) != 0 {
		return invalid("credential inputs cannot publish body examples")
	}
	if op.Validation == nil {
		return nil
	}
	// Follow the existing rule tree into the corresponding transport/schema.
	// No validation rule is translated into a second set of form constraints.
	var visit func(validation.Description, contract.TypeID, []Parameter, contract.Presentation, bool) error
	visit = func(rule validation.Description, id contract.TypeID, fields []Parameter, hint contract.Presentation, root bool) error {
		if rule.Kind == validation.FieldKind || rule.Kind == validation.CompareKind {
			if root {
				switch rule.Field {
				case "path":
					fields = op.Path
				case "query":
					fields = op.Query
				case "body":
					if op.Body != nil {
						id, fields = op.Body.Type, op.Body.Fields
						for _, part := range op.Body.Parts {
							fields = append(fields, part.Parameter)
						}
					}
				}
			} else {
				hint = contract.Presentation{}
				if id != "" {
					shape := types.resolved(id)
					id = ""
					for _, p := range shape.Properties {
						if p.Name == rule.Field {
							id, hint = p.Type, p.Presentation
							break
						}
					}
				} else {
					for _, p := range fields {
						if p.Name == rule.Field {
							id, hint = p.Type, p.Presentation
							break
						}
					}
				}
				fields = nil
			}
			root = false
			if hint.LabelKey != "" && rule.LabelKey != "" && hint.LabelKey != rule.LabelKey {
				return invalid("presentation and validation label keys disagree")
			}
		}
		if rule.Kind == validation.EachKind || rule.Kind == validation.EachValueKind {
			id = types.resolved(id).Element
			hint = contract.Presentation{}
		}
		if rule.Kind == validation.EachKeyKind {
			// A map key has no property presentation. Do not accidentally inspect
			// value fields or restart at the operation root inside a key rule.
			id, fields, hint, root = "", nil, contract.Presentation{}, false
		}
		if rule.Spec != nil {
			if hint.Kind == contract.EmailPresentation && rule.Spec.ID == "foundry.url" || hint.Kind == contract.URLPresentation && rule.Spec.ID == "foundry.email" {
				return invalid("presentation and validation format disagree")
			}
		}
		for _, child := range rule.Children {
			if err := visit(child, id, fields, hint, root); err != nil {
				return err
			}
		}
		return nil
	}
	return visit(*op.Validation, "", nil, contract.Presentation{}, true)
}
