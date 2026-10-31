package expr

import (
	"reflect"
	"unsafe"

	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/ormcore"
)

// JSONField binds a payload field to its JSON tag.
//
//	Example: expr.JSONFieldOf(func(p *Profile) *string {
//		return &p.Name
//	})
//
// Call Validate at startup. Owner and result types are invariant, even under conversion.
type JSONField[P, V any] struct {
	_       [0]*P
	_       [0]*V
	binding ormcore.JSONFieldBinding
}

// JSONFieldOf resolves a direct pointer selector against a fresh payload at startup.
// Ignored, ambiguous, foreign, zero-sized and opaque custom-encoded fields fail closed.
//
//	Example: expr.JSONFieldOf(func(p *Profile) *string {
//		return &p.Name
//	})
func JSONFieldOf[P, V any](selectField func(*P) *V) JSONField[P, V] {
	var binding ormcore.JSONFieldBinding
	err := callback.Isolated("declare JSON field", func() error {
		if selectField == nil {
			return ormcore.Invalid("missing JSON field selector")
		}
		p := new(P)
		ptr := selectField(p)
		binding = ormcore.JSONFieldAt(reflect.ValueOf(p).Elem(), unsafe.Pointer(ptr), reflect.TypeFor[V]())
		return nil
	})
	if err != nil {
		binding.Err = ormcore.Invalid("JSON field selector failed")
	}
	return JSONField[P, V]{binding: binding}
}

// Validate reports declaration errors before serving queries.
//
// Example: Theme.Validate()
func (f JSONField[P, V]) Validate() error { return f.binding.Validate() }

// JSONFixedArray binds a fixed Go array's element type without generated accessors.
//
//	Example: JSONFixedArrayOf(func(a *[3]string) *string {
//		return &a[0]
//	})
type JSONFixedArray[P, V any] struct {
	_     [0]*P
	_     [0]*V
	bound bool
	err   error
}

// JSONFixedArrayOf validates an element selector once; query-time indices remain int32.
//
//	Example: expr.JSONFixedArrayOf(func(p *[3]int64) *int64 {
//		return &p[0]
//	})
func JSONFixedArrayOf[P, V any](selectElement func(*P) *V) JSONFixedArray[P, V] {
	err := callback.Isolated("declare JSON array", func() error {
		if selectElement == nil {
			return ormcore.Invalid("missing JSON array selector")
		}
		p := new(P)
		ptr := selectElement(p)
		return ormcore.JSONArrayAt(reflect.ValueOf(p).Elem(), unsafe.Pointer(ptr), reflect.TypeFor[V]())
	})
	if err != nil {
		err = ormcore.Invalid("invalid JSON array declaration")
	}
	return JSONFixedArray[P, V]{bound: true, err: err}
}

// Validate reports invalid fixed-array declarations.
//
// Example: ThreeNames.Validate()
func (a JSONFixedArray[P, V]) Validate() error {
	if a.err != nil {
		return a.err
	}
	if !a.bound {
		return ormcore.Invalid("uninitialized JSON array declaration")
	}
	return nil
}
