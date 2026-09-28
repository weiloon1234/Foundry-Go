package raw

import (
	"strconv"
	"strings"

	"github.com/weiloon1234/Foundry-Go/fault"
)

// Argument snapshots one explicit raw value. Zero is invalid; empty Text/Bytes
// arguments are valid. Use Command.Key for additional keys, not Text(key.String()).
type Argument struct {
	text  string
	key   Key
	valid bool
}

func Text(text string) Argument {
	if len(text) > MaxArgumentBytes {
		return Argument{}
	}
	return Argument{text: strings.Clone(text), valid: true}
}
func Bytes(data []byte) Argument {
	if len(data) > MaxArgumentBytes {
		return Argument{}
	}
	return Argument{text: string(data), valid: true}
}
func Int64(value int64) Argument   { return Text(strconv.FormatInt(value, 10)) }
func Uint64(value uint64) Argument { return Text(strconv.FormatUint(value, 10)) }
func (a Argument) validate() error {
	if !a.valid {
		return fault.New(fault.Invalid, "invalid or oversized raw Redis argument")
	}
	return nil
}
func keyArgument(key Key) Argument {
	if key.Validate() != nil {
		return Argument{}
	}
	return Argument{text: key.String(), key: key, valid: true}
}
