package jobs

import (
	"context"
	"fmt"
	"reflect"

	"github.com/weiloon1234/Foundry-Go/encryption"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/identifier"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

// Handler executes one freshly decoded payload. Attempt metadata is available
// through Current; services should be captured by the handler constructor.
type Handler[P any] func(context.Context, P) error

// Definition preserves payload ownership through dispatch and registration.
// Define it once and reuse it across providers; it does not register globally.
type Definition[P any] struct {
	name    Name
	version Version
	policy  Policy
	keyring *encryption.Keyring
}

// Define constructs a typed job definition with an owned snapshot of the policy.
func Define[P any](name Name, version Version, policy Policy) Definition[P] {
	return Definition[P]{name: name, version: version, policy: policy.snapshot()}
}

// Name returns the semantic job name.
func (d Definition[P]) Name() Name { return d.name }

// Version returns the payload schema version.
func (d Definition[P]) Version() Version { return d.version }

// Policy returns an owned policy snapshot, including its backoff slice.
func (d Definition[P]) Policy() Policy { return d.policy.snapshot() }

// Validate checks the name, positive version, concrete payload type, and execution policy.
func (d Definition[P]) Validate() error {
	if !identifier.Semantic(string(d.name)) || d.version == 0 {
		return safeerror.NewFault(fault.Invalid, "job requires a semantic name and nonzero payload version")
	}
	if err := validatePayloadType(reflect.TypeFor[P]()); err != nil {
		return err
	}
	return d.policy.Validate()
}

// Declare associates this schema with a concrete handler. A nil handler is an
// explicit producer-only registration; workers retain such deliveries as failed
// rather than dropping them. Register at most one declaration per name/version.
func (d Definition[P]) Declare(handler Handler[P]) (Declaration, error) {
	return d.DeclareWith(handler, HandlerOptions[P]{})
}
func (d Definition[P]) declaration() Declaration {
	return Declaration{key: jobKey{d.name, d.version}, typ: reflect.TypeFor[P](), policy: d.policy.snapshot()}
}

type jobKey struct {
	name    Name
	version Version
}

// Declaration is the heterogeneous registry boundary built by Definition[P].
// Its fields are private so callers cannot attach a differently typed handler.
type Declaration struct {
	key     jobKey
	typ     reflect.Type
	policy  Policy
	prepare func(context.Context, Envelope) (preparedJob, error)
}

// Format writes a fixed description without exposing handler callbacks.
func (Declaration) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("job declaration")) }

// Registry freezes declarations. It owns no connections, goroutines or payloads.
type Registry struct{ entries map[jobKey]Declaration }

// NewRegistry validates and snapshots declarations, rejecting repeated names and versions.
func NewRegistry(declarations ...Declaration) (*Registry, error) {
	entries := make(map[jobKey]Declaration, len(declarations))
	for _, item := range declarations {
		if item.typ == nil || !identifier.Semantic(string(item.key.name)) || item.key.version == 0 {
			return nil, safeerror.NewFault(fault.Invalid, "invalid job declaration")
		}
		if err := item.policy.Validate(); err != nil {
			return nil, err
		}
		if _, exists := entries[item.key]; exists {
			return nil, safeerror.NewFault(fault.Duplicate, "job name and version are already registered")
		}
		item.policy = item.policy.snapshot()
		entries[item.key] = item
	}
	return &Registry{entries: entries}, nil
}
func (r *Registry) lookup(key jobKey) (Declaration, error) {
	if r == nil || r.entries == nil {
		return Declaration{}, safeerror.NewFault(fault.Invalid, "job registry is not initialized")
	}
	declaration, ok := r.entries[key]
	if !ok {
		return Declaration{}, safeerror.NewFault(fault.Missing, "job name or version is not registered")
	}
	return declaration, nil
}

type payloadError struct{ cause error }

// Error returns a fixed schema-failure message without formatting its cause.
func (*payloadError) Error() string { return "job payload does not match its registered schema" }

// Unwrap returns the payload schema failure cause.
func (e *payloadError) Unwrap() error { return e.cause }

func payloadFailure(cause error) error {
	failure := &payloadError{cause: cause}
	return safeerror.Owned(failure.Error(), failure)
}
