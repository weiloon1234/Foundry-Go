// Package attribution captures immutable human/system and request provenance.
// It carries no authenticated model, credentials, permissions or database handle.
package attribution

import (
	"context"
	"encoding/json"
	"fmt"
	"net/netip"
	"reflect"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/identifier"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

// SystemID identifies infrastructure/domain automation, such as a named schedule.
type SystemID string

// GuardName is the serialized name of an authentication strategy, never a credential.
type GuardName string

// RequestID correlates work with its originating request. It is not a secret.
type RequestID string

const MaxRequestIDBytes = 128
const MaxUserAgentBytes = 4096

// Request is a value snapshot of request metadata. HTTP adapters determine
// trusted client IPs before attribution is captured; this package trusts no headers.
type Request struct {
	ID        RequestID  `json:"id,omitzero"`
	IP        netip.Addr `json:"ip,omitzero"`
	UserAgent string     `json:"user_agent,omitzero"`
}

func (r Request) Validate() error {
	id := string(r.ID)
	if len(id) > MaxRequestIDBytes || !utf8.ValidString(id) || strings.TrimSpace(id) != id || strings.IndexFunc(id, unicode.IsControl) >= 0 {
		return fault.New(fault.Invalid, "invalid attribution request ID")
	}
	if len(r.UserAgent) > MaxUserAgentBytes || !utf8.ValidString(r.UserAgent) || strings.IndexFunc(r.UserAgent, unicode.IsControl) >= 0 {
		return fault.New(fault.Invalid, "invalid attribution user agent")
	}
	if r.IP.Zone() != "" {
		return fault.New(fault.Invalid, "attribution IP cannot include a local interface zone")
	}
	return nil
}

// Origin contains at most one model identity or system identity. Zero is valid
// anonymous provenance. It is metadata, never an authentication/authorization
// capability; security adapters must resolve their own concrete subject.
type Origin struct {
	subject value.Optional[model.Identity]
	system  SystemID
	guard   GuardName
	request Request
}

func (o Origin) Model() (model.Identity, bool) { return o.subject.Get() }
func (o Origin) System() SystemID              { return o.system }
func (o Origin) Guard() GuardName              { return o.guard }
func (o Origin) Request() Request              { return o.request }
func (Origin) Format(state fmt.State, _ rune)  { _, _ = state.Write([]byte("attribution origin")) }

func (o Origin) Validate() error {
	if subject, present := o.subject.Get(); present {
		if o.system != "" {
			return fault.New(fault.Invalid, "attribution cannot combine model and system subjects")
		}
		if err := subject.Validate(); err != nil {
			return err
		}
	}
	if o.system != "" && (!identifier.Semantic(string(o.system)) || o.guard != "") {
		return fault.New(fault.Invalid, "invalid system attribution")
	}
	if o.guard != "" && !identifier.Semantic(string(o.guard)) {
		return fault.New(fault.Invalid, "invalid attribution guard name")
	}
	return o.request.Validate()
}

// WithModel captures the model's generated stored-key identity immediately.
// Mutating the original model later cannot change this origin. No lookup or
// custom read getter runs. A custom metadata adapter must be pure.
func (o Origin) WithModel(subject model.Identifiable) (Origin, error) {
	if subject == nil {
		return Origin{}, fault.New(fault.Invalid, "attribution requires a model")
	}
	rv := reflect.ValueOf(subject)
	switch rv.Kind() {
	case reflect.Pointer, reflect.Interface, reflect.Map, reflect.Slice, reflect.Func, reflect.Chan:
		if rv.IsNil() {
			return Origin{}, fault.New(fault.Invalid, "attribution requires a non-nil model")
		}
	}
	var identity model.Identity
	err := callback.Isolated("capture model attribution", func() error { var err error; identity, err = subject.FoundryIdentity(); return err })
	if err != nil {
		return Origin{}, err
	}
	return o.WithIdentity(identity)
}

// WithIdentity is the explicit restored-metadata boundary. Supplying a parsed
// identity grants no authority and does not authenticate the caller.
func (o Origin) WithIdentity(subject model.Identity) (Origin, error) {
	o.subject = value.Set(subject)
	o.system = ""
	if err := o.Validate(); err != nil {
		return Origin{}, err
	}
	return o, nil
}

// WithSystem replaces the model subject and clears authentication metadata.
func (o Origin) WithSystem(system SystemID) (Origin, error) {
	if system == "" {
		return Origin{}, fault.New(fault.Invalid, "system attribution requires an identifier")
	}
	o.subject = value.Optional[model.Identity]{}
	o.system = system
	o.guard = ""
	if err := o.Validate(); err != nil {
		return Origin{}, err
	}
	return o, nil
}

// WithGuard captures only a strategy name. A system origin cannot use a guard.
func (o Origin) WithGuard(guard GuardName) (Origin, error) {
	o.guard = guard
	if err := o.Validate(); err != nil {
		return Origin{}, err
	}
	return o, nil
}

func (o Origin) WithRequest(request Request) (Origin, error) {
	o.request = request
	if err := o.Validate(); err != nil {
		return Origin{}, err
	}
	return o, nil
}

type contextKey struct{}

// WithContext attaches a validated value snapshot and preserves the caller's
// cancellation. It does not install security credentials or authorization state.
func WithContext(ctx context.Context, origin Origin) (context.Context, error) {
	if ctx == nil {
		return nil, fault.New(fault.Invalid, "attribution requires a context")
	}
	if err := origin.Validate(); err != nil {
		return nil, err
	}
	return context.WithValue(ctx, contextKey{}, origin), nil
}

// FromContext returns anonymous provenance when none is present. Later context
// derivation or model mutation cannot change a previously returned snapshot.
func FromContext(ctx context.Context) Origin {
	if ctx == nil {
		return Origin{}
	}
	origin, _ := ctx.Value(contextKey{}).(Origin)
	return origin
}

type originWire struct {
	Subject value.Optional[model.Identity] `json:"subject,omitzero"`
	System  SystemID                       `json:"system,omitzero"`
	Guard   GuardName                      `json:"guard,omitzero"`
	Request Request                        `json:"request"`
}

func (o Origin) MarshalJSON() ([]byte, error) {
	if err := o.Validate(); err != nil {
		return nil, err
	}
	return json.Marshal(originWire{Subject: o.subject, System: o.system, Guard: o.guard, Request: o.request})
}
func (o *Origin) UnmarshalJSON(data []byte) error {
	if o == nil {
		return fault.New(fault.Invalid, "nil attribution destination")
	}
	encoded, err := value.ParseJSON[originWire](string(data))
	if err != nil {
		return err
	}
	wire, err := encoded.Decode()
	if err != nil {
		return err
	}
	decoded := Origin{subject: wire.Subject, system: wire.System, guard: wire.Guard, request: wire.Request}
	if err := decoded.Validate(); err != nil {
		return err
	}
	*o = decoded
	return nil
}
