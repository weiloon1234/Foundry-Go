package query

import (
	"database/sql/driver"
	"reflect"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
)

const MaxCursorBytes = querycore.MaxCursorBytes

const MaxCursorFields = querycore.MaxCursorFields

// Cursor is an opaque model/result-owned position. Tokens are versioned and scoped to
// the query but are not signed/encrypted credentials. Apply authorization on
// every request. Token explicitly exports the transport value; formatting omits it.
type Cursor[M any] struct {
	_     [0]*M
	token string
}

func (c Cursor[M]) Token() string { return c.token }

func (c Cursor[M]) String() string { return "[cursor]" }

func (c Cursor[M]) GoString() string { return c.String() }

// ParseCursor validates bounded transport structure. CursorPaginate additionally
// verifies the model/query scope and decodes every key through its field codec.
func ParseCursor[M any](token string) (Cursor[M], error) {
	if _, err := decodeCursor(token); err != nil {
		return Cursor[M]{}, err
	}
	return Cursor[M]{token: token}, nil
}

type cursorEnvelope = querycore.CursorEnvelope
type cursorValue = querycore.CursorValue

func invalidCursor() error                                  { return querycore.InvalidCursor() }
func encodeCursorValue(v driver.Value) (cursorValue, error) { return querycore.EncodeCursorValue(v) }
func decodeCursorValue(v cursorValue) (driver.Value, error) { return querycore.DecodeCursorValue(v) }
func decodeCursor(token string) (cursorEnvelope, error)     { return querycore.DecodeCursor(token) }

func cursorScope[M any](q Query[M]) (string, error) {
	statement, err := q.Compile()
	if err != nil {
		return "", err
	}
	return cursorStatementScope[M](statement)
}

func cursorStatementScope[M any](statement Statement, identity ...string) (string, error) {
	typ := reflect.TypeFor[M]()
	return cursorStatementScopeFor(typ.PkgPath()+"/"+typ.String(), statement, identity)
}

func cursorStatementScopeFor(modelIdentity string, statement Statement, identity []string) (string, error) {
	return querycore.CursorStatementScopeFor(modelIdentity, querycore.Statement{SQLText: statement.sql, Bindings: statement.arguments}, identity)
}
func makeCursor[M any](scope string, fields []ModelField[M], model M) (Cursor[M], error) {
	token, err := querycore.BuildCursor(scope, len(fields), func(i int) (driver.Value, error) {
		field := fields[i]
		if field.sensitive {
			return nil, fault.New(fault.Invalid, "sensitive fields cannot be cursor keys")
		}
		return field.get(model)
	})
	if err != nil {
		return Cursor[M]{}, err
	}
	return Cursor[M]{token: token}, nil
}
