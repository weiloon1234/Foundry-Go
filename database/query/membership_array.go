package query

import (
	"database/sql/driver"
	"strings"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
)

func arrayLiteral(kind codec.ParameterType, values []driver.Value) (string, bool) {
	return querycore.ArrayLiteral(kind, values)
}

func quoteArrayElement(literal *strings.Builder, text string) {
	querycore.QuoteArrayElement(literal, text)
}
