package query

import (
	"database/sql/driver"

	"github.com/weiloon1234/Foundry-Go/internal/querycore"
)

type cursorBoundary struct {
	keys              []driver.Value
	backward, present bool
}

func validCursorRequest[R any](r CursorRequest[R]) bool {
	return querycore.ValidCursorRequest(r.Size, r.After.IsSet(), r.Before.IsSet())
}
func readCursorBoundary[R any](request CursorRequest[R], scope string, fields []RecordField[R], required []bool) (cursorBoundary, error) {
	if !validCursorRequest(request) {
		return cursorBoundary{}, cursorInputFailure(invalidCursor())
	}
	token, present := request.After.Get()
	backward := request.Before.IsSet()
	if backward {
		token, present = request.Before.Get()
	}
	adapters := make([]querycore.CursorField, len(fields))
	for i, f := range fields {
		adapters[i] = querycore.CursorField{Sensitive: f.sensitive, Decode: f.decode}
	}
	b, err := querycore.ReadCursorBoundary(token.Token(), present, backward, scope, adapters, required)
	return cursorBoundary{keys: b.Keys, backward: b.Backward, present: b.Present}, err
}
func cursorOrders(orders []queryOrder, backward bool) []queryOrder {
	return querycore.CursorOrders(orders, backward)
}
