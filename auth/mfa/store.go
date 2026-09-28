package mfa

import (
	"time"

	"github.com/weiloon1234/Foundry-Go/encryption"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/credential"
	"github.com/weiloon1234/Foundry-Go/keyspace"
)

const MaxEnrollmentLifetime = time.Hour
const MaxPrune = 128

type Config struct {
	Namespace          keyspace.Namespace
	Issuer             string
	EnrollmentLifetime time.Duration
	Window             int
	RecoveryCodes      int
	MaxConcurrent      int
	Timeout            time.Duration
}

func DefaultConfig(namespace keyspace.Namespace, issuer string) Config {
	return Config{Namespace: namespace, Issuer: issuer, EnrollmentLifetime: 10 * time.Minute, Window: 1, RecoveryCodes: DefaultRecoveryCodes, MaxConcurrent: 128, Timeout: 15 * time.Second}
}
func (c Config) Validate() error {
	if err := c.Namespace.Validate(); err != nil {
		return err
	}
	if !validLabel(c.Issuer) || c.Window < 0 || c.Window > 1 || c.RecoveryCodes < 1 || c.RecoveryCodes > MaxRecoveryCodes || c.MaxConcurrent < 1 || c.MaxConcurrent > 65536 || c.Timeout <= 0 {
		return fault.New(fault.Invalid, "invalid MFA policy or operation bounds")
	}
	return validateEnrollmentLifetime(c.EnrollmentLifetime)
}
func validateEnrollmentLifetime(lifetime time.Duration) error {
	if lifetime < time.Millisecond || lifetime > MaxEnrollmentLifetime || lifetime%time.Microsecond != 0 {
		return fault.New(fault.Invalid, "invalid MFA enrollment lifetime")
	}
	return nil
}

// Store borrows a transactional backend and immutable encryption keyring. One
// store bounds work across all model bindings. Construction performs no I/O.
type Store struct {
	backend Backend
	keys    *encryption.Keyring
	config  Config
	gate    *credential.Gate
}

func NewStore(backend Backend, keys *encryption.Keyring, config Config) (*Store, error) {
	if credential.IsNil(backend) {
		return nil, fault.New(fault.Invalid, "MFA store requires a backend")
	}
	if err := keys.Validate(); err != nil {
		return nil, err
	}
	if err := config.Validate(); err != nil {
		return nil, err
	}
	return &Store{backend: backend, keys: keys, config: config, gate: credential.NewGate(config.MaxConcurrent, config.Timeout)}, nil
}
func (s *Store) Validate() error {
	if s == nil || s.gate == nil {
		return fault.New(fault.Invalid, "MFA store is not configured")
	}
	return nil
}
