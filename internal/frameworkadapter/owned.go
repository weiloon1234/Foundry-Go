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

// Seal can only be named inside this module, so Owned is implementable only by
// framework adapters.
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
	concrete := reflect.TypeOf(adapter)
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
