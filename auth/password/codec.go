package password

import (
	"database/sql/driver"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/secret"
)

// Codec is the shared generated-model persistence boundary. PostgreSQL stores
// canonical PHC text; Go queries, drafts and hydration retain Hash. A plain or
// malformed string never becomes a hash by an implicit conversion. SQL NULL
// requires value.Nullable[Hash]. Automatic audit/cursor/identity disclosure is
// disabled by the codec, independently of the column's name.
func Codec() codec.Codec[Hash] {
	return codec.New(func(h Hash) (driver.Value, error) {
		if err := h.Validate(); err != nil {
			return nil, err
		}
		return h.encoded.Reveal(), nil
	}, func(source any) (Hash, error) {
		raw, err := codec.String[string]().Decode(source)
		if err != nil {
			return Hash{}, invalidHash()
		}
		return ParseHash(secret.New(raw))
	}).WithParameterType(codec.TypeText).WithSensitiveValues()
}

// Value is an explicit database/sql persistence boundary, not a display value.
func (h Hash) Value() (driver.Value, error) { return Codec().Bind(h) }

// Scan changes the destination only after a complete valid PHC decode.
func (h *Hash) Scan(source any) error { return Codec().Scan(h).Scan(source) }
