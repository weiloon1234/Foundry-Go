package database

import (
	"database/sql"
	"database/sql/driver"
	"reflect"
	"slices"

	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

// Adapt before database/sql formats callback errors. Clone only the caller's
// argument/destination list; the callback keeps its original receiver and owner.
func safeArguments(arguments []any) []any {
	var result []any
	for i, argument := range arguments {
		named, isNamed := argument.(sql.NamedArg)
		if isNamed {
			argument = named.Value
		}
		valuer, ok := argument.(driver.Valuer)
		if !ok {
			continue
		}
		if result == nil {
			result = slices.Clone(arguments)
		}
		var adapted any = sqlValuer{valuer}
		if isNamed {
			named.Value = adapted
			adapted = named
		}
		result[i] = adapted
	}
	if result == nil {
		return arguments
	}
	return result
}

type sqlValuer struct{ source driver.Valuer }

func (v sqlValuer) Value() (result driver.Value, err error) {
	// Match database/sql's nil pointer handling for a promoted value receiver.
	rv := reflect.ValueOf(v.source)
	if rv.Kind() == reflect.Pointer && rv.IsNil() && rv.Type().Elem().Implements(reflect.TypeFor[driver.Valuer]()) {
		return nil, nil
	}
	err = callback.Isolated("bind SQL argument", func() error {
		var failure error
		result, failure = v.source.Value()
		return safeerror.Wrap("SQL argument binding failed", failure)
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func safeDestinations(destinations []any) []any {
	var result []any
	for i, destination := range destinations {
		scanner, ok := destination.(sql.Scanner)
		if !ok {
			continue
		}
		if result == nil {
			result = slices.Clone(destinations)
		}
		result[i] = sqlScanner{scanner}
	}
	if result == nil {
		return destinations
	}
	return result
}

type sqlScanner struct{ source sql.Scanner }

func (s sqlScanner) Scan(value any) error {
	return callback.Isolated("scan SQL value", func() error {
		return safeerror.Wrap("SQL value scanning failed", s.source.Scan(value))
	})
}
