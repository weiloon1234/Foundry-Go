// Package httpquery exercises typed query binding from an independent consumer.
package httpquery

import (
	"foundry.test/consumer/models"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

//foundry:query
type SearchInput struct {
	User     model.ID[models.User]
	Search   value.Optional[string] `query:"q"`
	Statuses []models.Status        `query:"status"`
}

type OtherInput struct{ Search string }

var SearchParameters = SearchInputDescriptor()

func Parameters() foundryhttp.Query[SearchInput] { return SearchInputDescriptor() }
