// Package codec preserves concrete Go types through the SQL value boundary.
// Generated model code uses these codecs for bound values and fresh hydration.
// Constructors describe codecs, not physical table schemas.
package codec

import (
	"database/sql"
	"database/sql/driver"
	"slices"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/sqlvalue"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Codec binds and decodes one concrete type. Its zero value is invalid. Decode
// receives a driver-owned runtime value; ordinary model code retains T instead.
type Codec[T any] struct {
	encode        func(T) (driver.Value, error)
	decode        func(any) (T, error)
	clone         func(T) T
	parameterType ParameterType
	sensitive     bool
}

// New is an explicit custom-codec boundary. Callbacks must be safe for concurrent
// use, leave inputs unchanged, and return owned values without driver byte aliases.
// A custom codec owns its NULL semantics; built-ins reject NULL except Nullable.
func New[T any](encode func(T) (driver.Value, error), decode func(any) (T, error)) Codec[T] {
	return Codec[T]{encode: encode, decode: decode}
}

func invalid() error { return fault.New(fault.Invalid, "invalid database codec value or destination") }

// Validate checks that both codec directions are defined without invoking them.
// Metadata constructors use this before accepting a custom codec.
func (c Codec[T]) Validate() error {
	if c.encode == nil || c.decode == nil {
		return invalid()
	}
	return nil
}

// Bind validates the concrete value and returns a supported SQL driver value.
// Nil byte slices become SQL NULL. Non-nil byte buffers returned by custom
// encoders are copied before publication, including non-null empty buffers.
func (c Codec[T]) Bind(value T) (driver.Value, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	encoded, err := c.encode(value)
	if err != nil {
		return nil, err
	}
	if !driver.IsValue(encoded) {
		return nil, invalid()
	}
	encoded = sqlvalue.NormalizeNull(encoded)
	if data, ok := encoded.([]byte); ok {
		encoded = slices.Clone(data)
	}
	return encoded, nil
}

// Decode converts a driver value into an owned T. A failure returns a zero T.
func (c Codec[T]) Decode(source any) (T, error) {
	if err := c.Validate(); err != nil {
		return *new(T), err
	}
	decoded, err := c.decode(source)
	if err != nil {
		return *new(T), err
	}
	return decoded, nil
}

// Scan creates a standard SQL scanner. It changes the destination only after a
// successful decode; a NULL or malformed value cannot partially replace it.
func (c Codec[T]) Scan(destination *T) sql.Scanner { return scanner[T]{c, destination} }

type scanner[T any] struct {
	codec       Codec[T]
	destination *T
}

func (s scanner[T]) Scan(source any) error {
	if s.destination == nil {
		return invalid()
	}
	decoded, err := s.codec.Decode(source)
	if err != nil {
		return err
	}
	*s.destination = decoded
	return nil
}

// Validated applies one typed rule at both persistence boundaries. Generated
// enums use their own Validate method as the single source of membership rules.
func (c Codec[T]) Validated(validate func(T) error) Codec[T] {
	return typed(c.parameterType, func(v T) (driver.Value, error) {
		if validate == nil {
			return nil, invalid()
		}
		if err := validate(v); err != nil {
			return nil, err
		}
		return c.Bind(v)
	}, func(source any) (T, error) {
		if validate == nil {
			return *new(T), invalid()
		}
		v, err := c.Decode(source)
		if err != nil {
			return *new(T), err
		}
		return v, validate(v)
	}).withSensitivity(c.sensitive).withClone(c.clone)
}

// Nullable adds explicit SQL NULL without conflating it with T's zero value.
// Omitted mutation fields are handled by drafts, not by a column codec.
func Nullable[T any](base Codec[T]) Codec[value.Nullable[T]] {
	return typed(base.parameterType, func(v value.Nullable[T]) (driver.Value, error) {
		if base.encode == nil || base.decode == nil {
			return nil, invalid()
		}
		item, present := v.Get()
		if !present {
			return nil, nil
		}
		return base.Bind(item)
	}, func(source any) (value.Nullable[T], error) {
		if base.encode == nil || base.decode == nil {
			return value.Null[T](), invalid()
		}
		if source == nil {
			return value.Null[T](), nil
		}
		item, err := base.Decode(source)
		if err != nil {
			return value.Null[T](), err
		}
		return value.Of(item), nil
	}).withSensitivity(base.sensitive).withClone(func(v value.Nullable[T]) value.Nullable[T] {
		item, present := v.Get()
		if !present {
			return value.Null[T]()
		}
		return value.Of(base.Clone(item))
	})
}
