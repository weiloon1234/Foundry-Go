package query

import (
	"database/sql/driver"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/errorgraph"
	"slices"
)

type cursorBoundary struct {
	keys              []driver.Value
	backward, present bool
}

func validCursorRequest[R any](request CursorRequest[R]) bool {
	return validPageSize(request.Size) && !(request.After.IsSet() && request.Before.IsSet())
}

func readCursorBoundary[R any](request CursorRequest[R], scope string, fields []RecordField[R], required []bool) (cursorBoundary, error) {
	if len(fields) != len(required) {
		return cursorBoundary{}, invalidCursor()
	}
	if !validCursorRequest(request) {
		return cursorBoundary{}, cursorInputFailure(invalidCursor())
	}
	for _, field := range fields {
		if field.sensitive {
			return cursorBoundary{}, fault.New(fault.Invalid, "sensitive fields cannot be cursor keys")
		}
	}
	token, present := request.After.Get()
	backward := request.Before.IsSet()
	if backward {
		token, present = request.Before.Get()
	}
	b := cursorBoundary{backward: backward, present: present}
	if !present {
		return b, nil
	}
	envelope, err := decodeCursor(token.token)
	if err != nil {
		return cursorBoundary{}, cursorInputFailure(err)
	}
	if envelope.Scope != scope || len(envelope.Values) != len(fields) {
		return cursorBoundary{}, cursorInputFailure(invalidCursor())
	}
	b.keys = make([]driver.Value, len(fields))
	for i, field := range fields {
		raw, err := envelope.Values[i].decode()
		if err != nil {
			return cursorBoundary{}, cursorInputFailure(err)
		}
		if field.decode == nil {
			return cursorBoundary{}, invalidCursor()
		}
		// Decode/bind and custom error classification share one owned callback.
		// Only declared invalid-value errors are client input. Panic, Goexit and
		// arbitrary infrastructure errors retain their server classification.
		err = callback.Isolated("decode cursor boundary key", func() error {
			var err error
			b.keys[i], err = field.decode(raw)
			if err != nil && errorgraph.Is(err, fault.Invalid) {
				return cursorInputFailure(err)
			}
			return err
		})
		if err != nil {
			return cursorBoundary{}, err
		}
		if required[i] && b.keys[i] == nil {
			return cursorBoundary{}, cursorInputFailure(invalidCursor())
		}
	}
	return b, nil
}

func cursorOrders[S any](orders []Order[S], backward bool) []Order[S] {
	if !backward {
		return orders
	}
	orders = slices.Clone(orders)
	for i := range orders {
		orders[i].descending = !orders[i].descending
	}
	return orders
}
