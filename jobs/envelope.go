package jobs

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/weiloon1234/Foundry-Go/attribution"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/identifier"
	"github.com/weiloon1234/Foundry-Go/internal/jsonwire"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"github.com/weiloon1234/Foundry-Go/tracing"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Envelope is an immutable adapter snapshot. Ordinary consumers use Definition
// dispatch methods; explicit transport codecs use MarshalJSON/DecodeEnvelope.
// It never contains a handler, model instance, request context or service graph.
type Envelope struct {
	wire    envelopeWire
	payload string
}
type envelopeWire struct {
	EnvelopeVersion EnvelopeVersion                 `json:"envelope_version"`
	Trace           value.Optional[tracing.Context] `json:"trace,omitzero"`
	ID              ExecutionID                     `json:"id"`
	Name            Name                            `json:"name"`
	Version         Version                         `json:"version"`
	Policy          Policy                          `json:"policy"`
	AvailableAt     time.Time                       `json:"available_at,omitzero"`
	Origin          attribution.Origin              `json:"origin"`
	Unique          Uniqueness                      `json:"unique,omitzero"`
	Encrypted       bool                            `json:"encrypted,omitzero"`
	Payload         json.RawMessage                 `json:"payload"`
}

// Target returns the dispatch identity, name, and version used for exact cancellation matching.
func (e Envelope) Target() Target { return Target{ID: e.ID(), Name: e.Name(), Version: e.Version()} }

// ID returns the stable heterogeneous dispatch identity.
func (e Envelope) ID() ExecutionID { return e.wire.ID }

// Name returns the job name.
func (e Envelope) Name() Name { return e.wire.Name }

// Version returns the job payload schema version.
func (e Envelope) Version() Version { return e.wire.Version }

// Uniqueness returns the immutable admission deduplication policy.
func (e Envelope) Uniqueness() Uniqueness { return e.wire.Unique }

// Queue returns the captured routing destination.
func (e Envelope) Queue() Queue { return e.wire.Policy.Queue }

// Policy returns an owned execution policy snapshot.
func (e Envelope) Policy() Policy { return e.wire.Policy.snapshot() }

// AvailableAt returns the captured eligibility time. Zero means immediately eligible.
func (e Envelope) AvailableAt() time.Time { return e.wire.AvailableAt }

// Origin returns the attribution captured for this dispatch.
func (e Envelope) Origin() attribution.Origin { return e.wire.Origin }

// Encrypted reports a sealed payload: PayloadJSON is then a JSON string of
// ciphertext, decrypted by the definition (Definition.Payload).
func (e Envelope) Encrypted() bool { return e.wire.Encrypted }

// EnvelopeVersion identifies the transport format independently of a job's payload Version.
type EnvelopeVersion uint8

// CurrentEnvelopeVersion is the only supported job transport format.
const CurrentEnvelopeVersion EnvelopeVersion = 3

// WireVersion returns the explicit transport format.
func (e Envelope) WireVersion() EnvelopeVersion { return e.wire.EnvelopeVersion }

// Trace returns the optional captured trace context.
func (e Envelope) Trace() value.Optional[tracing.Context] { return e.wire.Trace }

// PayloadJSON is an explicit inspection boundary; payloads may contain personal
// data. Default formatting deliberately omits payload and attribution values.
func (e Envelope) PayloadJSON() string { return e.payload }

// Format writes a fixed description without exposing payload or attribution.
func (Envelope) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("job envelope")) }

// Validate checks transport format, identity, payload bounds, policy, attribution, and eligibility time.
func (e Envelope) Validate() error {
	if e.wire.EnvelopeVersion != CurrentEnvelopeVersion {
		return safeerror.NewFault(fault.Invalid, "unsupported job envelope transport version")
	}
	if trace, present := e.wire.Trace.Get(); present {
		if err := trace.Validate(); err != nil {
			return err
		}
	}
	if e.ID().IsZero() || !identifier.Semantic(string(e.Name())) || e.Version() == 0 || len(e.payload) == 0 || len(e.payload) > MaxPayloadBytes {
		return safeerror.NewFault(fault.Invalid, "invalid job envelope identity or payload bounds")
	}
	if err := e.wire.Unique.Validate(); err != nil {
		return err
	}
	if err := e.wire.Policy.Validate(); err != nil {
		return err
	}
	if err := e.wire.Origin.Validate(); err != nil {
		return err
	}
	if !e.AvailableAt().IsZero() && (e.AvailableAt().Year() < 1970 || e.AvailableAt().Year() > 9999) {
		return safeerror.NewFault(fault.Invalid, "invalid job scheduled time")
	}
	return nil
}

// MarshalJSON validates and encodes the envelope within transport byte and JSON structure bounds.
func (e Envelope) MarshalJSON() ([]byte, error) {
	if err := e.Validate(); err != nil {
		return nil, err
	}
	wire := e.wire
	wire.Payload = json.RawMessage(e.payload)
	data, err := json.Marshal(wire)
	if err != nil {
		return nil, err
	}
	if _, err := jsonwire.Decode(data, jsonwire.Limits{Bytes: MaxEnvelopeBytes, Depth: value.JSONMaxDepth, Nodes: value.JSONMaxNodes}); err != nil {
		return nil, err
	}
	return data, nil
}

// DecodeEnvelope bounds and validates transport input. Unknown job versions are
// structurally valid: a worker records them as failed with an inspectable reason.
func DecodeEnvelope(data []byte) (Envelope, error) {
	if len(data) > MaxEnvelopeBytes {
		return Envelope{}, safeerror.NewFault(fault.Invalid, "job envelope exceeds transport bounds")
	}
	captured, err := value.ParseJSON[envelopeWire](string(data))
	if err != nil {
		return Envelope{}, err
	}
	wire, err := captured.Decode()
	if err != nil {
		return Envelope{}, err
	}
	body, err := value.ParseJSON[json.RawMessage](string(wire.Payload))
	if err != nil {
		return Envelope{}, err
	}
	payload, err := body.Text()
	if err != nil {
		return Envelope{}, err
	}
	wire.Payload = nil
	wire.Policy = wire.Policy.snapshot()
	wire.AvailableAt = wire.AvailableAt.UTC()
	envelope := Envelope{wire: wire, payload: payload}
	return envelope, envelope.Validate()
}
