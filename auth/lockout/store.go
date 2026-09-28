package lockout

import (
	"sync"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/credential"
	"github.com/weiloon1234/Foundry-Go/keyspace"
)

// Config bounds all key encoding, backend calls and credential callbacks held by
// a borrowing store. Timeout must fit the intended verification work. A callback
// retaining control after cancellation also retains its concurrency slot.
type Config struct {
	Namespace                                   keyspace.Namespace
	MaxKeyBytes, MaxDeclarations, MaxConcurrent int
	Timeout                                     time.Duration
}

func DefaultConfig(namespace keyspace.Namespace) Config {
	return Config{Namespace: namespace, MaxKeyBytes: 1024, MaxDeclarations: 256, MaxConcurrent: 128, Timeout: 10 * time.Second}
}
func (c Config) Validate() error {
	if err := c.Namespace.Validate(); err != nil {
		return err
	}
	if c.MaxKeyBytes < 1 || c.MaxKeyBytes > keyspace.MaxKeyBytes || c.MaxDeclarations < 1 || c.MaxDeclarations > 65536 || c.MaxConcurrent < 1 || c.MaxConcurrent > 65536 || c.Timeout <= 0 {
		return fault.New(fault.Invalid, "invalid lockout store bounds")
	}
	return nil
}

// Store borrows a backend without I/O or background work. One store may bind
// distinct password, MFA and recovery declarations; names isolate their policy.
type Store struct {
	backend      Backend
	config       Config
	gate         *credential.Gate
	mu           sync.Mutex
	declarations map[Name]*declarationID
}

func NewStore(backend Backend, config Config) (*Store, error) {
	if credential.IsNil(backend) {
		return nil, fault.New(fault.Invalid, "lockout requires a backend")
	}
	if err := config.Validate(); err != nil {
		return nil, err
	}
	return &Store{backend: backend, config: config, gate: credential.NewGate(config.MaxConcurrent, config.Timeout), declarations: make(map[Name]*declarationID)}, nil
}
func (s *Store) Namespace() keyspace.Namespace {
	if s == nil {
		return keyspace.Namespace{}
	}
	return s.config.Namespace
}
