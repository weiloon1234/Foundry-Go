package lockout

import "github.com/weiloon1234/Foundry-Go/internal/credential"

// Generation is a random window identity. It prevents an expired/reset window
// from being confused with a newly created window that has the same counters.
// This adapter token is not an authentication credential.
type Generation struct{ digest credential.Digest }

func NewGeneration() (Generation, error) {
	_, digest, err := credential.New()
	return Generation{digest}, err
}
func ParseGeneration(text string) (Generation, error) {
	digest, err := credential.ParseDigest(text)
	return Generation{digest}, err
}
func (g Generation) Validate() error  { _, err := credential.ParseDigest(g.Token()); return err }
func (g Generation) Token() string    { return g.digest.Hex() }
func (Generation) String() string     { return "[lockout generation]" }
func (g Generation) GoString() string { return g.String() }

// Snapshot is an admission's immutable adapter observation. It is not a lease or
// permission to skip the final state check. Revision changes on completed failure
// and successful clearing, so a late success cannot erase newer failures.
type Snapshot struct {
	Generation Generation
	Revision   uint32
}

func (s Snapshot) Validate() error { return s.Generation.Validate() }
