package challenge

import (
	"fmt"
	"unicode/utf8"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/credential"
	"github.com/weiloon1234/Foundry-Go/secret"
)

// Binding fingerprints the exact account state relevant to a link. Password
// reset includes the current email AND stored hash; verification includes the
// current email. Recovery flows also include a persisted Revision, so restoring
// an old address cannot revive old links. Bind alone compares current values.
// Bindings are not passwords or standalone authenticators. Keep them private.
type Binding struct{ hash credential.Digest }

func Bind(parts ...secret.String) (Binding, error) {
	if len(parts) < 1 || len(parts) > 8 {
		return Binding{}, fault.New(fault.Invalid, "invalid challenge binding components")
	}
	values := make([]string, len(parts))
	for i, part := range parts {
		raw := part.Reveal()
		if len(raw) == 0 || len(raw) > 4096 || !utf8.ValidString(raw) {
			return Binding{}, fault.New(fault.Invalid, "invalid challenge binding component")
		}
		values[i] = raw
	}
	return Binding{hash: credential.Fingerprint(values...)}, nil
}
func (Binding) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("challenge binding")) }
func (b Binding) Hex() string              { return b.hash.Hex() }
func (b Binding) IsZero() bool             { return b.hash.IsZero() }
func (b Binding) Equal(other Binding) bool { return b.hash.Equal(other.hash) }
func ParseBinding(text string) (Binding, error) {
	hash, err := credential.ParseDigest(text)
	return Binding{hash: hash}, err
}
