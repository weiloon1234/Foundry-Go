package querycore

import (
	"bytes"
	"crypto/sha256"
	"database/sql/driver"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/sqlvalue"
)

// Cursor limits bound token decoding and lexicographic predicate construction.
const (
	MaxCursorBytes  = 64 * 1024
	MaxCursorFields = 16
)

type CursorEnvelope struct {
	Version int           `json:"v"`
	Scope   string        `json:"scope"`
	Values  []CursorValue `json:"values"`
}

type CursorValue = sqlvalue.Value

func InvalidCursor() error { return fault.New(fault.Invalid, "invalid cursor token or query scope") }

func EncodeCursorValue(v driver.Value) (CursorValue, error) {
	encoded, err := sqlvalue.Encode(v)
	if err != nil {
		return CursorValue{}, InvalidCursor()
	}
	return CursorValue(encoded), nil
}

func DecodeCursorValue(v CursorValue) (driver.Value, error) {
	decoded, err := sqlvalue.Value(v).Decode()
	if err != nil {
		return nil, InvalidCursor()
	}
	return decoded, nil
}

func DecodeCursor(token string) (CursorEnvelope, error) {
	if len(token) == 0 || len(token) > MaxCursorBytes {
		return CursorEnvelope{}, InvalidCursor()
	}
	data, err := base64.RawURLEncoding.Strict().DecodeString(token)
	if err != nil {
		return CursorEnvelope{}, InvalidCursor()
	}
	var envelope CursorEnvelope
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&envelope); err != nil {
		return CursorEnvelope{}, InvalidCursor()
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF || envelope.Version != 1 || len(envelope.Scope) != sha256.Size*2 || len(envelope.Values) == 0 || len(envelope.Values) > MaxCursorFields {
		return CursorEnvelope{}, InvalidCursor()
	}
	if _, err := hex.DecodeString(envelope.Scope); err != nil {
		return CursorEnvelope{}, InvalidCursor()
	}
	for _, v := range envelope.Values {
		if _, err := DecodeCursorValue(v); err != nil {
			return CursorEnvelope{}, err
		}
	}
	return envelope, nil
}

func CursorStatementScopeFor(modelIdentity string, statement Statement, identity []string) (string, error) {
	values := make([]CursorValue, len(statement.Bindings))
	for i, v := range statement.Bindings {
		encoded, err := EncodeCursorValue(v)
		if err != nil {
			return "", err
		}
		values[i] = encoded
	}
	data, err := json.Marshal(struct {
		Model, SQL string
		Arguments  []CursorValue
		Identity   []string `json:",omitempty"`
	}{modelIdentity, statement.SQLText, values, identity})
	if err != nil {
		return "", InvalidCursor()
	}
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:]), nil
}

// MakeCursor serializes the unchanged v1 envelope after bounding each key.
func MakeCursor(scope string, values []driver.Value) (string, error) {
	return BuildCursor(scope, len(values), func(i int) (driver.Value, error) { return values[i], nil })
}

// BuildCursor bounds and encodes each field before evaluating the next getter.
// This preserves error precedence and bounded expansion for both API adapters.
func BuildCursor(scope string, count int, get func(int) (driver.Value, error)) (string, error) {
	envelope := CursorEnvelope{Version: 1, Scope: scope, Values: make([]CursorValue, count)}
	for i := range count {
		bound, err := get(i)
		if err != nil {
			return "", err
		}
		switch v := bound.(type) {
		case string:
			if len(v) > MaxCursorBytes {
				return "", InvalidCursor()
			}
		case []byte:
			if len(v) > MaxCursorBytes {
				return "", InvalidCursor()
			}
		}
		envelope.Values[i], err = EncodeCursorValue(bound)
		if err != nil {
			return "", err
		}
	}
	data, err := json.Marshal(envelope)
	if err != nil {
		return "", InvalidCursor()
	}
	if base64.RawURLEncoding.EncodedLen(len(data)) > MaxCursorBytes {
		return "", InvalidCursor()
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}
