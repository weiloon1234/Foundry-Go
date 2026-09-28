package token

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/credential"
)

// Store borrows a backend and shares bounded operation capacity across bindings.
// The application owns the backend pool's lifecycle.
type Store struct {
	backend Backend
	config  Config
	gate    *credential.Gate
}

func NewStore(backend Backend, config Config) (*Store, error) {
	if credential.IsNil(backend) {
		return nil, fault.New(fault.Invalid, "token store requires a backend")
	}
	if err := config.Validate(); err != nil {
		return nil, err
	}
	return &Store{backend: backend, config: config, gate: credential.NewGate(config.MaxConcurrent, config.Timeout)}, nil
}
func (s *Store) validate() error {
	if s == nil || s.gate == nil {
		return fault.New(fault.Invalid, "tokens require a bound store")
	}
	return nil
}
func (s *Store) execute(ctx context.Context, fn func(context.Context) error) error {
	if err := s.validate(); err != nil {
		return err
	}
	return s.gate.Execute(ctx, fn)
}
