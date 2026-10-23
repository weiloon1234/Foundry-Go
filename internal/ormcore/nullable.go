package ormcore

import (
	"database/sql"

	"github.com/weiloon1234/Foundry-Go/internal/sqlvalue"
	"github.com/weiloon1234/Foundry-Go/value"
)

// NullableScan widens a stored codec's scanner without erasing its concrete type.
func NullableScan[V any](scan func(*V) any, destination *value.Nullable[V]) sql.Scanner {
	return nullableScanner[V]{scan: scan, destination: destination}
}

type nullableScanner[V any] struct {
	scan        func(*V) any
	destination *value.Nullable[V]
}

func (s nullableScanner[V]) Scan(source any) error {
	if s.scan == nil || s.destination == nil {
		return Invalid("uninitialized nullable scanner")
	}
	if sqlvalue.NormalizeNull(source) == nil {
		*s.destination = value.Null[V]()
		return nil
	}
	var item V
	scanner, ok := s.scan(&item).(sql.Scanner)
	if !ok {
		return Invalid("invalid stored value scanner")
	}
	if err := scanner.Scan(source); err != nil {
		return err
	}
	*s.destination = value.Of(item)
	return nil
}
