package logging

import (
	"context"
	"encoding/json"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"log/slog"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/jsonwire"
	"github.com/weiloon1234/Foundry-Go/secret"
)

// Request-scoped context field bounds. Excess fields are ignored; oversized
// string values are truncated at a UTF-8 boundary.
const (
	MaxContextFields     = 32
	MaxContextGroupItems = 16
	MaxContextKeyBytes   = 64
	MaxContextValueBytes = 1024
	maxContextJSONBytes  = jsonwire.MaxBytes
)

type contextFieldsKey struct{}

// WithAttrs returns a child context carrying request-scoped log fields. The
// JSON/Correlate handlers add them to every record logged with that context
// under the reserved "context" group. A later field replaces an earlier field
// with the same key. Only scalar values (string, integers, floats, bool,
// duration, time) and one level of groups are retained; LogValuer values are
// resolved first, credential-like keys are redacted and other values (maps,
// structs, errors, models) are ignored so they cannot leak into every record.
// ctx must be non-nil.
func WithAttrs(ctx context.Context, attrs ...slog.Attr) context.Context {
	merged := slices.Clone(contextAttrs(ctx))
	for _, attr := range attrs {
		merged = mergeField(merged, attr, MaxContextFields, 0)
	}
	return context.WithValue(ctx, contextFieldsKey{}, merged)
}

// AttrsFromContext returns an owned copy of the context's log fields.
func AttrsFromContext(ctx context.Context) []slog.Attr { return slices.Clone(contextAttrs(ctx)) }

func contextAttrs(ctx context.Context) []slog.Attr {
	if ctx == nil {
		return nil
	}
	attrs, _ := ctx.Value(contextFieldsKey{}).([]slog.Attr)
	return attrs
}

func mergeField(fields []slog.Attr, attr slog.Attr, limit, depth int) []slog.Attr {
	clean, ok := sanitizeField(attr, depth)
	if !ok {
		return fields
	}
	for i := range fields {
		if fields[i].Key == clean.Key {
			fields[i] = clean
			return fields
		}
	}
	if len(fields) >= limit {
		return fields
	}
	return append(fields, clean)
}

func sanitizeField(attr slog.Attr, depth int) (slog.Attr, bool) {
	key := attr.Key
	if key == "" || len(key) > MaxContextKeyBytes || !utf8.ValidString(key) || strings.IndexFunc(key, unicode.IsControl) >= 0 {
		return slog.Attr{}, false
	}
	if sensitiveKey(key) {
		return slog.String(key, secret.Redacted), true
	}
	value := attr.Value.Resolve()
	switch value.Kind() {
	case slog.KindString:
		return slog.String(key, truncateText(value.String(), MaxContextValueBytes)), true
	case slog.KindInt64, slog.KindUint64, slog.KindFloat64, slog.KindBool, slog.KindDuration, slog.KindTime:
		return slog.Attr{Key: key, Value: value}, true
	case slog.KindGroup:
		if depth > 0 {
			return slog.Attr{}, false
		}
		var members []slog.Attr
		for _, member := range value.Group() {
			members = mergeField(members, member, MaxContextGroupItems, depth+1)
		}
		if len(members) == 0 {
			return slog.Attr{}, false
		}
		return slog.Attr{Key: key, Value: slog.GroupValue(members...)}, true
	default:
		return slog.Attr{}, false
	}
}

func truncateText(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	text = text[:limit]
	for len(text) > 0 && !utf8.ValidString(text) {
		text = text[:len(text)-1]
	}
	return text
}

// ContextFields is an owned, serializable snapshot of request-scoped log
// fields for crossing an asynchronous boundary, such as a job payload or an
// outbox record. It stores only its bounded JSON object text, so it is a
// concrete value accepted by job payload validation. Durations and times
// serialize as text and decode as strings; integers and floats decode as
// numbers. Decoding reapplies every bound and redaction rule of WithAttrs.
// The zero value carries no fields.
type ContextFields struct{ encoded string }

// FieldsFromContext snapshots the context's log fields.
func FieldsFromContext(ctx context.Context) ContextFields {
	attrs := contextAttrs(ctx)
	if len(attrs) == 0 {
		return ContextFields{}
	}
	var output strings.Builder
	if err := writeFieldObject(&output, attrs); err != nil || output.Len() > maxContextJSONBytes {
		return ContextFields{}
	}
	return ContextFields{encoded: output.String()}
}

// Attrs returns the snapshot's fields in stable key order.
func (f ContextFields) Attrs() []slog.Attr {
	if f.encoded == "" {
		return nil
	}
	attrs, err := decodeFields([]byte(f.encoded))
	if err != nil {
		return nil
	}
	return attrs
}
func (f ContextFields) IsZero() bool { return f.encoded == "" }

// Context restores the snapshot on ctx; existing fields with the same key are
// replaced.
func (f ContextFields) Context(ctx context.Context) context.Context {
	return WithAttrs(ctx, f.Attrs()...)
}

func (f ContextFields) MarshalJSON() ([]byte, error) {
	if f.encoded == "" {
		return []byte("{}"), nil
	}
	return []byte(f.encoded), nil
}

func (f *ContextFields) UnmarshalJSON(data []byte) error {
	if f == nil {
		return fault.New(fault.Invalid, "context fields decoding requires a destination")
	}
	attrs, err := decodeFields(data)
	if err != nil {
		return err
	}
	if len(attrs) == 0 {
		*f = ContextFields{}
		return nil
	}
	var output strings.Builder
	if err := writeFieldObject(&output, attrs); err != nil {
		return err
	}
	*f = ContextFields{encoded: output.String()}
	return nil
}

func writeFieldObject(output *strings.Builder, attrs []slog.Attr) error {
	output.WriteByte('{')
	for i, attr := range attrs {
		if i > 0 {
			output.WriteByte(',')
		}
		key, err := json.Marshal(attr.Key)
		if err != nil {
			return err
		}
		output.Write(key)
		output.WriteByte(':')
		value := attr.Value
		var encoded []byte
		switch value.Kind() {
		case slog.KindGroup:
			if err := writeFieldObject(output, value.Group()); err != nil {
				return err
			}
			continue
		case slog.KindString:
			encoded, err = json.Marshal(value.String())
		case slog.KindInt64:
			encoded = strconv.AppendInt(nil, value.Int64(), 10)
		case slog.KindUint64:
			encoded = strconv.AppendUint(nil, value.Uint64(), 10)
		case slog.KindFloat64:
			if number := value.Float64(); math.IsInf(number, 0) || math.IsNaN(number) {
				encoded, err = json.Marshal(strconv.FormatFloat(number, 'g', -1, 64))
			} else {
				encoded = strconv.AppendFloat(nil, number, 'g', -1, 64)
			}
		case slog.KindBool:
			encoded = strconv.AppendBool(nil, value.Bool())
		case slog.KindDuration:
			encoded, err = json.Marshal(value.Duration().String())
		case slog.KindTime:
			encoded, err = json.Marshal(value.Time().Format(time.RFC3339Nano))
		default:
			return fault.New(fault.Invalid, "context log field is not serializable")
		}
		if err != nil {
			return err
		}
		output.Write(encoded)
	}
	output.WriteByte('}')
	return nil
}

// decodeFields parses a bounded snapshot and applies the WithAttrs rules.
func decodeFields(data []byte) ([]slog.Attr, error) {
	node, err := jsonwire.Decode(data, jsonwire.Limits{Bytes: maxContextJSONBytes, Depth: 2, Nodes: 2 * MaxContextFields * (MaxContextGroupItems + 1)})
	if err != nil {
		return nil, fault.New(fault.Invalid, "invalid context log fields")
	}
	object, ok := node.(map[string]any)
	if !ok {
		return nil, fault.New(fault.Invalid, "context log fields require an object")
	}
	attrs, err := decodeFieldObject(object, true)
	if err != nil {
		return nil, err
	}
	var fields []slog.Attr
	for _, attr := range attrs {
		fields = mergeField(fields, attr, MaxContextFields, 0)
	}
	return fields, nil
}

func decodeFieldObject(object map[string]any, groups bool) ([]slog.Attr, error) {
	keys := make([]string, 0, len(object))
	for key := range object {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	attrs := make([]slog.Attr, 0, len(keys))
	for _, key := range keys {
		switch value := object[key].(type) {
		case string:
			attrs = append(attrs, slog.String(key, value))
		case bool:
			attrs = append(attrs, slog.Bool(key, value))
		case json.Number:
			if number, err := strconv.ParseInt(string(value), 10, 64); err == nil {
				attrs = append(attrs, slog.Int64(key, number))
			} else if number, err := strconv.ParseUint(string(value), 10, 64); err == nil {
				attrs = append(attrs, slog.Uint64(key, number))
			} else if number, err := strconv.ParseFloat(string(value), 64); err == nil {
				attrs = append(attrs, slog.Float64(key, number))
			} else {
				return nil, fault.New(fault.Invalid, "context log field number is out of range")
			}
		case map[string]any:
			if !groups {
				return nil, fault.New(fault.Invalid, "context log fields allow one group level")
			}
			members, err := decodeFieldObject(value, false)
			if err != nil {
				return nil, err
			}
			attrs = append(attrs, slog.Attr{Key: key, Value: slog.GroupValue(members...)})
		default:
			return nil, fault.New(fault.Invalid, "context log fields accept only scalar values and groups")
		}
	}
	return attrs, nil
}
