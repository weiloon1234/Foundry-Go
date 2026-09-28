package migrate

import (
	"encoding/hex"
	"strings"

	"github.com/weiloon1234/Foundry-Go/fault"
)

// Checksum is the SHA-256 identity of a complete migration definition.
type Checksum [32]byte

func (c Checksum) String() string               { return hex.EncodeToString(c[:]) }
func (c Checksum) MarshalText() ([]byte, error) { return []byte(c.String()), nil }

// ParseChecksum requires the exact lowercase hexadecimal history representation.
func ParseChecksum(text string) (Checksum, error) {
	var result Checksum
	if len(text) != hex.EncodedLen(len(result)) || text != strings.ToLower(text) {
		return result, fault.New(fault.Invalid, "invalid migration checksum")
	}
	if _, err := hex.Decode(result[:], []byte(text)); err != nil {
		return Checksum{}, fault.Wrap(fault.Invalid, "invalid migration checksum", err)
	}
	return result, nil
}

func (c *Checksum) UnmarshalText(text []byte) error {
	value, err := ParseChecksum(string(text))
	if err != nil {
		return err
	}
	if c == nil {
		return fault.New(fault.Invalid, "nil migration checksum receiver")
	}
	*c = value
	return nil
}
