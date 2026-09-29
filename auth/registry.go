package auth

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"sync"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/internal/admission"
)

type declarationID struct{ marker byte } // Nonzero size: independent identities cannot alias.
type registrationKind uint8

const (
	guardRegistration registrationKind = iota + 1
	policyRegistration
	hookRegistration
)

// Registration is the erased assembly boundary, never a dynamic subject lookup.
// Obtain registrations from typed guards and policies; zero is invalid.
type Registration struct {
	owner        foundation.ProviderID
	kind         registrationKind
	id           *declarationID
	name         string
	providerID   *declarationID
	providerName ProviderName
	validate     func() error
	hook         beforeHook
}

// Config bounds concurrent callbacks across all scopes owned by this registry.
// Timeout covers each verification/provider resolution or policy evaluation.
// A burst queues for at most min(Timeout, 5s) before the budget starts; an
// unsatisfied wait reports fault.Overloaded. A callback that ignores
// cancellation still owns its slot until actual exit.
type Config struct {
	MaxConcurrent int
	Timeout       time.Duration
}

func DefaultConfig() Config { return Config{MaxConcurrent: 128, Timeout: 5 * time.Second} }
func (c Config) Validate() error {
	if c.MaxConcurrent < 1 || c.MaxConcurrent > 65536 || c.Timeout <= 0 {
		return fault.New(fault.Invalid, "invalid authentication registry limits")
	}
	return nil
}

const MaxRegistrations = 256

// Registry owns immutable explicit guard/policy declarations and operation
// capacity. It stores no process-global or cross-request authenticated model.
type Registry struct {
	config    Config
	guards    map[*declarationID]bool
	policies  map[*declarationID]bool
	hooks     []beforeHook
	names     map[registrationKind]map[string]foundation.ProviderID
	providers map[ProviderName]providerOwner
	count     int
	slots     *admission.Semaphore
}
type providerOwner struct {
	id    *declarationID
	owner foundation.ProviderID
}

func NewRegistry(c Config, registrations ...Registration) (*Registry, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	r := &Registry{config: c, guards: make(map[*declarationID]bool), policies: make(map[*declarationID]bool), names: make(map[registrationKind]map[string]foundation.ProviderID), providers: make(map[ProviderName]providerOwner), slots: admission.New(c.MaxConcurrent)}
	if err := r.add(registrations); err != nil {
		return nil, err
	}
	return r, nil
}

// With returns a new immutable registry containing this registry's declarations
// plus registrations. It shares the receiver's configuration and callback
// capacity, so derived registries never multiply the application's concurrency
// bound. Names and provider identities are validated against every inherited
// declaration; the receiver itself is never modified.
func (r *Registry) With(registrations ...Registration) (*Registry, error) {
	if err := r.Validate(); err != nil {
		return nil, err
	}
	derived := &Registry{config: r.config, guards: maps.Clone(r.guards), policies: maps.Clone(r.policies), hooks: slices.Clone(r.hooks), names: make(map[registrationKind]map[string]foundation.ProviderID, len(r.names)), providers: maps.Clone(r.providers), count: r.count, slots: r.slots}
	for kind, names := range r.names {
		derived.names[kind] = maps.Clone(names)
	}
	if err := derived.add(registrations); err != nil {
		return nil, err
	}
	return derived, nil
}

func (r *Registry) add(registrations []Registration) error {
	if len(registrations) > MaxRegistrations-r.count {
		return fault.New(fault.Invalid, "authentication registration capacity exceeded")
	}
	for _, item := range registrations {
		if item.id == nil || item.validate == nil || (item.kind != guardRegistration && item.kind != policyRegistration && item.kind != hookRegistration) {
			return fault.New(fault.Invalid, "invalid authentication registration")
		}
		if err := item.validate(); err != nil {
			return err
		}
		if r.names[item.kind] == nil {
			r.names[item.kind] = make(map[string]foundation.ProviderID)
		}
		if previous, exists := r.names[item.kind][item.name]; exists {
			return duplicateAuthorization("authentication declaration name is repeated", previous, item.owner)
		}
		r.names[item.kind][item.name] = item.owner
		switch item.kind {
		case guardRegistration:
			if old, ok := r.providers[item.providerName]; ok && old.id != item.providerID {
				return duplicateAuthorization("authentication provider name has different declarations", old.owner, item.owner)
			}
			r.providers[item.providerName] = providerOwner{item.providerID, item.owner}
			r.guards[item.id] = true
		case policyRegistration:
			r.policies[item.id] = true
		default:
			r.hooks = append(r.hooks, item.hook)
		}
		r.count++
	}
	return nil
}

func duplicateAuthorization(message string, previous, owner foundation.ProviderID) error {
	if previous != "" || owner != "" {
		if previous == "" {
			previous = "application"
		}
		if owner == "" {
			owner = "application"
		}
		message += fmt.Sprintf("; belongs to both %s and %s", previous, owner)
	}
	return fault.New(fault.Duplicate, message)
}
func (r *Registry) Validate() error {
	if r == nil || r.slots == nil || r.guards == nil {
		return fault.New(fault.Invalid, "authentication requires a registry")
	}
	return nil
}

// Scope owns credentials and coalesced guard results for one request or explicit
// authorization boundary. New scopes reverify credentials and load current state.
// Never retain a scope for a job or a WebSocket connection. Close at its boundary.
type Scope struct {
	registry    *Registry
	ctx         context.Context
	cancel      context.CancelFunc
	credentials Credentials
	mu          sync.Mutex
	closed      bool
	active      sync.WaitGroup
	guards      map[*declarationID]*resolutionEntry
}
type resolutionEntry struct {
	done  chan struct{}
	value any
	err   error
}

func (r *Registry) NewScope(ctx context.Context, credentials Credentials) (*Scope, error) {
	if err := r.Validate(); err != nil {
		return nil, err
	}
	if ctx == nil {
		return nil, fault.New(fault.Invalid, "authentication scope requires a context")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	owned, cancel := context.WithCancel(ctx)
	scope := &Scope{registry: r, cancel: cancel, credentials: credentials, guards: make(map[*declarationID]*resolutionEntry)}
	scope.ctx = context.WithValue(owned, scopeKey{}, scope)
	return scope, nil
}

// Close cancels and waits for callbacks started in this scope, then releases
// credential and model references. It is idempotent. Do not call it from an
// authentication callback belonging to this scope: that callback must exit first.
func (s *Scope) Close() error {
	if s == nil || s.cancel == nil {
		return nil
	}
	s.mu.Lock()
	s.closed = true
	s.cancel()
	s.mu.Unlock()
	s.active.Wait()
	s.mu.Lock()
	s.credentials = Credentials{}
	s.guards = nil
	s.mu.Unlock()
	return nil
}

// Context carries this scope's cancellation and private authentication binding.
// Derive downstream contexts from it; a fresh NewScope replaces inherited
// authentication state. A zero or nil scope returns nil.
func (s *Scope) Context() context.Context {
	if s == nil {
		return nil
	}
	return s.ctx
}

type scopeKey struct{}

func currentScope(ctx context.Context) (*Scope, error) {
	if ctx == nil {
		return nil, fault.New(fault.Invalid, "authentication requires a context")
	}
	scope, _ := ctx.Value(scopeKey{}).(*Scope)
	if scope == nil {
		return nil, fault.New(fault.Missing, "context has no authentication scope")
	}
	if err := scope.check(ctx); err != nil {
		return nil, err
	}
	return scope, nil
}
