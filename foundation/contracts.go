// Package foundation owns application assembly and lifecycle contracts shared
// by all Foundry kernels. It does not depend on concrete infrastructure drivers.
package foundation

import (
	"context"
	"reflect"
	"strings"
	"unicode"

	safeinvoke "github.com/weiloon1234/Foundry-Go/internal/callback"
)

// ProviderID identifies an application module or plugin contribution.
type ProviderID string

// TaskID identifies an owned background operation within its provider scope.
type TaskID string

// ResourceID identifies an owned cleanup within its provider scope.
type ResourceID string

// KernelKind selects one of the framework's five runtime kernels.
type KernelKind string

const (
	HTTP      KernelKind = "http"
	CLI       KernelKind = "cli"
	Worker    KernelKind = "worker"
	Scheduler KernelKind = "scheduler"
	WebSocket KernelKind = "websocket"
)

func (k KernelKind) valid() bool {
	switch k {
	case HTTP, CLI, Worker, Scheduler, WebSocket:
		return true
	default:
		return false
	}
}

// Kernel owns its accept/work loop and must cooperate with cancellation. Shared
// infrastructure remains alive until the loop and managed tasks have finished.
type Kernel interface{ Run(context.Context) error }

// KernelFunc adapts a function to a kernel.
type KernelFunc func(context.Context) error

func (f KernelFunc) Run(ctx context.Context) error { return f(ctx) }

// KernelFactory constructs a kernel from resolved services after providers boot.
type KernelFactory func(*Runtime) (Kernel, error)

// Provider registers constructors and kernel factories. Register must not open
// resources or start goroutines; resource acquisition belongs in Boot.
type Provider interface {
	ID() ProviderID
	Register(*Registrar) error
}

// Dependent declares providers that must register and boot before this one.
type Dependent interface{ Dependencies() []ProviderID }

// Booter optionally acquires resources after all services have been resolved.
// Register each acquired resource with Runtime.OnShutdown before continuing so
// partial startup failures are cleaned up by the framework.
type Booter interface {
	Boot(context.Context, *Runtime) error
}

// Module is a small function-based provider for application composition.
type Module struct {
	Name       ProviderID
	Requires   []ProviderID
	OnRegister func(*Registrar) error
	OnBoot     func(context.Context, *Runtime) error
}

func (m Module) ID() ProviderID             { return m.Name }
func (m Module) Dependencies() []ProviderID { return append([]ProviderID(nil), m.Requires...) }
func (m Module) Register(r *Registrar) error {
	if m.OnRegister != nil {
		return m.OnRegister(r)
	}
	return nil
}
func (m Module) Boot(ctx context.Context, r *Runtime) error {
	if m.OnBoot != nil {
		return m.OnBoot(ctx, r)
	}
	return nil
}

func validName(name string) bool {
	if name == "" || strings.TrimSpace(name) != name {
		return false
	}
	for _, r := range name {
		if unicode.IsControl(r) || unicode.IsSpace(r) {
			return false
		}
	}
	return true
}

func nilValue(value any) bool {
	if value == nil {
		return true
	}
	rv := reflect.ValueOf(value)
	switch rv.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return rv.IsNil()
	}
	return false
}

// invoke isolates extension panics. Panic payloads are deliberately not included
// in returned diagnostics because they can contain credentials or request data.
func invoke(operation string, callback func() error) (err error) {
	return safeinvoke.Invoke(operation, callback)
}
