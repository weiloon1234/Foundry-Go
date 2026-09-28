package cache

import (
	"sync"

	"github.com/weiloon1234/Foundry-Go/fault"
)

// Store binds cache declarations to one namespace and a borrowed Backend.
// NewStore is pure construction: it performs no I/O and starts no goroutines.
// Each application builds its own Store and owns the adapter lifecycle.
// TaggedBackend adapters automatically protect every value/counter operation with
// a reserved namespace generation, enabling Invalidate across stores and processes.
// Basic Backend adapters remain usable but cannot invalidate a namespace.
type Store struct {
	coordination *coordinator
	backend      Backend
	config       Config
	mu           sync.Mutex
	declarations map[Name]*declarationID
	fills        map[EntryKey]*fill
}

func NewStore(backend Backend, config Config) (*Store, error) {
	if backend == nil {
		return nil, fault.New(fault.Invalid, "cache store requires a backend")
	}
	if err := config.Validate(); err != nil {
		return nil, err
	}
	return &Store{backend: backend, config: config, declarations: make(map[Name]*declarationID), fills: make(map[EntryKey]*fill)}, nil
}
func (s *Store) Namespace() Namespace { return s.config.Namespace }
