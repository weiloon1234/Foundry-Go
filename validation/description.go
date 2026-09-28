package validation

import "github.com/weiloon1234/Foundry-Go/internal/jsonwire"

// Normalize validates and owns serialized inspection metadata. It reuses rule
// parameter validation and composition budgets; it creates no executable rule
// and does not claim that a custom/server-only rule runs in a browser.
func (d Description) Normalize() (Description, error) {
	remaining := maxRuleNodes
	var visit func(Description, int) ruleNode
	visit = func(info Description, depth int) ruleNode {
		bad := func() ruleNode { return ruleNode{err: invalid("invalid validation metadata")} }
		if remaining == 0 || depth > jsonwire.MaxDepth || len(info.Children) > remaining {
			return bad()
		}
		remaining--
		if !validText(info.Field, true) || !validText(info.OtherField, true) || !validText(info.Label, true) || !validText(info.OtherLabel, true) {
			return bad()
		}
		if info.LabelKey != "" && info.LabelKey.Validate() != nil || info.OtherLabelKey != "" && info.OtherLabelKey.Validate() != nil {
			return bad()
		}
		if info.Kind != FieldKind && info.Kind != CompareKind && (info.Field != "" || info.Label != "" || info.LabelKey != "") {
			return bad()
		}
		if info.Kind != CompareKind && (info.OtherField != "" || info.OtherLabel != "" || info.OtherLabelKey != "") {
			return bad()
		}
		if info.Kind == LeafKind {
			if info.Spec == nil || len(info.Children) != 0 {
				return bad()
			}
			spec, err := copySpec(*info.Spec)
			if err != nil {
				return ruleNode{err: err}
			}
			info.Spec = &spec
			if infoBytes(info) > maxDescriptionBytes {
				return bad()
			}
			return ruleNode{info: info, nodes: 1, bytes: infoBytes(info)}
		}
		if info.Spec != nil {
			return bad()
		}
		switch info.Kind {
		case AllKind, BailKind:
			if len(info.Children) == 0 {
				return bad()
			}
		case FieldKind, CompareKind:
			if info.Field == "" || len(info.Children) != 1 || info.Kind == CompareKind && info.OtherField == "" {
				return bad()
			}
		case OptionalKind, NullableKind, EachKind, PointerKind:
			if len(info.Children) != 1 {
				return bad()
			}
		case WhenKind, UnlessKind:
			if len(info.Children) != 2 {
				return bad()
			}
		default:
			return bad()
		}
		children := make([]ruleNode, 0, len(info.Children))
		for _, child := range info.Children {
			node := visit(child, depth+1)
			if node.err != nil {
				return node
			}
			children = append(children, node)
		}
		info.Children = nil
		return composeNode(info, children)
	}
	node := visit(d, 0)
	if node.err != nil {
		return Description{}, node.err
	}
	return node.info, nil
}
