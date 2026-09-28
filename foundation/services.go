package foundation

import (
	"fmt"
	"reflect"
	"sync"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	pluginmanifest "github.com/weiloon1234/Foundry-Go/plugin/manifest"
)

// Key identifies one typed bootstrap service. Declare a key once and pass the
// resulting concrete dependency to domain constructors instead of resolving it
// inside request/business operations.
type Key[T any] struct{ name string }

// NewKey declares a service key. Its name is checked at registration/resolution.
func NewKey[T any](name string) Key[T] { return Key[T]{name: name} }
func (k Key[T]) Name() string          { return k.name }

type serviceRef struct {
	name string
	typ  reflect.Type
}

func ref[T any](key Key[T]) serviceRef { return serviceRef{key.name, reflect.TypeFor[T]()} }

// Resolver is a sealed, typed-bootstrap resolution capability. There is no
// public string-key lookup or exported map of application services.
type Resolver interface {
	resolve(serviceRef) (any, error)
	resolveAll(reflect.Type) ([]any, error)
	resolveCollection(collectionRef) ([]any, error)
}

// Resolve obtains a typed service during construction. The value type is
// inferred from its key; application code does not perform type assertions.
func Resolve[T any](r Resolver, key Key[T]) (T, error) {
	var zero T
	if nilValue(r) || !validName(key.name) {
		return zero, fault.New(fault.Invalid, "invalid service resolver or key")
	}
	value, err := r.resolve(ref(key))
	if err != nil {
		return zero, err
	}
	result, ok := value.(T)
	if !ok {
		return zero, fault.New(fault.Internal, "service value does not match its declared type")
	}
	return result, nil
}

// Provide binds an existing value. Resources requiring cleanup should be
// acquired during Boot and registered with Runtime.OnShutdown.
func Provide[T any](r *Registrar, key Key[T], value T) error {
	if nilValue(value) {
		return fault.New(fault.Invalid, "nil service value")
	}
	return Factory(r, key, func(Resolver) (T, error) { return value, nil })
}

// Factory registers an explicit constructor. Constructors run once, eagerly,
// after registration freezes. Missing/cyclic dependencies fail Build before any
// provider boots. Panics and Goexit report fault.Panicked and expire the resolver.
// Reflection records only the declared type; it never autowires.
func Factory[T any](r *Registrar, key Key[T], create func(Resolver) (T, error)) error {
	if r == nil || r.registry == nil || !validName(key.name) || create == nil {
		return fault.New(fault.Invalid, "invalid service registration")
	}
	return r.registry.add(serviceDefinition{
		ref: ref(key), owner: r.owner,
		create: func(resolver Resolver) (any, error) {
			value, err := create(resolver)
			if err != nil {
				return nil, err
			}
			if nilValue(value) {
				return nil, fault.New(fault.Invalid, "constructor returned a nil service")
			}
			return value, nil
		},
	}, r.override)
}

// Registrar accepts typed services and kernels during a provider's registration
// callback. Registrars retained after Build cannot mutate the frozen registry.
type Registrar struct {
	registry *registry
	owner    ProviderID
	override bool
	plugin   *pluginmanifest.Manifest
}

func (r *Registrar) Owner() ProviderID { return r.owner }

// Plugin returns the captured manifest when called in a plugin's registration.
func (r *Registrar) Plugin() (pluginmanifest.Manifest, bool) {
	if r == nil || r.plugin == nil {
		return pluginmanifest.Manifest{}, false
	}
	return r.plugin.Snapshot(), true
}

// Kernel registers one factory for a runtime kind. Duplicate kinds are errors.
func (r *Registrar) Kernel(kind KernelKind, factory KernelFactory) error {
	if r == nil || r.registry == nil || !kind.valid() || factory == nil {
		return fault.New(fault.Invalid, "invalid kernel registration")
	}
	r.registry.mu.Lock()
	defer r.registry.mu.Unlock()
	if r.registry.frozen {
		return fault.New(fault.Closed, "registration is frozen")
	}
	previous, exists := r.registry.kernels[kind]
	if r.override {
		if !exists {
			return fault.New(fault.Missing, "cannot override missing kernel "+string(kind))
		}
		if previous.replaces != "" {
			return fault.New(fault.Duplicate, "kernel override is already declared: "+string(kind))
		}
	} else if exists {
		return fault.New(fault.Duplicate, fmt.Sprintf("kernel %s belongs to both %s and %s", kind, previous.owner, r.owner))
	}
	definition := kernelDefinition{owner: r.owner, factory: factory}
	if r.override {
		definition.replaces = previous.owner
	}
	r.registry.kernels[kind] = definition
	return nil
}

type serviceDefinition struct {
	ref        serviceRef
	schema     reflect.Type
	collection collectionRef
	owner      ProviderID
	replaces   ProviderID
	create     func(Resolver) (any, error)
}
type kernelDefinition struct {
	owner    ProviderID
	replaces ProviderID
	factory  KernelFactory
}
type registry struct {
	mu       sync.Mutex
	frozen   bool
	order    []string
	services map[string]serviceDefinition
	kernels  map[KernelKind]kernelDefinition
}

func newRegistry() *registry {
	return &registry{services: make(map[string]serviceDefinition), kernels: make(map[KernelKind]kernelDefinition)}
}
func (r *registry) add(def serviceDefinition, replace bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.frozen {
		return fault.New(fault.Closed, "registration is frozen")
	}
	previous, exists := r.services[def.ref.name]
	if replace {
		if !exists {
			return fault.New(fault.Missing, "cannot override missing service "+def.ref.name)
		}
		if previous.ref.typ != def.ref.typ || previous.schema != def.schema || previous.collection != def.collection {
			return fault.New(fault.Invalid, "service override has a different type or schema: "+def.ref.name)
		}
		if previous.replaces != "" {
			return fault.New(fault.Duplicate, "service override is already declared: "+def.ref.name)
		}
		def.replaces = previous.owner
	} else if exists {
		return fault.New(fault.Duplicate, fmt.Sprintf("service %s belongs to both %s and %s", def.ref.name, previous.owner, def.owner))
	}
	r.services[def.ref.name] = def
	if !exists {
		r.order = append(r.order, def.ref.name)
	}
	return nil
}
func (r *registry) freeze() { r.mu.Lock(); r.frozen = true; r.mu.Unlock() }

// Services contains fully constructed, immutable bindings. Bound service
// objects manage their own concurrency; changing a binding is not supported.
type Services struct {
	values      map[string]serviceValue
	collections map[string]collectionRef
	order       []serviceRef
}
type serviceValue struct {
	typ   reflect.Type
	value any
}

func (s *Services) resolve(key serviceRef) (any, error) {
	value, ok := s.values[key.name]
	if !ok {
		return nil, fault.New(fault.Missing, "service "+key.name+" is not registered")
	}
	if value.typ != key.typ {
		return nil, fault.New(fault.Invalid, "service "+key.name+" was requested with a different type")
	}
	return value.value, nil
}

// constructionResolver belongs to one synchronous constructor chain. Once a
// constructor finishes, retaining its resolver cannot mutate service bindings.
type constructionResolver struct {
	mu     sync.Mutex
	build  *serviceBuild
	stack  []string
	active bool
}
type serviceBuild struct {
	order       []serviceRef
	definitions map[string]serviceDefinition
	values      map[string]serviceValue
}

func (r *constructionResolver) beginResolve() (func(), error) {
	if !r.mu.TryLock() {
		return nil, fault.New(fault.Conflict, "constructor dependencies must resolve sequentially")
	}
	if !r.active {
		r.mu.Unlock()
		return nil, fault.New(fault.Closed, "constructor resolver is no longer active")
	}
	return r.mu.Unlock, nil
}

func (r *constructionResolver) resolve(key serviceRef) (any, error) {
	release, err := r.beginResolve()
	if err != nil {
		return nil, err
	}
	defer release()
	return r.resolveLocked(key)
}

func (r *constructionResolver) resolveLocked(key serviceRef) (any, error) {
	if _, ok := r.build.values[key.name]; ok {
		return (&Services{values: r.build.values}).resolve(key)
	}
	def, ok := r.build.definitions[key.name]
	if !ok {
		return nil, fault.New(fault.Missing, "service "+key.name+" is not registered")
	}
	if def.ref.typ != key.typ {
		return nil, fault.New(fault.Invalid, "service "+key.name+" was requested with a different type")
	}
	for _, name := range r.stack {
		if name == key.name {
			return nil, fault.New(fault.Cycle, "constructor cycle at service "+key.name)
		}
	}
	child := &constructionResolver{build: r.build, stack: append(append([]string(nil), r.stack...), key.name), active: true}
	var value any
	err := callback.Isolated("construct service "+key.name, func() error {
		defer child.seal()
		var err error
		value, err = def.create(child)
		return err
	})
	if err != nil {
		return nil, err
	}
	r.build.values[key.name] = serviceValue{key.typ, value}
	return value, nil
}

func (r *constructionResolver) seal() { r.mu.Lock(); r.active = false; r.mu.Unlock() }

func (r *registry) construct() (*Services, error) {
	order := make([]serviceRef, len(r.order))
	for i, name := range r.order {
		order[i] = r.services[name].ref
	}
	build := &serviceBuild{definitions: r.services, values: make(map[string]serviceValue), order: order}
	resolver := &constructionResolver{build: build, active: true}
	defer resolver.seal()
	for _, name := range r.order {
		if _, err := resolver.resolve(r.services[name].ref); err != nil {
			return nil, err
		}
	}
	collections := make(map[string]collectionRef)
	for name, definition := range r.services {
		if definition.collection.name != "" {
			collections[name] = definition.collection
		}
	}
	return &Services{values: build.values, order: order, collections: collections}, nil
}
