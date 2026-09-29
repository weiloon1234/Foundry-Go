package record

import (
	"encoding/json"
	"fmt"
	"slices"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/jsonwire"
	"github.com/weiloon1234/Foundry-Go/internal/sqlname"
	"github.com/weiloon1234/Foundry-Go/internal/sqlvalue"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

const MaxFields = 1024

// The snapshot document version equals the Redaction policy that captured it.
// Version 1 predates oversized digests and changed-only updates.
type dataWire struct {
	Version uint32      `json:"version"`
	Primary string      `json:"primary"`
	Fields  []fieldWire `json:"fields"`
}

// entryData is validated once at construction and never mutated afterwards.
type entryData struct {
	redaction Redaction
	primary   string
	fields    []capturedField
	text      string
}

// Entry is the immutable heterogeneous storage boundary of a model audit. Normal
// application code receives the model-owned Model wrapper and generated fields.
// Entry is attribution/history, never authentication or authorization evidence.
// Constructors validate the complete document once; copies share that result.
type Entry struct {
	identity  model.Identity
	operation lifecycle.Operation
	data      *entryData
}

func (e Entry) Identity() model.Identity       { return e.identity }
func (e Entry) Operation() lifecycle.Operation { return e.operation }
func (Entry) Format(state fmt.State, _ rune)   { _, _ = state.Write([]byte("audit entry")) }

// Redaction reports the sensitive-name policy that captured this entry. Values
// of an entry captured under an older policy are additionally masked under
// CurrentRedaction when it is parsed, and the result stays valid under the
// capturing policy.
func (e Entry) Redaction() Redaction {
	if e.data == nil {
		return 0
	}
	return e.data.redaction
}

// Payload exposes the versioned snapshot document only at a deliberate storage
// or report boundary. It contains audit values, not serialized model DTOs.
func (e Entry) Payload() (string, error) {
	if err := e.Validate(); err != nil {
		return "", err
	}
	return e.data.text, nil
}

// Validate reports whether the entry came from Build or ParseEntry, which check
// metadata, snapshot states and redaction under the entry's own policy.
func (e Entry) Validate() error {
	if e.data == nil {
		return fault.New(fault.Invalid, "audit entry is not initialized")
	}
	return nil
}

// ParseEntry is an explicit persistence/transport boundary. It validates stored
// metadata, snapshot states and redaction before allowing export or typed restore.
// Each stored document is checked against the redaction policy that wrote it.
func ParseEntry(identity model.Identity, operation lifecycle.Operation, payload string) (Entry, error) {
	parsed, err := value.ParseJSON[dataWire](payload)
	if err != nil {
		return Entry{}, fault.Wrap(fault.Invalid, "invalid audit payload", err)
	}
	wire, err := parsed.Decode()
	if err != nil {
		return Entry{}, fault.Wrap(fault.Invalid, "invalid audit payload", err)
	}
	text, err := parsed.Text()
	if err != nil {
		return Entry{}, err
	}
	data, err := parseData(identity, operation, wire)
	if err != nil {
		return Entry{}, err
	}
	masked, err := data.maskCurrent()
	if err != nil {
		return Entry{}, err
	}
	if !masked {
		data.text = text
	} else if text, err = data.marshal(); err != nil {
		return Entry{}, err
	} else if err := data.canonical(text); err != nil {
		return Entry{}, err
	}
	return Entry{identity: identity, operation: operation, data: data}, nil
}

// maskCurrent hides, at read time, values that CurrentRedaction treats as
// sensitive although the older policy that wrote the row disclosed them, such
// as an otp or card_number column captured under RedactionV1. The row's own
// policy has already validated the structure, and the masked entry remains
// valid under it and never grows: a sensitive column becomes Redacted, a
// disclosed JSON value with a newly sensitive key is redacted completely, and
// an already redacted JSON value also redacts its newly sensitive keys. The
// subject key is the entry's identity and is never masked.
func (d *entryData) maskCurrent() (bool, error) {
	if d.redaction >= CurrentRedaction {
		return false, nil
	}
	masked := false
	for i := range d.fields {
		field := &d.fields[i]
		if field.name == d.primary {
			continue
		}
		sensitive := CurrentRedaction.Sensitive(field.name)
		for _, snapshot := range []*Snapshot{&field.before, &field.after} {
			switch {
			case snapshot.state == Absent || snapshot.state == Redacted:
			case sensitive:
				*snapshot, masked = Snapshot{state: Redacted}, true
			case field.typ == codec.TypeJSON && snapshot.value.Kind == "string" && (snapshot.state == Disclosed || snapshot.state == RedactedJSON):
				text, changed, err := redactJSON(snapshot.value.Text, CurrentRedaction)
				if err != nil {
					return false, err
				}
				switch {
				case !changed || text == snapshot.value.Text:
				case snapshot.state == Disclosed || len(text) > len(snapshot.value.Text):
					// A partially redacted copy would not be valid under the
					// writing policy, or would outgrow the stored document.
					*snapshot, masked = Snapshot{state: Redacted}, true
				default:
					encoded, err := sqlvalue.Encode(text)
					if err != nil {
						return false, invalidSnapshot()
					}
					snapshot.value, masked = encoded, true
				}
			}
		}
	}
	return masked, nil
}

func parseData(identity model.Identity, operation lifecycle.Operation, wire dataWire) (*entryData, error) {
	if err := identity.Validate(); err != nil {
		return nil, err
	}
	before, after, err := existence(operation)
	if err != nil {
		return nil, err
	}
	if wire.Version < uint32(RedactionV1) || wire.Version > uint32(CurrentRedaction) {
		return nil, fault.New(fault.Invalid, "invalid audit snapshot version or field count")
	}
	policy := Redaction(wire.Version)
	if !sqlname.Valid(wire.Primary) ||
		policy.Sensitive(wire.Primary) || len(wire.Fields) == 0 || len(wire.Fields) > MaxFields {
		return nil, fault.New(fault.Invalid, "invalid audit snapshot version or field count")
	}
	data := &entryData{redaction: policy, primary: wire.Primary, fields: make([]capturedField, 0, len(wire.Fields))}
	seen := make(map[string]struct{}, len(wire.Fields))
	for _, item := range wire.Fields {
		field, err := fieldFromWire(item)
		if err != nil {
			return nil, err
		}
		if _, exists := seen[field.name]; exists {
			return nil, fault.New(fault.Duplicate, "duplicate audit field")
		}
		seen[field.name] = struct{}{}
		if err := validateField(field, before, after, policy); err != nil {
			return nil, err
		}
		if field.name == wire.Primary {
			if err := validatePrimary(field, identity, before, after); err != nil {
				return nil, err
			}
		}
		data.fields = append(data.fields, field)
	}
	if _, present := seen[wire.Primary]; !present {
		return nil, fault.New(fault.Missing, "audit payload omitted its subject field")
	}
	return data, nil
}

// Compact returns an entry whose disclosed values above limit bytes are stored
// as Oversized digests. It never fails because values are large; it is used by
// recorders to apply their configured per-value threshold before storage.
// Entries captured under an older policy are returned unchanged.
func (e Entry) Compact(limit int) (Entry, error) {
	if err := e.Validate(); err != nil {
		return Entry{}, err
	}
	if limit < 1 {
		return Entry{}, fault.New(fault.Invalid, "audit value limit must be positive")
	}
	if e.data.redaction < RedactionV2 {
		return e, nil
	}
	var fields []capturedField
	for i, field := range e.data.fields {
		if field.name == e.data.primary {
			continue
		}
		before, changedBefore, err := field.before.compact(limit)
		if err != nil {
			return Entry{}, err
		}
		after, changedAfter, err := field.after.compact(limit)
		if err != nil {
			return Entry{}, err
		}
		if !changedBefore && !changedAfter {
			continue
		}
		if fields == nil {
			fields = slices.Clone(e.data.fields)
		}
		fields[i].before, fields[i].after = before, after
	}
	if fields == nil {
		return e, nil
	}
	data := &entryData{redaction: e.data.redaction, primary: e.data.primary, fields: fields}
	if err := data.encode(); err != nil {
		return Entry{}, err
	}
	return Entry{identity: e.identity, operation: e.operation, data: data}, nil
}

// encode produces the stored document, replacing the largest disclosed values
// with Oversized digests until the whole record fits its representation bound.
// The subject key is never replaced.
func (d *entryData) encode() error {
	text, err := d.marshal()
	if err != nil {
		return err
	}
	if len(text) <= jsonwire.MaxBytes {
		return d.canonical(text)
	}
	type candidate struct {
		field int
		after bool
		bytes int
	}
	var candidates []candidate
	for i, field := range d.fields {
		if field.name == d.primary {
			continue
		}
		for _, after := range []bool{false, true} {
			snapshot := field.before
			if after {
				snapshot = field.after
			}
			if snapshot.state != Disclosed && snapshot.state != RedactedJSON {
				continue
			}
			encoded, err := json.Marshal(snapshot.wire())
			if err != nil {
				return invalidSnapshot()
			}
			candidates = append(candidates, candidate{field: i, after: after, bytes: len(encoded)})
		}
	}
	slices.SortStableFunc(candidates, func(a, b candidate) int { return b.bytes - a.bytes })
	excess := len(text) - jsonwire.MaxBytes
	for _, item := range candidates {
		target := &d.fields[item.field].before
		if item.after {
			target = &d.fields[item.field].after
		}
		replaced, _, err := target.compact(0)
		if err != nil {
			return err
		}
		*target = replaced
		excess -= item.bytes - 128
		if excess > 0 {
			continue
		}
		if text, err = d.marshal(); err != nil {
			return err
		}
		if excess = len(text) - jsonwire.MaxBytes; excess <= 0 {
			return d.canonical(text)
		}
	}
	return fault.New(fault.Invalid, "audit field metadata exceeds its representation bound")
}

// canonical stores the same canonical text a stored row reloads with, and
// checks the shared JSON node/depth bounds before any SQL.
func (d *entryData) canonical(text string) error {
	canonical, _, err := jsonwire.Parse([]byte(text))
	if err != nil {
		return fault.Wrap(fault.Invalid, "audit payload exceeds its representation bound", err)
	}
	d.text = canonical
	return nil
}

func (d *entryData) marshal() (string, error) {
	fields := make([]fieldWire, len(d.fields))
	for i, field := range d.fields {
		fields[i] = field.wire()
	}
	encoded, err := json.Marshal(dataWire{Version: uint32(d.redaction), Primary: d.primary, Fields: fields})
	if err != nil {
		return "", fault.Wrap(fault.Invalid, "audit payload capture failed", err)
	}
	return string(encoded), nil
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
// Updates, soft deletes and restorations keep only assigned or changed fields
// and the subject key; creation and deletion keep complete snapshots.
type Builder[M, K any] struct {
	template      model.Reference[M, K]
	identity      model.Identity
	operation     lifecycle.Operation
	primary       string
	before, after bool
	fields        []capturedField
	seen          map[string]struct{}
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
		primary: primary, before: before, after: after, seen: make(map[string]struct{})}, nil
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

// skips reports whether an existing-model operation leaves this field out.
func (b *Builder[M, K]) skips(name string, assigned, changed bool) bool {
	return b.before && b.after && name != b.primary && !assigned && !changed
}

// skip records an omitted field for duplicate and bound detection.
func (b *Builder[M, K]) skip(name string) error {
	if b.closed {
		return fault.New(fault.Closed, "audit capture is already complete")
	}
	if b.err != nil {
		return b.err
	}
	b.err = b.claim(name)
	return b.err
}

func (b *Builder[M, K]) fail(err error) error {
	if b.err == nil && !b.closed {
		b.err = err
	}
	return err
}

func (b *Builder[M, K]) claim(name string) error {
	if !sqlname.Valid(name) {
		return fault.New(fault.Invalid, "audit field requires a declared column")
	}
	if _, exists := b.seen[name]; exists {
		return fault.New(fault.Duplicate, "duplicate audit field")
	}
	if len(b.seen) >= MaxFields {
		return fault.New(fault.Invalid, "audit capture exceeds its field bound")
	}
	b.seen[name] = struct{}{}
	return nil
}

func (b *Builder[M, K]) add(item Field[M]) error {
	field := item.field
	if !sqlname.Valid(field.name) || item.before != b.before || item.after != b.after {
		return fault.New(fault.Invalid, "audit field does not match its model operation")
	}
	if field.name == b.primary && (item.excluded ||
		(b.before && field.before.state != Disclosed) || (b.after && field.after.state != Disclosed)) {
		return fault.New(fault.Invalid, "audit subject field cannot hide its captured identity")
	}
	if err := b.claim(field.name); err != nil {
		return err
	}
	if item.excluded || b.skips(field.name, field.assigned, field.changed) {
		return nil
	}
	if err := validateField(field, b.before, b.after, CurrentRedaction); err != nil {
		return err
	}
	if field.name == b.primary {
		if err := validatePrimary(field, b.identity, b.before, b.after); err != nil {
			return err
		}
	}
	b.fields = append(b.fields, field)
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
	data := &entryData{redaction: CurrentRedaction, primary: b.primary, fields: b.fields}
	b.fields = nil
	if err := data.encode(); err != nil {
		return Model[M, K]{}, err
	}
	return Model[M, K]{template: b.template, entry: Entry{identity: b.identity, operation: b.operation, data: data}}, nil
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

func validateField(field capturedField, before, after bool, policy Redaction) error {
	if !sqlname.Valid(field.name) || field.typ < codec.TypeBoolean || field.typ > codec.TypeJSON ||
		(field.before.state != Absent) != before || (field.after.state != Absent) != after || field.assigned && !after ||
		before != after && !field.changed {
		return fault.New(fault.Invalid, "invalid audit field metadata or snapshot states")
	}
	sensitive := policy.Sensitive(field.name)
	for _, snapshot := range []Snapshot{field.before, field.after} {
		if err := snapshot.validate(); err != nil {
			return err
		}
		switch {
		case snapshot.state == Absent:
			continue
		case sensitive && snapshot.state != Redacted,
			snapshot.state == RedactedJSON && field.typ != codec.TypeJSON,
			snapshot.state == Oversized && policy < RedactionV2:
			return invalidSnapshot()
		}
		if field.typ != codec.TypeJSON || snapshot.state != RedactedJSON && (snapshot.state != Disclosed || snapshot.value.Kind == "null") {
			continue
		}
		if snapshot.value.Kind != "string" {
			return invalidSnapshot()
		}
		canonical, changed, err := redactJSON(snapshot.value.Text, policy)
		if err != nil || changed != (snapshot.state == RedactedJSON) || canonical != snapshot.value.Text {
			return invalidSnapshot()
		}
	}
	return nil
}

func validatePrimary(field capturedField, identity model.Identity, before, after bool) error {
	key, err := identity.KeyJSON()
	if err != nil {
		return err
	}
	if before && after && (field.assigned || field.changed) {
		return fault.New(fault.Invalid, "audit cannot replace or assign an existing subject key")
	}
	for _, snapshot := range []Snapshot{field.before, field.after} {
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
