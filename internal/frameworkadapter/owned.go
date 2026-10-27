// Package frameworkadapter identifies adapters implemented by Foundry itself.
//
// Feature boundaries isolate application adapters in an owned goroutine so a
// panic or runtime.Goexit cannot escape. Framework adapters never call
// runtime.Goexit, so their I/O can use the cheaper in-goroutine panic boundary.
// Application codecs, loaders and handlers always remain isolated.
package frameworkadapter

import (
	"reflect"
	"strings"

	"github.com/weiloon1234/Foundry-Go/internal/callback"
)

// Seal is a method-set marker, not a capability: FoundryAdapter is a pure no-op
// and is never invoked. Generic inference can implement Owned outside this
// module; Is therefore also checks the concrete type's defining package.
type Seal struct{ _ struct{} }

// Owned marks a framework adapter. The method has no behavior.
type Owned interface {
	FoundryAdapter(Seal)
}

// modulePrefix is the framework module's import path prefix.
var modulePrefix = strings.TrimSuffix(reflect.TypeOf(Seal{}).PkgPath(), "internal/frameworkadapter")

// Is reports whether adapter's concrete type is a framework-owned
// implementation. The marker alone is not enough: a type declared outside the
// framework (an application, plugin or external test package) that embeds a
// framework adapter inherits the method by promotion, yet its own overriding
// methods are application code, so it stays isolated. Only the concrete type's
// defining package is inspected; no value is copied or compared.
func Is(adapter any) bool {
	if _, ok := adapter.(Owned); !ok {
		return false
	}
	return IsFrameworkType(adapter)
}

// IsFrameworkType reports whether a concrete type is defined by the framework.
// It does not trust promoted methods or execute application code. Callers must
// separately establish the required method contract.
func IsFrameworkType(value any) bool {
	concrete := reflect.TypeOf(value)
	if concrete == nil {
		return false
	}
	if concrete.Kind() == reflect.Pointer {
		concrete = concrete.Elem()
	}
	path := concrete.PkgPath()
	return strings.HasPrefix(path, modulePrefix) && !strings.HasSuffix(path, "_test")
}

// Call runs adapter I/O under the cheapest boundary that still contains every
// failure the adapter can produce: Invoke for framework adapters, Isolated for
// application adapters. Both wait for the callback's actual exit.
func Call(adapter any, operation string, fn func() error) error {
	if Is(adapter) {
		return callback.Invoke(operation, fn)
	}
	return callback.Isolated(operation, fn)
}
