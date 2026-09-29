package query

import (
	"database/sql/driver"
	"strconv"
	"strings"
	"time"

	"github.com/weiloon1234/Foundry-Go/database/codec"
)

// arrayLiteral renders bound scalar values as one PostgreSQL array literal for
// = ANY($n) / <> ALL($n). It is a bound parameter, never SQL text: the server
// parses it using the element type inferred from the compared operand. Only
// scalar kinds whose driver values have one unambiguous text form qualify;
// anything else keeps one parameter per value.
func arrayLiteral(kind codec.ParameterType, values []driver.Value) (string, bool) {
	switch kind {
	case codec.TypeBoolean, codec.TypeInteger, codec.TypeText, codec.TypeUUID, codec.TypeDecimal, codec.TypeDate, codec.TypeDateTime:
	default:
		return "", false
	}
	var literal strings.Builder
	literal.WriteByte('{')
	for i, value := range values {
		if i != 0 {
			literal.WriteByte(',')
		}
		switch v := value.(type) {
		case bool:
			if kind != codec.TypeBoolean {
				return "", false
			}
			if v {
				literal.WriteString("t")
			} else {
				literal.WriteString("f")
			}
		case int64:
			literal.WriteString(strconv.FormatInt(v, 10))
		case string:
			quoteArrayElement(&literal, v)
		case time.Time:
			if kind != codec.TypeDateTime {
				return "", false
			}
			quoteArrayElement(&literal, v.UTC().Format("2006-01-02 15:04:05.999999999Z07:00"))
		default:
			return "", false
		}
	}
	literal.WriteByte('}')
	return literal.String(), true
}

// quoteArrayElement always quotes, so NULL, empty strings, braces, commas and
// whitespace remain ordinary element text. Backslash and quote are escaped.
func quoteArrayElement(literal *strings.Builder, text string) {
	literal.WriteByte('"')
	for i := 0; i < len(text); i++ {
		if text[i] == '"' || text[i] == '\\' {
			literal.WriteByte('\\')
		}
		literal.WriteByte(text[i])
	}
	literal.WriteByte('"')
}
