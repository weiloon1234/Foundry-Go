package jobs

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/weiloon1234/Foundry-Go/attribution"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/identifier"
	"github.com/weiloon1234/Foundry-Go/internal/jsonwire"
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
	EnvelopeVersion EnvelopeVersion                 `json:"envelope_version,omitempty"`
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

func (e Envelope) Target() Target             { return Target{ID: e.ID(), Name: e.Name(), Version: e.Version()} }
func (e Envelope) ID() ExecutionID            { return e.wire.ID }
func (e Envelope) Name() Name                 { return e.wire.Name }
func (e Envelope) Version() Version           { return e.wire.Version }
func (e Envelope) Uniqueness() Uniqueness     { return e.wire.Unique }
func (e Envelope) Queue() Queue               { return e.wire.Policy.Queue }
func (e Envelope) Policy() Policy             { return e.wire.Policy.snapshot() }
func (e Envelope) AvailableAt() time.Time     { return e.wire.AvailableAt }
func (e Envelope) Origin() attribution.Origin { return e.wire.Origin }

// Encrypted reports a sealed payload: PayloadJSON is then a JSON string of
// ciphertext, decrypted by the definition (Definition.Payload).
func (e Envelope) Encrypted() bool { return e.wire.Encrypted }

// EnvelopeVersion identifies the transport format independently of a job's
// payload Version. LegacyEnvelope omits new fields for older strict readers.
// ExtendedEnvelope (3) is selected automatically when an envelope uses a field
// older readers reject: Policy.MaxExceptions, Policy.RetryUntil,
// Unique.UntilProcessing or payload encryption. Its trace is optional. Envelopes
// without those fields keep the legacy or traced format. Upgrade every worker,
// publisher and inspection tool before dispatching extended envelopes.
type EnvelopeVersion uint8

const (
	LegacyEnvelope   EnvelopeVersion = 1
	TracedEnvelope   EnvelopeVersion = 2
	ExtendedEnvelope EnvelopeVersion = 3
)

// extended reports whether the envelope carries fields that need format 3.
func (w envelopeWire) extended() bool {
	return w.Policy.extended() || w.Unique.UntilProcessing || w.Encrypted
}

func (e Envelope) WireVersion() EnvelopeVersion {
	if e.wire.EnvelopeVersion == 0 {
		return LegacyEnvelope
	}
	return e.wire.EnvelopeVersion
}
func (e Envelope) Trace() value.Optional[tracing.Context] { return e.wire.Trace }

// PayloadJSON is an explicit inspection boundary; payloads may contain personal
// data. Default formatting deliberately omits payload and attribution values.
func (e Envelope) PayloadJSON() string      { return e.payload }
func (Envelope) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("job envelope")) }
func (e Envelope) Validate() error {
	switch e.wire.EnvelopeVersion {
	case 0:
		if e.wire.Trace.IsSet() {
			return fault.New(fault.Invalid, "legacy job envelope cannot contain trace context")
		}
	case TracedEnvelope:
		if !e.wire.Trace.IsSet() {
			return fault.New(fault.Invalid, "traced job envelope requires trace context")
		}
	case ExtendedEnvelope:
		if !e.wire.extended() {
			return fault.New(fault.Invalid, "extended job envelope carries no extended field")
		}
	default:
		return fault.New(fault.Invalid, "unsupported job envelope transport version")
	}
	if e.wire.EnvelopeVersion != ExtendedEnvelope && e.wire.extended() {
		return fault.New(fault.Invalid, "job envelope fields require the extended transport format")
	}
	if trace, present := e.wire.Trace.Get(); present {
		if err := trace.Validate(); err != nil {
			return err
		}
	}
	if e.ID().IsZero() || !identifier.Semantic(string(e.Name())) || e.Version() == 0 || len(e.payload) == 0 || len(e.payload) > MaxPayloadBytes {
		return fault.New(fault.Invalid, "invalid job envelope identity or payload bounds")
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
		return fault.New(fault.Invalid, "invalid job scheduled time")
	}
	return nil
}
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
		return Envelope{}, fault.New(fault.Invalid, "job envelope exceeds transport bounds")
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
