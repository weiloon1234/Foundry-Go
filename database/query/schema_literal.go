package query

import (
	"encoding/hex"
	"math"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/weiloon1234/Foundry-Go/fault"
)

// Only a conflict target compiler enables this path. Values have already passed
// their declared codec; this final boundary owns PostgreSQL literal escaping and
// output bounds, independent of standard_conforming_strings.
func (c *compiler) schemaLiteral(value any) (string, error) {
	remaining := MaxScalarSQLBytes - c.scalarSQLBytes
	var text string
	switch v := value.(type) {
	case nil:
		text = "NULL"
	case bool:
		if v {
			text = "TRUE"
		} else {
			text = "FALSE"
		}
	case int64:
		text = strconv.FormatInt(v, 10)
	case float64:
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return "", fault.New(fault.Invalid, "schema constants require finite numbers")
		}
		if v == 0 {
			v = 0
		}
		text = strconv.FormatFloat(v, 'g', -1, 64)
	case string:
		var err error
		text, err = quoteSchemaText(v, remaining)
		if err != nil {
			return "", err
		}
	case []byte:
		if v == nil {
			text = "NULL"
			break
		}
		if remaining < 17 || len(v) > (remaining-17)/2 {
			return "", fault.New(fault.Invalid, "schema constant exceeds its resource bound")
		}
		text = "DECODE('" + hex.EncodeToString(v) + "', 'hex')"
	case time.Time:
		var err error
		text, err = quoteSchemaText(v.Format("2006-01-02T15:04:05.999999999Z07:00:00"), remaining)
		if err != nil {
			return "", err
		}
	default:
		return "", fault.New(fault.Invalid, "unsupported schema constant representation")
	}
	if len(text) > remaining {
		return "", fault.New(fault.Invalid, "schema constant exceeds its resource bound")
	}
	c.scalarSQLBytes += len(text)
	return text, nil
}

func quoteSchemaText(value string, remaining int) (string, error) {
	if remaining < 3 || len(value) > remaining-3 {
		return "", fault.New(fault.Invalid, "schema text constant exceeds its resource bound")
	}
	if !utf8.ValidString(value) || strings.IndexByte(value, 0) >= 0 {
		return "", fault.New(fault.Invalid, "schema text constant requires valid PostgreSQL UTF-8")
	}
	size := len(value) + 3
	for i := range len(value) {
		if value[i] == '\'' || value[i] == '\\' {
			size++
		}
		if size > remaining {
			return "", fault.New(fault.Invalid, "escaped schema constant exceeds its resource bound")
		}
	}
	var result strings.Builder
	result.Grow(size)
	result.WriteString("E'")
	for i := range len(value) {
		switch value[i] {
		case '\'':
			result.WriteString("''")
		case '\\':
			result.WriteString("\\\\")
		default:
			result.WriteByte(value[i])
		}
	}
	result.WriteByte('\'')
	return result.String(), nil
}
