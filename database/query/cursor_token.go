package query

import (
	"bytes"
	"crypto/sha256"
	"database/sql/driver"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"reflect"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/sqlvalue"
)

// Cursor limits bound token decoding and lexicographic predicate construction.
const (
	MaxCursorBytes  = 64 * 1024
	MaxCursorFields = 16
)

// Cursor is an opaque model/result-owned position. Tokens are versioned and scoped to
// the query but are not signed/encrypted credentials. Apply authorization on
// every request. Token explicitly exports the transport value; formatting omits it.
type Cursor[M any] struct {
	_     [0]*M
	token string
}

func (c Cursor[M]) Token() string    { return c.token }
func (c Cursor[M]) String() string   { return "[cursor]" }
func (c Cursor[M]) GoString() string { return c.String() }

// ParseCursor validates bounded transport structure. CursorPaginate additionally
// verifies the model/query scope and decodes every key through its field codec.
func ParseCursor[M any](token string) (Cursor[M], error) {
	if _, err := decodeCursor(token); err != nil {
		return Cursor[M]{}, err
	}
	return Cursor[M]{token: token}, nil
}

type cursorEnvelope struct {
	Version int           `json:"v"`
	Scope   string        `json:"scope"`
	Values  []cursorValue `json:"values"`
}
type cursorValue sqlvalue.Value

func invalidCursor() error { return fault.New(fault.Invalid, "invalid cursor token or query scope") }
func encodeCursorValue(v driver.Value) (cursorValue, error) {
	encoded, err := sqlvalue.Encode(v)
	if err != nil {
		return cursorValue{}, invalidCursor()
	}
	return cursorValue(encoded), nil
}
func (v cursorValue) decode() (driver.Value, error) {
	decoded, err := sqlvalue.Value(v).Decode()
	if err != nil {
		return nil, invalidCursor()
	}
	return decoded, nil
}

func decodeCursor(token string) (cursorEnvelope, error) {
	if len(token) == 0 || len(token) > MaxCursorBytes {
		return cursorEnvelope{}, invalidCursor()
	}
	data, err := base64.RawURLEncoding.Strict().DecodeString(token)
	if err != nil {
		return cursorEnvelope{}, invalidCursor()
	}
	var envelope cursorEnvelope
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&envelope); err != nil {
		return cursorEnvelope{}, invalidCursor()
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF || envelope.Version != 1 || len(envelope.Scope) != sha256.Size*2 || len(envelope.Values) == 0 || len(envelope.Values) > MaxCursorFields {
		return cursorEnvelope{}, invalidCursor()
	}
	if _, err := hex.DecodeString(envelope.Scope); err != nil {
		return cursorEnvelope{}, invalidCursor()
	}
	for _, v := range envelope.Values {
		if _, err := v.decode(); err != nil {
			return cursorEnvelope{}, err
		}
	}
	return envelope, nil
}

func cursorScope[M any](q Query[M]) (string, error) {
	statement, err := q.Compile()
	if err != nil {
		return "", err
	}
	return cursorStatementScope[M](statement)
}

func cursorStatementScope[M any](statement Statement, identity ...string) (string, error) {
	values := make([]cursorValue, len(statement.arguments))
	for i, v := range statement.arguments {
		encoded, err := encodeCursorValue(v)
		if err != nil {
			return "", err
		}
		values[i] = encoded
	}
	typ := reflect.TypeFor[M]()
	data, err := json.Marshal(struct {
		Model, SQL string
		Arguments  []cursorValue
		Identity   []string `json:",omitempty"`
	}{typ.PkgPath() + "/" + typ.String(), statement.sql, values, identity})
	if err != nil {
		return "", invalidCursor()
	}
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:]), nil
}

func makeCursor[M any](scope string, fields []ModelField[M], model M) (Cursor[M], error) {
	envelope := cursorEnvelope{Version: 1, Scope: scope, Values: make([]cursorValue, len(fields))}
	for i, field := range fields {
		if field.sensitive {
			return Cursor[M]{}, fault.New(fault.Invalid, "sensitive fields cannot be cursor keys")
		}
		bound, err := field.get(model)
		if err != nil {
			return Cursor[M]{}, err
		}
		// Bound allocation before base64/JSON expansion of a large ordered value.
		switch v := bound.(type) {
		case string:
			if len(v) > MaxCursorBytes {
				return Cursor[M]{}, invalidCursor()
			}
		case []byte:
			if len(v) > MaxCursorBytes {
				return Cursor[M]{}, invalidCursor()
			}
		}
		envelope.Values[i], err = encodeCursorValue(bound)
		if err != nil {
			return Cursor[M]{}, err
		}
	}
	data, err := json.Marshal(envelope)
	if err != nil {
		return Cursor[M]{}, invalidCursor()
	}
	if base64.RawURLEncoding.EncodedLen(len(data)) > MaxCursorBytes {
		return Cursor[M]{}, invalidCursor()
	}
	return Cursor[M]{token: base64.RawURLEncoding.EncodeToString(data)}, nil
}
