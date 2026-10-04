package application

import (
	"slices"

	"github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/http/pagination"
	"github.com/weiloon1234/Foundry-Go/i18n"
	"github.com/weiloon1234/Foundry-Go/validation"
)

// MessageDefinitions returns the framework message definitions a configured
// application's catalog registers: validation rules, HTTP errors and input
// decoding, and pagination labels. An application that builds its own catalog
// declares them beside its own messages, so later framework additions reach it
// as well. The result is owned by the caller.
func MessageDefinitions() []i18n.MessageDefinition {
	return slices.Concat(validation.MessageDefinitions(), http.MessageDefinitions(), pagination.MessageDefinitions())
}
