package pagination

import (
	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/i18n"
)

// Label keys of the page number and page size parameters. Validation issues
// and exported parameter metadata carry them; their English labels are built
// in, and an application translates them, English included, in its catalog.
const (
	NumberLabelKey i18n.MessageKey = "http.pagination.page"
	SizeLabelKey   i18n.MessageKey = "http.pagination.size"
)

const (
	numberLabel = "Page"
	sizeLabel   = "Page size"
)

// MessageDefinitions declares the pagination labels for a catalog, beside
// validation.MessageDefinitions and http.MessageDefinitions. Configured
// applications register them automatically.
func MessageDefinitions() []i18n.MessageDefinition {
	return []i18n.MessageDefinition{{Key: NumberLabelKey}, {Key: SizeLabelKey}}
}

func labelPresentation(key i18n.MessageKey) contract.Presentation {
	return contract.Presentation{LabelKey: key}
}
