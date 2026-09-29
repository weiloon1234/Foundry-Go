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

// MaxCapturedValueBytes bounds one captured value before the record-level
// payload bound applies. Larger values are stored as an Oversized digest; the
// recorder's configured value limit can replace smaller values the same way.
const MaxCapturedValueBytes = jsonwire.MaxBytes / 4

// Field is an opaque, model-owned captured field for generated audit adapters.
// Its constructor is a declaration boundary; normal callers use model Changes.
type Field[M any] struct {
	_             [0]*M
	field         capturedField
	before, after bool
	excluded      bool
}

func (Field[M]) Format(state fmt.State, _ rune) { _, _ = state.Write([]byte("audit field")) }

type fieldWire struct {
	Name     string              `json:"name"`
	Type     codec.ParameterType `json:"type"`
	Assigned bool                `json:"assigned"`
	Changed  bool                `json:"changed"`
	Before   snapshotWire        `json:"before"`
	After    snapshotWire        `json:"after"`
}

// capturedField is the validated in-memory form of fieldWire.
type capturedField struct {
	name          string
	typ           codec.ParameterType
	assigned      bool
	changed       bool
	before, after Snapshot
}

func (f capturedField) wire() fieldWire {
	return fieldWire{Name: f.name, Type: f.typ, Assigned: f.assigned, Changed: f.changed, Before: f.before.wire(), After: f.after.wire()}
}

func fieldFromWire(wire fieldWire) (capturedField, error) {
	before, err := snapshotFromWire(wire.Before)
	if err != nil {
		return capturedField{}, err
	}
	after, err := snapshotFromWire(wire.After)
	if err != nil {
		return capturedField{}, err
	}
	return capturedField{name: wire.Name, typ: wire.Type, assigned: wire.Assigned, changed: wire.Changed, before: before, after: after}, nil
}

// CaptureField consumes the already-computed stored change. It never compares
// values again or invokes model presentation accessors. Excluded/redacted fields
// do not invoke their persistence codec at all. Codecs must be pure and return
// owned values, as for ordinary persistence; custom callback faults are isolated.
// Values above MaxCapturedValueBytes become Oversized digests rather than
// failing the business write.
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
	result := Field[M]{field: capturedField{name: name, typ: c.ParameterType(), assigned: change.Assigned(), changed: change.Changed()},
		before: before, after: after, excluded: disclosure == Exclude}
	if result.excluded {
		return result, nil
	}
	if disclosure == Redact || c.SensitiveValues() || SensitiveName(name) {
		if before {
			result.field.before.state = Redacted
		}
		if after {
			result.field.after.state = Redacted
		}
		return result, nil
	}
	err := callback.Isolated("capture audit field", func() error {
		var err error
		result.field.before, err = captureSnapshot(c, change.Before())
		if err != nil {
			return err
		}
		result.field.after, err = captureSnapshot(c, change.After())
		return err
	})
	if err != nil {
		return Field[M]{}, fault.Wrap(fault.Invalid, "audit field capture failed", err)
	}
	return result, nil
}

// Capture adds one generated field to builder. Updates, soft deletes and
// restorations record only assigned or changed fields plus the subject key, so
// unchanged columns never invoke their codec or enlarge the stored row. Create
// and delete operations retain complete snapshots.
func Capture[M, K, V any](builder *Builder[M, K], name string, c codec.Codec[V], change lifecycle.FieldChange[V], disclosure Disclosure) error {
	if builder == nil || builder.seen == nil {
		return fault.New(fault.Invalid, "audit builder is not initialized")
	}
	if err := disclosure.Validate(); err != nil {
		return builder.fail(err)
	}
	if builder.skips(name, change.Assigned(), change.Changed()) {
		return builder.skip(name)
	}
	field, err := CaptureField[M](name, c, change, disclosure)
	if err != nil {
		return builder.fail(err)
	}
	return builder.Add(field)
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
	switch v := bound.(type) {
	case string:
		if len(v) > MaxCapturedValueBytes {
			if c.ParameterType() == codec.TypeJSON {
				return capturedJSONOverflow(v)
			}
			return oversized([]byte(v), true), nil
		}
	case []byte:
		if len(v) > MaxCapturedValueBytes {
			return oversized(v, true), nil
		}
	}
	state := Disclosed
	if bound != nil && c.ParameterType() == codec.TypeJSON {
		text, ok := bound.(string)
		if !ok {
			return Snapshot{}, invalidSnapshot()
		}
		text, changed, err := redactJSON(text, CurrentRedaction)
		if err != nil {
			return Snapshot{}, err
		}
		bound = text
		if changed {
			state = RedactedJSON
		}
	}
	return encodedSnapshot(state, bound)
}

// capturedJSONOverflow digests a large JSON value only after its sensitive keys
// were redacted. JSON too large to inspect keeps only its size.
func capturedJSONOverflow(text string) (Snapshot, error) {
	if len(text) > jsonwire.MaxBytes {
		return oversized([]byte(text), false), nil
	}
	redacted, _, err := redactJSON(text, CurrentRedaction)
	if err != nil {
		return Snapshot{}, err
	}
	return oversized([]byte(redacted), true), nil
}

func encodedSnapshot(state State, bound driver.Value) (Snapshot, error) {
	encoded, err := sqlvalue.Encode(bound)
	if err != nil {
		return Snapshot{}, err
	}
	result := Snapshot{state: state, value: encoded}
	if len(encoded.Text) > MaxCapturedValueBytes {
		data, err := result.payloadBytes()
		if err != nil {
			return Snapshot{}, invalidSnapshot()
		}
		return oversized(data, true), nil
	}
	return result, result.validate()
}

const redactedJSONValue = "[redacted]"

func redactJSON(text string, policy Redaction) (string, bool, error) {
	canonical, tree, err := jsonwire.Parse([]byte(text))
	if err != nil {
		return "", false, err
	}
	if !redactTree(tree, policy) {
		return canonical, false, nil
	}
	encoded, err := json.Marshal(tree)
	if err != nil || len(encoded) > jsonwire.MaxBytes {
		return "", false, invalidSnapshot()
	}
	return string(encoded), true, nil
}

func redactTree(tree any, policy Redaction) bool {
	changed := false
	switch node := tree.(type) {
	case map[string]any:
		for name, child := range node {
			if policy.Sensitive(name) {
				node[name] = redactedJSONValue
				changed = true
			} else if redactTree(child, policy) {
				changed = true
			}
		}
	case []any:
		for _, child := range node {
			if redactTree(child, policy) {
				changed = true
			}
		}
	}
	return changed
}
