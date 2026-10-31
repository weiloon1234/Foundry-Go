package lockout

import "github.com/weiloon1234/Foundry-Go/internal/credential"

// Generation is a random window identity. It prevents an expired/reset window
// from being confused with a newly created window that has the same counters.
// This adapter token is not an authentication credential.
type Generation struct{ digest credential.Digest }

// NewGeneration creates a random generation digest for a new lockout window.
func NewGeneration() (Generation, error) {
	_, digest, err := credential.New()
	return Generation{digest}, err
}

// ParseGeneration restores a generation from its hexadecimal digest.
func ParseGeneration(text string) (Generation, error) {
	digest, err := credential.ParseDigest(text)
	return Generation{digest}, err
}

// Validate checks that the generation contains a valid digest.
func (g Generation) Validate() error { _, err := credential.ParseDigest(g.Token()); return err }

// Token exposes the generation's hexadecimal adapter representation.
func (g Generation) Token() string { return g.digest.Hex() }

// String returns a generation label without exposing the token.
func (Generation) String() string { return "[lockout generation]" }

// GoString returns the redacted generation label for Go-syntax formatting.
func (g Generation) GoString() string { return g.String() }

// Snapshot is an admission's immutable adapter observation. It is not a lease or
// permission to skip the final state check. Revision changes on completed failure
// and successful clearing, so a late success cannot erase newer failures.
type Snapshot struct {
	Generation Generation
	Revision   uint32
}

// Validate checks the snapshot's generation token.
func (s Snapshot) Validate() error { return s.Generation.Validate() }
