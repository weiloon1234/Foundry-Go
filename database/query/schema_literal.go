package query

import (
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
)

func quoteSchemaText(value string, remaining int) (string, error) {
	return querycore.QuoteSchemaText(value, remaining)
}
