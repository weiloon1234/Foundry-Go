package contractmeta

import (
	"slices"

	"github.com/weiloon1234/Foundry-Go/contract"
)

// ComposePage substitutes the explicit row parameter in a pagination template.
// The template owns every page field; includes retain typed codec providers when
// constructing runtime contracts and normalized nodes when inspecting metadata.
func ComposePage(template contract.Schema, parameter, row, root contract.TypeID, includes ...contract.Type) contract.Schema {
	items := contract.TypeID("pagination:items:" + string(parameter))
	replace := func(id contract.TypeID) contract.TypeID {
		switch id {
		case template.Root:
			return root
		case parameter:
			return row
		case items:
			return contract.TypeID("pagination:items:" + string(row))
		default:
			return id
		}
	}
	result := contract.Schema{Root: root}
	for _, typ := range template.Types {
		if typ.ID == parameter {
			continue
		}
		typ.ID, typ.Element = replace(typ.ID), replace(typ.Element)
		typ.Properties = slices.Clone(typ.Properties)
		for i := range typ.Properties {
			typ.Properties[i].Type = replace(typ.Properties[i].Type)
		}
		result.Types = append(result.Types, typ)
	}
	result.Types = append(result.Types, includes...)
	return result
}

// PageRow is the named row parameter for metadata-only pagination templates.
type PageRow struct{}
