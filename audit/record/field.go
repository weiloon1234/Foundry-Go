package record

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/jsonwire"
	"github.com/weiloon1234/Foundry-Go/internal/sqlname"
	"github.com/weiloon1234/Foundry-Go/internal/sqlvalue"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Field is an opaque, model-owned captured field for generated audit adapters.
// Its constructor is a declaration boundary; normal callers use model Changes.
type Field[M any] struct {
	_             [0]*M
	wire          fieldWire
	before, after bool
	excluded      bool
	bytes         int
}

func (Field[M]) Format(state fmt.State, _ rune) { _, _ = state.Write([]byte("audit field")) }

type fieldWire struct {
	Name     string              `json:"name"`
	Type     codec.ParameterType `json:"type"`
	Assigned bool                `json:"assigned"`
	Changed  bool                `json:"changed"`
	Before   Snapshot            `json:"before"`
	After    Snapshot            `json:"after"`
}

// CaptureField consumes the already-computed stored change. It never compares
// values again or invokes model presentation accessors. Excluded/redacted fields
// do not invoke their persistence codec at all. Codecs must be pure and return
// owned values, as for ordinary persistence; custom callback faults are isolated.
func CaptureField[M, V any](name string, c codec.Codec[V], change lifecycle.FieldChange[V], disclosure Disclosure) (Field[M], error) {
	if !sqlname.Valid(name) {
		return Field[M]{}, fault.New(fault.Invalid, "audit field requires a declared column")
	}
	if err := disclosure.Validate(); err != nil {
		return Field[M]{}, err
	}
	before, after := change.Before().IsSet(), change.After().IsSet()
	if !before && !after || change.Assigned() && !after {
		return Field[M]{}, fault.New(fault.Invalid, "audit field requires a stored change")
	}
	result := Field[M]{wire: fieldWire{Name: name, Type: c.ParameterType(), Assigned: change.Assigned(), Changed: change.Changed()},
		before: before, after: after, excluded: disclosure == Exclude}
	if result.excluded {
		return result, nil
	}
	if disclosure == Redact || c.SensitiveValues() || SensitiveName(name) {
		if before {
			result.wire.Before.state = Redacted
		}
		if after {
			result.wire.After.state = Redacted
		}
	} else {
		err := callback.Isolated("capture audit field", func() error {
			var err error
			result.wire.Before, err = captureSnapshot(c, change.Before())
			if err != nil {
				return err
			}
			result.wire.After, err = captureSnapshot(c, change.After())
			return err
		})
		if err != nil {
			return Field[M]{}, fault.Wrap(fault.Invalid, "audit field capture failed", err)
		}
	}
	encoded, err := json.Marshal(result.wire)
	if err != nil || len(encoded) > jsonwire.MaxBytes {
		return Field[M]{}, fault.New(fault.Invalid, "audit field exceeds its representation bound")
	}
	result.bytes = len(encoded)
	return result, nil
}

func captureSnapshot[V any](c codec.Codec[V], input value.Optional[V]) (Snapshot, error) {
	item, present := input.Get()
	if !present {
		return Snapshot{}, nil
	}
	bound, err := c.Bind(item)
	if err != nil {
		return Snapshot{}, err
	}
	if err := boundValue(bound); err != nil {
		return Snapshot{}, err
	}
	state := Disclosed
	if bound != nil && c.ParameterType() == codec.TypeJSON {
		text, ok := bound.(string)
		if !ok {
			return Snapshot{}, invalidSnapshot()
		}
		text, changed, err := redactJSON(text)
		if err != nil {
			return Snapshot{}, err
		}
		bound = text
		if changed {
			state = RedactedJSON
		}
	}
	encoded, err := sqlvalue.Encode(bound)
	if err != nil {
		return Snapshot{}, err
	}
	result := Snapshot{state: state, value: encoded}
	return result, result.validate()
}

func boundValue(bound driver.Value) error {
	switch v := bound.(type) {
	case string:
		if len(v) > jsonwire.MaxBytes {
			return invalidSnapshot()
		}
	case []byte:
		if len(v) > jsonwire.MaxBytes*3/4 {
			return invalidSnapshot()
		}
	}
	return nil
}

const redactedJSONValue = "[redacted]"

func redactJSON(text string) (string, bool, error) {
	canonical, tree, err := jsonwire.Parse([]byte(text))
	if err != nil {
		return "", false, err
	}
	if !redactTree(tree) {
		return canonical, false, nil
	}
	encoded, err := json.Marshal(tree)
	if err != nil || len(encoded) > jsonwire.MaxBytes {
		return "", false, invalidSnapshot()
	}
	return string(encoded), true, nil
}

func redactTree(tree any) bool {
	changed := false
	switch node := tree.(type) {
	case map[string]any:
		for name, child := range node {
			if SensitiveName(name) {
				node[name] = redactedJSONValue
				changed = true
			} else if redactTree(child) {
				changed = true
			}
		}
	case []any:
		for _, child := range node {
			if redactTree(child) {
				changed = true
			}
		}
	}
	return changed
}
