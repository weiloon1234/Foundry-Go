// Package sqlvalue owns lossless SQL driver-value snapshots shared by cursors,
// model identities and other explicit persistence/transport boundaries.
package sqlvalue

import (
	"database/sql/driver"
	"encoding/base64"
	"math"
	"strconv"
	"time"
	"unicode/utf8"

	"github.com/weiloon1234/Foundry-Go/fault"
)

// Value retains the driver kind, exact text and owned byte encoding. This is an
// internal wire representation, not an application model or query API.
type Value struct {
	Kind string `json:"k"`
	Text string `json:"t,omitempty"`
}

func invalid() error { return fault.New(fault.Invalid, "invalid encoded SQL value") }

// NormalizeNull maps driver nil byte slices to SQL NULL. Non-nil empty slices
// remain empty bytes, matching PostgreSQL and the database/sql byte boundary.
func NormalizeNull(v driver.Value) driver.Value {
	if data, ok := v.([]byte); ok && data == nil {
		return nil
	}
	return v
}

// Encode snapshots a supported driver value without retaining mutable bytes.
func Encode(v driver.Value) (Value, error) {
	switch v := NormalizeNull(v).(type) {
	case nil:
		return Value{Kind: "null"}, nil
	case string:
		if !utf8.ValidString(v) {
			return Value{}, invalid()
		}
		return Value{Kind: "string", Text: v}, nil
	case []byte:
		return Value{Kind: "bytes", Text: base64.RawURLEncoding.EncodeToString(v)}, nil
	case int64:
		return Value{Kind: "int", Text: strconv.FormatInt(v, 10)}, nil
	case float64:
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return Value{}, invalid()
		}
		return Value{Kind: "float", Text: strconv.FormatFloat(v, 'g', -1, 64)}, nil
	case bool:
		return Value{Kind: "bool", Text: strconv.FormatBool(v)}, nil
	case time.Time:
		return Value{Kind: "time", Text: v.UTC().Format(time.RFC3339Nano)}, nil
	default:
		return Value{}, invalid()
	}
}

// Decode restores a driver value and gives each caller its own byte buffer.
func (v Value) Decode() (driver.Value, error) {
	switch v.Kind {
	case "null":
		if v.Text == "" {
			return nil, nil
		}
	case "string":
		return v.Text, nil
	case "bytes":
		b, err := base64.RawURLEncoding.Strict().DecodeString(v.Text)
		if err == nil {
			return b, nil
		}
	case "int":
		n, err := strconv.ParseInt(v.Text, 10, 64)
		if err == nil {
			return n, nil
		}
	case "float":
		n, err := strconv.ParseFloat(v.Text, 64)
		if err == nil && !math.IsNaN(n) && !math.IsInf(n, 0) {
			return n, nil
		}
	case "bool":
		if v.Text == "true" {
			return true, nil
		}
		if v.Text == "false" {
			return false, nil
		}
	case "time":
		instant, err := time.Parse(time.RFC3339Nano, v.Text)
		if err == nil {
			return instant, nil
		}
	}
	return nil, invalid()
}
