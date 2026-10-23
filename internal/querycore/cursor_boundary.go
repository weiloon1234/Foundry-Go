package querycore

import (
	"database/sql/driver"
	"slices"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/errorgraph"
)

type CursorField struct {
	Sensitive bool
	Decode    func(any) (driver.Value, error)
}
type CursorBoundary struct {
	Keys              []driver.Value
	Backward, Present bool
}

func ReadCursorBoundary(token string, present, backward bool, scope string, fields []CursorField, required []bool) (CursorBoundary, error) {
	if len(fields) != len(required) {
		return CursorBoundary{}, InvalidCursor()
	}
	for _, field := range fields {
		if field.Sensitive {
			return CursorBoundary{}, fault.New(fault.Invalid, "sensitive fields cannot be cursor keys")
		}
	}
	b := CursorBoundary{Backward: backward, Present: present}
	if !present {
		return b, nil
	}
	envelope, err := DecodeCursor(token)
	if err != nil {
		return CursorBoundary{}, CursorInputFailure(err)
	}
	if envelope.Scope != scope || len(envelope.Values) != len(fields) {
		return CursorBoundary{}, CursorInputFailure(InvalidCursor())
	}
	b.Keys = make([]driver.Value, len(fields))
	for i, field := range fields {
		raw, err := DecodeCursorValue(envelope.Values[i])
		if err != nil {
			return CursorBoundary{}, CursorInputFailure(err)
		}
		if field.Decode == nil {
			return CursorBoundary{}, InvalidCursor()
		}
		// Decode/bind and custom error classification share one owned callback.
		// Only declared invalid-value errors are client input. Panic, Goexit and
		// arbitrary infrastructure errors retain their server classification.
		err = callback.Isolated("decode cursor boundary key", func() error {
			var err error
			b.Keys[i], err = field.Decode(raw)
			if err != nil && errorgraph.Is(err, fault.Invalid) {
				return CursorInputFailure(err)
			}
			return err
		})
		if err != nil {
			return CursorBoundary{}, err
		}
		if required[i] && b.Keys[i] == nil {
			return CursorBoundary{}, CursorInputFailure(InvalidCursor())
		}
	}
	return b, nil
}

func CursorOrders(orders []QueryOrder, backward bool) []QueryOrder {
	if !backward {
		return orders
	}
	orders = slices.Clone(orders)
	for i := range orders {
		orders[i].Descending = !orders[i].Descending
	}
	return orders
}
