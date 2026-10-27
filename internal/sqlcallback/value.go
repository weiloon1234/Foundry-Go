// Package sqlcallback contains application callbacks before SQL inspects errors.
package sqlcallback

import (
	"database/sql"
	"database/sql/driver"
	"reflect"

	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

// Invoke seals a callback error before a foreign library can inspect its cause.
func Invoke(operation string, fn func() error) error {
	return safeerror.Seal(operation+" failed", callback.Isolated(operation, fn))
}

const pointerLimit = 64

// Argument protects SQL's recursive pointer conversion without changing ordinary values.
func Argument(value any) any {
	rv := reflect.ValueOf(value)
	for depth := 0; rv.IsValid(); depth++ {
		if depth == pointerLimit {
			return rejectedArgument{}
		}
		if rv.CanInterface() {
			if v, ok := rv.Interface().(driver.Valuer); ok {
				return valuer{v}
			}
		}
		if rv.Kind() != reflect.Pointer || rv.IsNil() {
			break
		}
		rv = rv.Elem()
	}
	return value
}

type valuer struct{ source driver.Valuer }

func (v valuer) Value() (value driver.Value, err error) {
	rv := reflect.ValueOf(v.source)
	if rv.Kind() == reflect.Pointer && rv.IsNil() && rv.Type().Elem().Implements(reflect.TypeFor[driver.Valuer]()) {
		return nil, nil
	}
	err = Invoke("convert SQL argument", func() error { var failure error; value, failure = v.source.Value(); return failure })
	return value, err
}

// Destinations intercepts Scanner callbacks, including database/sql's pointer allocation path.
func Destinations(values []any) []any {
	var buffer ScanBuffer
	return buffer.Prepare(values)
}

// ScanBuffer owns adapter scratch for one sequential stream. It never mutates
// or retains the caller's slice; Reset drops retained destinations on closure.
type ScanBuffer struct{ values []any }

func (b *ScanBuffer) Reset() { clear(b.values); b.values = nil }

func (b *ScanBuffer) Prepare(values []any) []any {
	clear(b.values)
	var result []any
	for i, v := range values {
		t := reflect.TypeOf(v)
		if s, ok := v.(*codecScanner); ok && s != nil {
			if result == nil {
				b.values = append(b.values[:0], values...)
				result = b.values
			}
			result[i] = &s.native
			continue
		}
		found := false
		for n := 0; t != nil && n < pointerLimit; n++ {
			if t.Implements(reflect.TypeFor[sql.Scanner]()) {
				found = true
				break
			}
			if t.Kind() != reflect.Pointer {
				break
			}
			t = t.Elem()
		}
		if !found && t != nil && t.Kind() == reflect.Pointer {
			found = true
		}
		if !found {
			continue
		}
		if result == nil {
			b.values = append(b.values[:0], values...)
			result = b.values
		}
		result[i] = scanner{reflect.ValueOf(v)}
	}
	if result == nil {
		return values
	}
	return result
}

type scanner struct{ target reflect.Value }

func (s scanner) Scan(value any) error {
	return Invoke("scan SQL value", func() error {
		target := s.target
		for n := 0; n < pointerLimit; n++ {
			if n > 0 && value == nil && target.Kind() == reflect.Pointer && target.CanSet() {
				target.SetZero()
				return nil
			}
			if target.Kind() == reflect.Pointer && target.IsNil() && target.CanSet() {
				if value == nil {
					target.SetZero()
					return nil
				}
				target.Set(reflect.New(target.Type().Elem()))
			}
			if target.CanInterface() {
				if scanner, ok := target.Interface().(sql.Scanner); ok {
					return scanner.Scan(value)
				}
			}
			if target.Kind() != reflect.Pointer || target.IsNil() && !target.CanSet() {
				return sql.ConvertAssign(driver.ScanContext{}, s.target.Interface(), value)
			}
			target = target.Elem()
		}
		return safeerror.New("SQL destination pointer depth exceeded")
	})
}

type rejectedArgument struct{}

func (rejectedArgument) Value() (driver.Value, error) {
	return nil, safeerror.New("SQL argument pointer depth exceeded")
}
