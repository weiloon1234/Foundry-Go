package challenge

import (
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/credential"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

// Store borrows its backend and bounds callback capacity across every flow.
type Store struct {
	backend Backend
	config  Config
	gate    *credential.Gate
}

func NewStore(backend Backend, config Config) (*Store, error) {
	if credential.IsNil(backend) {
		return nil, safeerror.NewFault(fault.Invalid, "challenge store requires a backend")
	}
	if err := config.Validate(); err != nil {
		return nil, err
	}
	return &Store{backend: backend, config: config, gate: credential.NewGate(config.MaxConcurrent, config.Timeout)}, nil
}
func (s *Store) Validate() error {
	if s == nil || s.gate == nil {
		return safeerror.NewFault(fault.Invalid, "challenge store is not configured")
	}
	return nil
}
