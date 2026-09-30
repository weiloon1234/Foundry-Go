package manifest

import (
	"maps"
	"slices"

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

// passwordTypes computes sensitive reachability once through the shared
// contract owner that route registration also uses.
func (types typeIndex) passwordTypes() map[contract.TypeID]bool {
	return contract.PasswordTypes(slices.Collect(maps.Values(types)))
}

// urlParameter checks a path, query or realtime room parameter. Credentials
// must not travel in URLs, which proxies, logs and browser history retain.
func (types typeIndex) urlParameter(p Parameter) error {
	if p.Presentation.Kind == contract.PasswordPresentation {
		return invalid("password presentation cannot describe a URL parameter")
	}
	return types.parameter(p)
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
	// A repeated parameter's type and hint already describe each element, so
	// its element rule keeps them instead of resolving another level.
	member := func(id contract.TypeID, fields []Parameter, name string) (contract.TypeID, contract.Presentation, bool) {
		if id != "" {
			for _, p := range types.resolved(id).Properties {
				if p.Name == name {
					return p.Type, p.Presentation, false
				}
			}
			return "", contract.Presentation{}, false
		}
		for _, p := range fields {
			if p.Name == name {
				return p.Type, p.Presentation, p.Repeated
			}
		}
		return "", contract.Presentation{}, false
	}
	var visit func(validation.Description, contract.TypeID, []Parameter, contract.Presentation, bool, bool) error
	visit = func(rule validation.Description, id contract.TypeID, fields []Parameter, hint contract.Presentation, root, repeated bool) error {
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
				// A comparison's other field is a sibling in the same parent.
				if rule.Kind == validation.CompareKind && rule.OtherLabelKey != "" {
					if _, other, _ := member(id, fields, rule.OtherField); other.LabelKey != "" && other.LabelKey != rule.OtherLabelKey {
						return invalid("presentation and validation label keys disagree")
					}
				}
				id, hint, repeated = member(id, fields, rule.Field)
				fields = nil
			}
			root = false
			if hint.LabelKey != "" && rule.LabelKey != "" && hint.LabelKey != rule.LabelKey {
				return invalid("presentation and validation label keys disagree")
			}
		}
		if rule.Kind == validation.EachKind || rule.Kind == validation.EachValueKind {
			if repeated {
				repeated = false
			} else {
				id = types.resolved(id).Element
				hint = contract.Presentation{}
			}
		}
		if rule.Kind == validation.EachKeyKind {
			// A map key has no property presentation. Do not accidentally inspect
			// value fields or restart at the operation root inside a key rule.
			id, fields, hint, root, repeated = "", nil, contract.Presentation{}, false, false
		}
		if rule.Spec != nil {
			if hint.Kind == contract.EmailPresentation && rule.Spec.ID == validation.URLRuleID || hint.Kind == contract.URLPresentation && rule.Spec.ID == validation.EmailRuleID {
				return invalid("presentation and validation format disagree")
			}
		}
		for _, child := range rule.Children {
			if err := visit(child, id, fields, hint, root, repeated); err != nil {
				return err
			}
		}
		return nil
	}
	return visit(*op.Validation, "", nil, contract.Presentation{}, true, false)
}
