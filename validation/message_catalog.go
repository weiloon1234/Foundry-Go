package validation

import "github.com/weiloon1234/Foundry-Go/i18n"

// ValidateMessages checks a rule description against a catalog at startup.
// Missing definitions are optional (English fallback); registered signatures and
// parameter-free label declarations must match. No selectors or checks execute.
func ValidateMessages(description Description, catalog *i18n.Catalog) error {
	if catalog.Validate() != nil {
		return invalid("invalid validation message catalog")
	}
	owned, err := description.Normalize()
	if err != nil {
		return err
	}
	var visit func(Description) error
	visit = func(node Description) error {
		for _, key := range []i18n.MessageKey{node.LabelKey, node.OtherLabelKey} {
			if key != "" {
				if definition, exists := catalog.Definition(key); exists && len(definition.Parameters) != 0 {
					return invalid("validation labels require parameter-free messages")
				}
			}
		}
		if node.Spec != nil && node.Spec.Translation != nil {
			definition := node.Spec.Translation.Definition
			if _, exists := catalog.Definition(definition.Key); exists {
				if err := catalog.Accepts(definition); err != nil {
					return err
				}
			}
		}
		for _, child := range node.Children {
			if err := visit(child); err != nil {
				return err
			}
		}
		return nil
	}
	return visit(owned)
}
