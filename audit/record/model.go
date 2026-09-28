package record

import (
	"encoding/json"
	"fmt"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/jsonwire"
	"github.com/weiloon1234/Foundry-Go/internal/sqlname"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

const MaxFields = 1024
const snapshotVersion = 1

type dataWire struct {
	Version uint32      `json:"version"`
	Primary string      `json:"primary"`
	Fields  []fieldWire `json:"fields"`
}

// Entry is the immutable heterogeneous storage boundary of a model audit. Normal
// application code receives the model-owned Model wrapper and generated fields.
// Entry is attribution/history, never authentication or authorization evidence.
type Entry struct {
	identity  model.Identity
	operation lifecycle.Operation
	data      value.JSON[dataWire]
}

func (e Entry) Identity() model.Identity       { return e.identity }
func (e Entry) Operation() lifecycle.Operation { return e.operation }
func (Entry) Format(state fmt.State, _ rune)   { _, _ = state.Write([]byte("audit entry")) }

// Payload exposes the versioned snapshot document only at a deliberate storage
// or report boundary. It contains audit values, not serialized model DTOs.
func (e Entry) Payload() (string, error) {
	if err := e.Validate(); err != nil {
		return "", err
	}
	return e.data.Text()
}

func (e Entry) Validate() error {
	if err := e.identity.Validate(); err != nil {
		return err
	}
	before, after, err := existence(e.operation)
	if err != nil {
		return err
	}
	data, err := e.data.Decode()
	if err != nil {
		return fault.Wrap(fault.Invalid, "invalid audit payload", err)
	}
	if data.Version != snapshotVersion || !sqlname.Valid(data.Primary) || SensitiveName(data.Primary) || len(data.Fields) == 0 || len(data.Fields) > MaxFields {
		return fault.New(fault.Invalid, "invalid audit snapshot version or field count")
	}
	seen := make(map[string]struct{}, len(data.Fields))
	for _, field := range data.Fields {
		if _, exists := seen[field.Name]; exists {
			return fault.New(fault.Duplicate, "duplicate audit field")
		}
		seen[field.Name] = struct{}{}
		if err := validateField(field, before, after); err != nil {
			return err
		}
		if field.Name == data.Primary {
			if err := validatePrimary(field, e.identity, before, after); err != nil {
				return err
			}
		}
	}
	if _, present := seen[data.Primary]; !present {
		return fault.New(fault.Missing, "audit payload omitted its subject field")
	}
	return nil
}

// ParseEntry is an explicit persistence/transport boundary. It validates stored
// metadata, snapshot states and redaction before allowing export or typed restore.
func ParseEntry(identity model.Identity, operation lifecycle.Operation, payload string) (Entry, error) {
	data, err := value.ParseJSON[dataWire](payload)
	if err != nil {
		return Entry{}, fault.Wrap(fault.Invalid, "invalid audit payload", err)
	}
	entry := Entry{identity: identity, operation: operation, data: data}
	if err := entry.Validate(); err != nil {
		return Entry{}, err
	}
	return entry, nil
}

// Model preserves the audited subject and stored key types. Its snapshots remain
// audit representations: exclusions/redaction never produce incomplete models.
type Model[M, K any] struct {
	template model.Reference[M, K]
	entry    Entry
}

func (m Model[M, K]) Entry() Entry                   { return m.entry }
func (m Model[M, K]) Operation() lifecycle.Operation { return m.entry.operation }
func (Model[M, K]) Format(state fmt.State, _ rune)   { _, _ = state.Write([]byte("model audit")) }

// Subject restores a fresh concrete key from the captured identity. It does not
// reuse a caller's later-mutated key or perform model lookup or getter evaluation.
func (m Model[M, K]) Subject() (model.Reference[M, K], error) {
	return m.template.Parse(m.entry.identity)
}

// RestoreModel reapplies the expected model/key owner to an already validated
// history entry. The template's current key is ignored, as with Reference.Parse.
func RestoreModel[M, K any](template model.Reference[M, K], entry Entry) (Model[M, K], error) {
	if err := entry.Validate(); err != nil {
		return Model[M, K]{}, err
	}
	if _, err := template.Parse(entry.identity); err != nil {
		return Model[M, K]{}, err
	}
	return Model[M, K]{template: template, entry: entry}, nil
}

// Builder is a generated, operation-local capture boundary. It is not safe for
// concurrent use; the completed Model is immutable. Add errors remain sticky so
// ignoring one cannot publish a partial record. No database work occurs here.
type Builder[M, K any] struct {
	template      model.Reference[M, K]
	identity      model.Identity
	operation     lifecycle.Operation
	primary       string
	before, after bool
	fields        []fieldWire
	seen          map[string]struct{}
	bytes         int
	err           error
	closed        bool
}

func (Builder[M, K]) Format(state fmt.State, _ rune) {
	_, _ = state.Write([]byte("audit capture builder"))
}

// NewBuilder is emitted from the model's existing primary metadata. A subject
// key is necessarily present in audit identity; excluding/redacting that field
// would misleadingly retain its value elsewhere. Such policies, including a
// conventionally sensitive primary column, fail before binding the subject key.
func NewBuilder[M, K any](template model.Reference[M, K], operation lifecycle.Operation, primary string, disclosure Disclosure) (*Builder[M, K], error) {
	if !sqlname.Valid(primary) || disclosure != Automatic || SensitiveName(primary) {
		return nil, fault.New(fault.Invalid, "audit subject key must permit explicit identity capture")
	}
	before, after, err := existence(operation)
	if err != nil {
		return nil, err
	}
	identity, err := template.Identity()
	if err != nil {
		return nil, fault.Wrap(fault.Invalid, "audit subject capture failed", err)
	}
	return &Builder[M, K]{template: template, identity: identity, operation: operation,
		primary: primary, before: before, after: after, seen: make(map[string]struct{}), bytes: 128}, nil
}

func (b *Builder[M, K]) Add(field Field[M]) error {
	if b == nil || b.seen == nil {
		return fault.New(fault.Invalid, "audit builder is not initialized")
	}
	if b.closed {
		return fault.New(fault.Closed, "audit capture is already complete")
	}
	if b.err != nil {
		return b.err
	}
	b.err = b.add(field)
	return b.err
}

func (b *Builder[M, K]) add(field Field[M]) error {
	if !sqlname.Valid(field.wire.Name) || field.before != b.before || field.after != b.after {
		return fault.New(fault.Invalid, "audit field does not match its model operation")
	}
	if _, exists := b.seen[field.wire.Name]; exists {
		return fault.New(fault.Duplicate, "duplicate audit field")
	}
	if len(b.seen) >= MaxFields {
		return fault.New(fault.Invalid, "audit capture exceeds its field bound")
	}
	if field.wire.Name == b.primary && (field.excluded ||
		(b.before && field.wire.Before.state != Disclosed) || (b.after && field.wire.After.state != Disclosed)) {
		return fault.New(fault.Invalid, "audit subject field cannot hide its captured identity")
	}
	b.seen[field.wire.Name] = struct{}{}
	if field.excluded {
		return nil
	}
	if err := validateField(field.wire, b.before, b.after); err != nil {
		return err
	}
	if field.wire.Name == b.primary {
		if err := validatePrimary(field.wire, b.identity, b.before, b.after); err != nil {
			return err
		}
	}
	if field.bytes <= 0 || field.bytes > jsonwire.MaxBytes-b.bytes-1 {
		return fault.New(fault.Invalid, "audit record exceeds its representation bound")
	}
	b.bytes += field.bytes + 1
	b.fields = append(b.fields, field.wire)
	return nil
}

func (b *Builder[M, K]) Build() (Model[M, K], error) {
	if b == nil || b.seen == nil {
		return Model[M, K]{}, fault.New(fault.Invalid, "audit builder is not initialized")
	}
	if b.closed {
		return Model[M, K]{}, fault.New(fault.Closed, "audit capture is already complete")
	}
	b.closed = true
	if b.err != nil {
		return Model[M, K]{}, b.err
	}
	if _, present := b.seen[b.primary]; !present {
		return Model[M, K]{}, fault.New(fault.Missing, "audit capture omitted its subject field")
	}
	data, err := value.NewJSON(dataWire{Version: snapshotVersion, Primary: b.primary, Fields: b.fields})
	if err != nil {
		return Model[M, K]{}, fault.Wrap(fault.Invalid, "audit payload capture failed", err)
	}
	entry := Entry{identity: b.identity, operation: b.operation, data: data}
	if err := entry.Validate(); err != nil {
		return Model[M, K]{}, err
	}
	return Model[M, K]{template: b.template, entry: entry}, nil
}

func existence(operation lifecycle.Operation) (before, after bool, err error) {
	switch operation {
	case lifecycle.Create:
		return false, true, nil
	case lifecycle.Update, lifecycle.SoftDelete, lifecycle.Restore:
		return true, true, nil
	case lifecycle.Delete, lifecycle.ForceDelete:
		return true, false, nil
	default:
		return false, false, fault.New(fault.Invalid, "audit requires a captured model operation")
	}
}

func validateField(field fieldWire, before, after bool) error {
	if !sqlname.Valid(field.Name) || field.Type < codec.TypeBoolean || field.Type > codec.TypeJSON ||
		(field.Before.state != Absent) != before || (field.After.state != Absent) != after || field.Assigned && !after ||
		before != after && !field.Changed {
		return fault.New(fault.Invalid, "invalid audit field metadata or snapshot states")
	}
	for _, snapshot := range []Snapshot{field.Before, field.After} {
		if err := snapshot.validate(); err != nil {
			return err
		}
		if snapshot.state == Absent {
			continue
		}
		if SensitiveName(field.Name) && snapshot.state != Redacted {
			return invalidSnapshot()
		}
		if snapshot.state == RedactedJSON && field.Type != codec.TypeJSON {
			return invalidSnapshot()
		}
		if field.Type == codec.TypeJSON && snapshot.state == Disclosed && snapshot.value.Kind != "null" {
			if snapshot.value.Kind != "string" {
				return invalidSnapshot()
			}
			canonical, changed, err := redactJSON(snapshot.value.Text)
			if err != nil || changed || canonical != snapshot.value.Text {
				return invalidSnapshot()
			}
		}
	}
	return nil
}

func validatePrimary(field fieldWire, identity model.Identity, before, after bool) error {
	key, err := identity.KeyJSON()
	if err != nil {
		return err
	}
	if before && after && (field.Assigned || field.Changed) {
		return fault.New(fault.Invalid, "audit cannot replace or assign an existing subject key")
	}
	for _, snapshot := range []Snapshot{field.Before, field.After} {
		if snapshot.state == Absent {
			continue
		}
		if snapshot.state != Disclosed {
			return fault.New(fault.Invalid, "audit subject field cannot hide its captured identity")
		}
		encoded, err := json.Marshal(snapshot.value)
		if err != nil || string(encoded) != key {
			return fault.New(fault.Invalid, "audit subject does not match its stored snapshots")
		}
	}
	return nil
}
