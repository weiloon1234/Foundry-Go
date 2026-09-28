package record

import (
	"encoding/json"
	"fmt"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/jsonwire"
	"github.com/weiloon1234/Foundry-Go/internal/sqlvalue"
	"github.com/weiloon1234/Foundry-Go/value"
)

// State distinguishes missing model snapshots, disclosed SQL values, completely
// redacted values and JSON whose sensitive descendants have been redacted.
// SQL NULL is a disclosed SQL value, distinct from an absent model snapshot.
type State uint8

const (
	Absent State = iota
	Disclosed
	Redacted
	RedactedJSON
)

// Snapshot owns one immutable audit value. It is an audit representation, not a
// model field or response DTO. Format omits content; JSON is an explicit export.
type Snapshot struct {
	state State
	value sqlvalue.Value
}

func (s Snapshot) State() State                 { return s.state }
func (Snapshot) Format(state fmt.State, _ rune) { _, _ = state.Write([]byte("audit snapshot")) }

type snapshotWire struct {
	State State                          `json:"state"`
	Value value.Optional[sqlvalue.Value] `json:"value,omitzero"`
}

func (s Snapshot) wire() snapshotWire {
	wire := snapshotWire{State: s.state}
	if s.state == Disclosed || s.state == RedactedJSON {
		wire.Value = value.Set(s.value)
	}
	return wire
}

func (s Snapshot) validate() error {
	switch s.state {
	case Absent, Redacted:
		if s.value != (sqlvalue.Value{}) {
			return invalidSnapshot()
		}
	case Disclosed, RedactedJSON:
		if len(s.value.Text) > jsonwire.MaxBytes {
			return invalidSnapshot()
		}
		decoded, err := s.value.Decode()
		if err != nil {
			return invalidSnapshot()
		}
		canonical, err := sqlvalue.Encode(decoded)
		if err != nil || canonical != s.value {
			return invalidSnapshot()
		}
		if s.state == RedactedJSON {
			if s.value.Kind != "string" {
				return invalidSnapshot()
			}
			canonical, changed, err := redactJSON(s.value.Text)
			if err != nil || !changed || canonical != s.value.Text {
				return invalidSnapshot()
			}
		}
	default:
		return invalidSnapshot()
	}
	return nil
}

func invalidSnapshot() error { return fault.New(fault.Invalid, "invalid audit snapshot") }

func (s Snapshot) MarshalJSON() ([]byte, error) {
	if err := s.validate(); err != nil {
		return nil, err
	}
	return json.Marshal(s.wire())
}

func (s *Snapshot) UnmarshalJSON(data []byte) error {
	if s == nil {
		return invalidSnapshot()
	}
	parsed, err := value.ParseJSON[snapshotWire](string(data))
	if err != nil {
		return invalidSnapshot()
	}
	wire, err := parsed.Decode()
	if err != nil {
		return invalidSnapshot()
	}
	encoded, present := wire.Value.Get()
	if present != (wire.State == Disclosed || wire.State == RedactedJSON) {
		return invalidSnapshot()
	}
	result := Snapshot{state: wire.State, value: encoded}
	if err := result.validate(); err != nil {
		return err
	}
	*s = result
	return nil
}
