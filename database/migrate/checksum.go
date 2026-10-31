package migrate

import (
	"encoding/hex"
	"strings"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

// Checksum is the SHA-256 identity of a complete migration definition.
type Checksum [32]byte

// String returns the checksum's lowercase hexadecimal representation.
func (c Checksum) String() string { return hex.EncodeToString(c[:]) }

// MarshalText returns the checksum's lowercase hexadecimal bytes.
func (c Checksum) MarshalText() ([]byte, error) { return []byte(c.String()), nil }

// ParseChecksum requires the exact lowercase hexadecimal history representation.
func ParseChecksum(text string) (Checksum, error) {
	var result Checksum
	if len(text) != hex.EncodedLen(len(result)) || text != strings.ToLower(text) {
		return result, safeerror.NewFault(fault.Invalid, "invalid migration checksum")
	}
	if _, err := hex.Decode(result[:], []byte(text)); err != nil {
		return Checksum{}, safeerror.WrapFault(fault.Invalid, "invalid migration checksum", err)
	}
	return result, nil
}

// UnmarshalText parses the exact lowercase hexadecimal representation and replaces the checksum on success.
// A nil receiver returns fault.Invalid.
func (c *Checksum) UnmarshalText(text []byte) error {
	value, err := ParseChecksum(string(text))
	if err != nil {
		return err
	}
	if c == nil {
		return safeerror.NewFault(fault.Invalid, "nil migration checksum receiver")
	}
	*c = value
	return nil
}
