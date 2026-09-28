package contract

import (
	"encoding/base64"
	"encoding/json"
	"math"
	"strconv"

	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/temporal"
)

func formatValid(format Format) bool {
	switch format {
	case "", UUIDFormat, DecimalFormat, DateFormat, TimeFormat, DateTimeFormat, LocalDateTimeFormat, IntervalFormat, Base64Format:
		return true
	default:
		return false
	}
}

func stringValid(format Format, text string) bool {
	var err error
	switch format {
	case "":
		return true
	case UUIDFormat:
		_, err = model.ParseID[struct{}](text)
	case DecimalFormat:
		_, err = decimal.Parse(text)
	case DateFormat:
		_, err = temporal.ParseDate(text)
	case TimeFormat:
		_, err = temporal.ParseTime(text)
	case DateTimeFormat:
		_, err = temporal.ParseDateTime(text)
	case LocalDateTimeFormat:
		_, err = temporal.ParseLocalDateTime(text)
	case IntervalFormat:
		_, err = temporal.ParseInterval(text)
	case Base64Format:
		var data []byte
		data, err = base64.StdEncoding.Strict().DecodeString(text)
		return err == nil && base64.StdEncoding.EncodeToString(data) == text
	default:
		return false
	}
	return err == nil
}

func scalarValid(typ Type, node any) bool {
	switch typ.Kind {
	case BooleanKind:
		_, ok := node.(bool)
		return ok
	case StringKind:
		text, ok := node.(string)
		return ok && stringValid(typ.Format, text)
	case IntegerKind:
		number, ok := node.(json.Number)
		if !ok {
			return false
		}
		if typ.Signed {
			_, err := strconv.ParseInt(string(number), 10, integerBits(typ))
			return err == nil
		}
		_, err := strconv.ParseUint(string(number), 10, integerBits(typ))
		return err == nil
	case NumberKind:
		number, ok := node.(json.Number)
		if !ok {
			return false
		}
		if typ.Bits == 0 {
			return true
		} // jsonwire already validated the exact number.
		decoded, err := strconv.ParseFloat(string(number), int(typ.Bits))
		return err == nil && !math.IsInf(decoded, 0) && !math.IsNaN(decoded)
	default:
		return false
	}
}

// Integer enum membership follows the Go value, not a JSON spelling: -0 and 0
// denote the same signed integer. String cases retain their exact decoded text.
func scalarCase(typ Type, node any) ([]byte, error) {
	if typ.Kind == IntegerKind {
		number := node.(json.Number)
		if typ.Signed {
			value, err := strconv.ParseInt(string(number), 10, integerBits(typ))
			if err != nil {
				return nil, err
			}
			return []byte(strconv.FormatInt(value, 10)), nil
		}
		value, err := strconv.ParseUint(string(number), 10, integerBits(typ))
		if err != nil {
			return nil, err
		}
		return []byte(strconv.FormatUint(value, 10)), nil
	}
	return json.Marshal(node)
}
