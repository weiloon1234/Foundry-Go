package model

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/sqlname"
	"github.com/weiloon1234/Foundry-Go/internal/sqlvalue"
	"github.com/weiloon1234/Foundry-Go/value"
)

// MaxIdentityKeyBytes bounds the encoded SQL key, including its kind and JSON
// envelope. It limits attribution metadata, not database column capacity.
const MaxIdentityKeyBytes = 4096

// KeyCodec is the small persistence boundary implemented by database codecs.
// Generated references reuse the primary field's existing codec. Custom codecs
// must be pure, safe for concurrent use, and return owned decoded values.
type KeyCodec[K any] interface {
	Bind(K) (driver.Value, error)
	Decode(any) (K, error)
}

// Reference retains a model and its concrete stored primary-key type. Generated
// FoundryReference methods supply its table, key and codec without calling getters.
// A reference does not hydrate a model or establish existence or authorization.
type Reference[M, K any] struct {
	_     [0]*M
	name  string
	key   K
	codec KeyCodec[K]
}

// NewReference is the explicit generated metadata boundary. Applications normally
// obtain references from a model's FoundryReference method rather than repeating
// names or codecs. Identity freezes the key when transport metadata is needed.
func NewReference[M, K any](name string, key K, codec KeyCodec[K]) Reference[M, K] {
	return Reference[M, K]{name: name, key: key, codec: codec}
}
func (r Reference[M, K]) ModelName() string            { return r.name }
func (r Reference[M, K]) Key() K                       { return r.key }
func (Reference[M, K]) Format(state fmt.State, _ rune) { _, _ = state.Write([]byte("model reference")) }

// Validate checks generated model/key metadata without encoding a key or doing I/O.
// The reference may hold a zero key, as it does when used to parse stored identities.
func (r Reference[M, K]) Validate() error { return r.validate() }

func (r Reference[M, K]) validate() error {
	if !sqlname.Table(r.name) || r.codec == nil || reflect.TypeFor[K]().Kind() == reflect.Interface {
		return fault.New(fault.Invalid, "model reference requires a model name and concrete key codec")
	}
	if metadata, ok := r.codec.(interface{ SensitiveValues() bool }); ok && metadata.SensitiveValues() {
		return fault.New(fault.Invalid, "sensitive values cannot be model identity keys")
	}
	return nil
}

// Identity snapshots only the stored key through its database codec. Read
// getters and presentation JSON marshalers do not run. Custom codec failures are
// isolated without exposing panic payloads. SQL NULL is never a valid identity.
func (r Reference[M, K]) Identity() (Identity, error) {
	if err := r.validate(); err != nil {
		return Identity{}, err
	}
	var result Identity
	err := callback.Isolated("snapshot model identity", func() error {
		bound, err := r.codec.Bind(r.key)
		if err != nil {
			return err
		}
		result, err = identityFromDriver(r.name, bound)
		return err
	})
	if err != nil {
		return Identity{}, fault.Wrap(fault.Invalid, "model identity capture failed", err)
	}
	return result, nil
}

// Parse restores a serialized identity using this reference's expected model and
// primary-key codec. Its current key is ignored. At a transport boundary use a
// generated zero model's FoundryReference().Parse(identity); no lookup occurs.
// Parsing validates metadata, never authentication, authorization or existence.
func (r Reference[M, K]) Parse(identity Identity) (Reference[M, K], error) {
	if err := r.validate(); err != nil {
		return Reference[M, K]{}, err
	}
	if err := identity.Validate(); err != nil {
		return Reference[M, K]{}, err
	}
	if r.name != identity.name {
		return Reference[M, K]{}, fault.New(fault.Invalid, "serialized identity does not match the expected model")
	}
	var key K
	err := callback.Isolated("decode model identity", func() error {
		bound, err := identity.key.Decode()
		if err != nil {
			return err
		}
		key, err = r.codec.Decode(bound)
		if err != nil {
			return err
		}
		// Hydration codecs may accept alternate driver representations. An identity
		// must round trip through this codec without changing its persisted meaning.
		rebound, err := r.codec.Bind(key)
		if err != nil {
			return err
		}
		restored, err := identityFromDriver(r.name, rebound)
		if err != nil {
			return err
		}
		if restored != identity {
			return fault.New(fault.Invalid, "identity key does not match its persistence codec")
		}
		return nil
	})
	if err != nil {
		return Reference[M, K]{}, fault.Wrap(fault.Invalid, "model identity decode failed", err)
	}
	return NewReference[M](r.name, key, r.codec), nil
}

// Identity is immutable serialized model attribution. Model/key types are
// erased only at this explicit transport boundary. Received metadata grants no
// authority. Routine formatting omits the key; JSON deliberately exports it.
type Identity struct {
	name string
	key  sqlvalue.Value
}

// Identifiable is implemented by generated model metadata. Framework adapters
// capture the stored identity without another database lookup or an Actor.
type Identifiable interface{ FoundryIdentity() (Identity, error) }

func (i Identity) ModelName() string            { return i.name }
func (i Identity) IsZero() bool                 { return i.name == "" && i.key == (sqlvalue.Value{}) }
func (Identity) Format(state fmt.State, _ rune) { _, _ = state.Write([]byte("model identity")) }

// KeyJSON exports the tagged SQL key for a deliberate storage/transport boundary.
// The kind and exact text preserve integers, bytes and times without JSON number
// rounding. Ordinary application code uses the concrete Reference.Key value.
func (i Identity) KeyJSON() (string, error) {
	if err := i.Validate(); err != nil {
		return "", err
	}
	data, err := json.Marshal(i.key)
	return string(data), err
}

// WithModelName returns the same stored key attributed to another model name.
// It is the explicit boundary for a declared storage identity that predates a
// table rename; parsing still decodes the key through the expected codec.
func (i Identity) WithModelName(name string) (Identity, error) {
	if err := i.Validate(); err != nil {
		return Identity{}, err
	}
	renamed := Identity{name: name, key: i.key}
	if err := renamed.Validate(); err != nil {
		return Identity{}, err
	}
	return renamed, nil
}

func (i Identity) Validate() error {
	if !sqlname.Table(i.name) || len(i.key.Text) > MaxIdentityKeyBytes {
		return invalidIdentity()
	}
	bound, err := i.key.Decode()
	if err != nil {
		return err
	}
	normalized, err := identityFromDriver(i.name, bound)
	if err != nil {
		return err
	}
	if normalized != i {
		return invalidIdentity()
	}
	return nil
}

func invalidIdentity() error { return fault.New(fault.Invalid, "invalid serialized model identity") }

func identityFromDriver(name string, bound driver.Value) (Identity, error) {
	bound = sqlvalue.NormalizeNull(bound)
	if !sqlname.Table(name) || bound == nil {
		return Identity{}, invalidIdentity()
	}
	// Bound allocations before base64 and JSON escaping. Custom codecs own their
	// work before this boundary, just as they do for normal query binding.
	switch v := bound.(type) {
	case string:
		if len(v) > MaxIdentityKeyBytes {
			return Identity{}, invalidIdentity()
		}
	case []byte:
		if len(v) > MaxIdentityKeyBytes {
			return Identity{}, invalidIdentity()
		}
	}
	encoded, err := sqlvalue.Encode(bound)
	if err != nil {
		return Identity{}, err
	}
	// Reject driver values that cannot be restored, such as out-of-range times.
	if _, err := encoded.Decode(); err != nil {
		return Identity{}, err
	}
	data, err := json.Marshal(encoded)
	if err != nil {
		return Identity{}, err
	}
	if len(data) > MaxIdentityKeyBytes {
		return Identity{}, invalidIdentity()
	}
	return Identity{name: name, key: encoded}, nil
}

// ParseIdentity validates an explicit model namespace and tagged SQL key JSON.
// Generated references perform the typed restoration; parsing alone grants no
// trust. Scalar Go JSON and presentation values are not accepted as stored keys.
func ParseIdentity(name, keyJSON string) (Identity, error) {
	if !sqlname.Table(name) || len(keyJSON) == 0 || len(keyJSON) > MaxIdentityKeyBytes {
		return Identity{}, invalidIdentity()
	}
	snapshot, err := value.ParseJSON[sqlvalue.Value](keyJSON)
	if err != nil {
		return Identity{}, err
	}
	encoded, err := snapshot.Decode()
	if err != nil {
		return Identity{}, err
	}
	bound, err := encoded.Decode()
	if err != nil {
		return Identity{}, err
	}
	return identityFromDriver(name, bound)
}

type identityWire struct {
	Model string         `json:"model"`
	Key   sqlvalue.Value `json:"key"`
}

func (i Identity) MarshalJSON() ([]byte, error) {
	if err := i.Validate(); err != nil {
		return nil, err
	}
	return json.Marshal(identityWire{Model: i.name, Key: i.key})
}
func (i *Identity) UnmarshalJSON(data []byte) error {
	if i == nil {
		return fault.New(fault.Invalid, "nil model identity destination")
	}
	if len(data) > MaxIdentityKeyBytes+2*sqlname.MaxBytes+128 {
		return invalidIdentity()
	}
	snapshot, err := value.ParseJSON[identityWire](string(data))
	if err != nil {
		return err
	}
	wire, err := snapshot.Decode()
	if err != nil {
		return err
	}
	if len(wire.Key.Text) > MaxIdentityKeyBytes {
		return invalidIdentity()
	}
	bound, err := wire.Key.Decode()
	if err != nil {
		return err
	}
	decoded, err := identityFromDriver(wire.Model, bound)
	if err != nil {
		return err
	}
	*i = decoded
	return nil
}
