package query

import (
	"database/sql/driver"
	"reflect"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
)

const MaxCursorBytes = querycore.MaxCursorBytes

const MaxCursorFields = querycore.MaxCursorFields

// Cursor is an opaque result-owned position with redacted formatting.
type Cursor[M any] = querycore.Cursor[M]

// ParseCursor validates bounded transport structure without granting query access.
func ParseCursor[M any](token string) (Cursor[M], error) { return querycore.ParseCursor[M](token) }

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
	return querycore.CursorStatementScopeFor(modelIdentity, querycore.Statement{SQLText: statement.SQL(), Bindings: statement.coreStatement().Bindings}, identity)
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
	return ParseCursor[M](token)
}
