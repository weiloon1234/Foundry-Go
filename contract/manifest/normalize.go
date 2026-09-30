package manifest

import (
	"cmp"
	"slices"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/internal/jsonwire"
)

func normalizeDocument(d *Document) error {
	if d.Version != Version {
		return invalid("unsupported manifest version")
	}
	if len(d.Types) == 0 || len(d.Types) > jsonwire.MaxNodes || len(d.Roots) > MaxOperations {
		return invalid("invalid schema catalogue size")
	}
	schema, err := (contract.Schema{Root: d.ErrorType, Types: d.Types}).Normalize()
	if err != nil {
		return err
	}
	d.Types = schema.Types
	types := make(typeIndex, len(d.Types))
	for _, typ := range d.Types {
		types[typ.ID] = typ
	}
	for _, root := range d.Roots {
		if !types.has(root) {
			return invalid("missing public schema root")
		}
	}
	slices.Sort(d.Roots)
	d.Roots = slices.Compact(d.Roots)
	if err := normalizeHTTP(d, types); err != nil {
		return err
	}
	if err := normalizeRealtime(d.Realtime, types); err != nil {
		return err
	}
	if err := normalizeFeatures(d, types); err != nil {
		return err
	}
	if err := types.outputPresentation(d); err != nil {
		return err
	}
	slices.SortFunc(d.Types, func(a, b contract.Type) int { return cmp.Compare(a.ID, b.ID) })
	return nil
}
