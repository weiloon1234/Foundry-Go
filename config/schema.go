// Package config decodes layered settings through typed field declarations.
// Feature packages own their settings structs, keys, defaults and validation.
package config

import (
	"fmt"
	"net/netip"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/secret"
)

var settingName = regexp.MustCompile(`^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)*$`)

// Field is a sealed typed setting declaration, normally generated from a
// feature's settings type. It does not expose untyped getters to consumers.
type Field[T any] interface {
	snapshot() Field[T]
	name() string
	valid() bool
	sensitive() bool
	decode(*T, string) error
	assign(*T, any) error
	validate(*T) error
	table() bool
	entries(*T) int
}

func (k Key[T, V]) snapshot() Field[T] { return k }

// Key couples a configuration identifier to its concrete Go field and decoder.
// The field accessor is compiler-checked; no reflection-based field-name lookup
// or automatic dependency injection is performed.
type Key[T, V any] struct {
	id             string
	access         func(*T) *V
	parse          func(string) (V, error)
	private        bool
	check          func(V) error
	declarationErr error
}

// NewKey declares a field accessor and text decoder without resolving the field
// by reflection. Schema construction validates the declaration's name/functions.
func NewKey[T, V any](name string, field func(*T) *V, parse func(string) (V, error)) Key[T, V] {
	return Key[T, V]{id: name, access: field, parse: parse}
}
func (k Key[T, V]) Name() string            { return k.id }
func (k Key[T, V]) Sensitive() Key[T, V]    { k.private = true; return k }
func (k Key[T, V]) Set(value V) Override[T] { return Override[T]{name: k.id, value: value} }
func (k Key[T, V]) name() string            { return k.id }
func (k Key[T, V]) valid() bool {
	return settingName.MatchString(k.id) && k.access != nil && k.parse != nil && k.declarationErr == nil
}
func (k Key[T, V]) sensitive() bool { return k.private }

// table reports a named collection (a map keyed by a string kind) decoded
// from a JSON object, such as the generated connection tables.
func (k Key[T, V]) table() bool {
	typ := reflect.TypeFor[V]()
	return typ.Kind() == reflect.Map && typ.Key().Kind() == reflect.String
}
func (k Key[T, V]) entries(target *T) int {
	field := k.access(target)
	if field == nil {
		return 0
	}
	return reflect.ValueOf(*field).Len()
}
func (k Key[T, V]) validate(target *T) error {
	if k.check == nil {
		return nil
	}
	field := k.access(target)
	if field == nil {
		return fault.New(fault.Invalid, "setting accessor returned nil")
	}
	return k.check(*field)
}
func (k Key[T, V]) decode(target *T, raw string) error {
	value, err := k.parse(raw)
	if err != nil {
		return err
	}
	return k.assign(target, value)
}
func (k Key[T, V]) assign(target *T, value any) error {
	typed, ok := value.(V)
	if !ok {
		return fault.New(fault.Invalid, "override type does not match setting declaration")
	}
	field := k.access(target)
	if field == nil {
		return fault.New(fault.Invalid, "setting accessor returned nil")
	}
	*field = typed
	return nil
}

func String[T any](name string, field func(*T) *string) Key[T, string] {
	return Scalar(name, field)
}
func Int[T any](name string, field func(*T) *int) Key[T, int] {
	return Scalar(name, field)
}
func Bool[T any](name string, field func(*T) *bool) Key[T, bool] {
	return Scalar(name, field)
}
func Duration[T any](name string, field func(*T) *time.Duration) Key[T, time.Duration] {
	return NewKey(name, field, time.ParseDuration)
}
func Secret[T any](name string, field func(*T) *secret.String) Key[T, secret.String] {
	return NewKey(name, field, func(raw string) (secret.String, error) { return secret.New(raw), nil }).Sensitive()
}

// Override is produced by Key.Set, preserving the value type at the call site.
type Override[T any] struct {
	name  string
	value any
}

// Values is the text-value boundary implemented by file format adapters. Values
// use declared setting names; unknown file entries are errors, never ignored.
type Values struct {
	Name string
	Data map[string]string
}

// Lookup matches os.LookupEnv and is injectable for deterministic tests.
type Lookup func(string) (string, bool)

// Inputs always applies files in order, then environment, then typed overrides.
// Environment names use PREFIX__SECTION__FIELD for a section.field declaration.
// NAME_FILE instead reads the value from the named regular file (at most
// MaxTableBytes, one trailing newline removed); setting both is an error.
//
// Environ, such as os.Environ, additionally enables per-entry overrides inside
// named collections: PREFIX__SERVICES__DATABASE__CONNECTIONS__MAIN__PRIMARY__PASSWORD
// sets primary.password of entry "main" (entry names of lowercase letters,
// digits and single underscores). They merge into the collection supplied by a
// file or the collection's own variable, or create entries in an empty one,
// then decode through the same element schema. _FILE applies to them too.
type Inputs[T any] struct {
	Files       []Values
	Environment Lookup
	Environ     func() []string
	Prefix      string
	Overrides   []Override[T]
	Validate    func(T) error
}

// Entry records provenance without storing or formatting a setting's value.
type Entry struct {
	Name   string
	Source string
	Secret bool
}

// Report is immutable provenance, suitable for diagnostics without credentials.
type Report struct{ entries []Entry }

func (r Report) Entries() []Entry { return append([]Entry(nil), r.entries...) }

// Schema owns validated, ordered field declarations. It is safe for concurrent
// Load calls provided caller-supplied accessors/decoders are themselves safe.
type Schema[T any] struct {
	fields []Field[T]
	byName map[string]Field[T]
}

// New validates and snapshots a settings struct's field declarations. Duplicate
// names and environment-name collisions fail before any input is decoded.
func New[T any](fields ...Field[T]) (*Schema[T], error) {
	if reflect.TypeFor[T]().Kind() != reflect.Struct {
		return nil, fault.New(fault.Invalid, "configuration requires a settings struct")
	}
	schema := &Schema[T]{fields: append([]Field[T](nil), fields...), byName: make(map[string]Field[T])}
	envNames := make(map[string]string)
	for i, field := range schema.fields {
		if field == nil || (reflect.ValueOf(field).Kind() == reflect.Pointer && reflect.ValueOf(field).IsNil()) || !field.valid() {
			return nil, fault.New(fault.Invalid, "invalid configuration field declaration")
		}
		field = field.snapshot()
		schema.fields[i] = field
		name := field.name()
		if _, exists := schema.byName[name]; exists {
			return nil, fault.New(fault.Duplicate, "configuration field "+name+" is duplicated")
		}
		env := environmentName("", name)
		if prior, exists := envNames[env]; exists {
			return nil, fault.New(fault.Duplicate, "configuration environment names collide: "+prior+" and "+name)
		}
		schema.byName[name] = field
		envNames[env] = name
	}
	for env, name := range envNames {
		if prior, exists := envNames[env+"_FILE"]; exists {
			return nil, fault.New(fault.Duplicate, "configuration environment names collide with a _FILE variant: "+name+" and "+prior)
		}
	}
	for _, field := range schema.fields {
		name := field.name()
		for index := strings.LastIndexByte(name, '.'); index >= 0; index = strings.LastIndexByte(name, '.') {
			name = name[:index]
			if _, exists := schema.byName[name]; exists {
				return nil, fault.New(fault.Duplicate, "configuration field also owns a namespace: "+name)
			}
		}
	}
	return schema, nil
}

// Names returns declared setting names in declaration order. File adapters use
// this snapshot to resolve namespaces against the same schema as Load.
func (s *Schema[T]) Names() []string {
	names := make([]string, len(s.fields))
	for i, field := range s.fields {
		names[i] = field.name()
	}
	return names
}

// Load returns caller-owned configuration plus value-free provenance. Defaults
// and reference-containing override values are copied, so one application/load
// cannot mutate another application's defaults. Invalid loads return no partial
// configuration; parse and validation causes are retained but not formatted.
func (s *Schema[T]) Load(defaults T, inputs Inputs[T]) (result T, report Report, err error) {
	defer func() {
		if recover() != nil {
			var zero T
			result = zero
			report = Report{}
			err = fault.New(fault.Panicked, "configuration decoder or accessor panicked")
		}
	}()
	value, err := copyConfiguration(defaults)
	if err != nil {
		return result, report, err
	}
	sources := make(map[string]string, len(s.fields))
	for _, field := range s.fields {
		sources[field.name()] = "defaults"
	}
	// Named collections remember their last supplied text so per-entry
	// environment overrides merge into it instead of replacing it.
	tables := make(map[string]string)
	apply := func(name, raw, source string) error {
		field, exists := s.byName[name]
		if !exists {
			return fault.New(fault.Invalid, "unknown configuration field "+name+" in "+source)
		}
		if err := field.decode(&value, raw); err != nil {
			return fault.Wrap(fault.Invalid, "cannot decode configuration field "+name+" from "+source, err)
		}
		sources[name] = source
		if field.table() {
			tables[name] = raw
		}
		return nil
	}
	for _, file := range inputs.Files {
		if strings.TrimSpace(file.Name) == "" {
			return result, report, fault.New(fault.Invalid, "configuration source needs a name")
		}
		names := make([]string, 0, len(file.Data))
		for name := range file.Data {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			if err := apply(name, file.Data[name], file.Name); err != nil {
				return result, report, err
			}
		}
	}
	if inputs.Environment != nil {
		for _, field := range s.fields {
			env := environmentName(inputs.Prefix, field.name())
			raw, source, exists, err := environmentValue(inputs.Environment, env)
			if err != nil {
				return result, report, err
			}
			if exists {
				if err := apply(field.name(), raw, source); err != nil {
					return result, report, err
				}
			}
		}
	}
	if inputs.Environ != nil {
		environ := inputs.Environ()
		for _, field := range s.fields {
			if !field.table() {
				continue
			}
			env := environmentName(inputs.Prefix, field.name())
			base, supplied := tables[field.name()]
			merged, changed, err := mergeEntryOverrides(environ, env, base, supplied || field.entries(&value) == 0)
			if err != nil {
				return result, report, err
			}
			if changed {
				if err := apply(field.name(), merged, "environment:"+env+"__*"); err != nil {
					return result, report, err
				}
			}
		}
	}
	for _, override := range inputs.Overrides {
		field, exists := s.byName[override.name]
		if !exists {
			return result, report, fault.New(fault.Invalid, "override references an undeclared setting")
		}
		if err := field.assign(&value, override.value); err != nil {
			return result, report, fault.Wrap(fault.Invalid, "invalid override for "+override.name, err)
		}
		sources[override.name] = "override"
	}
	// Detach any mutable override input before validation or returning it.
	value, err = copyConfiguration(value)
	if err != nil {
		return result, report, err
	}
	for _, field := range s.fields {
		if err := field.validate(&value); err != nil {
			return result, Report{}, fault.Wrap(fault.Invalid, "invalid configuration field "+field.name(), err)
		}
	}
	if inputs.Validate != nil {
		if err := inputs.Validate(value); err != nil {
			return result, report, fault.Wrap(fault.Invalid, "configuration validation failed", err)
		}
	}
	for _, field := range s.fields {
		report.entries = append(report.entries, Entry{field.name(), sources[field.name()], field.sensitive()})
	}
	return value, report, nil
}

func environmentName(prefix, name string) string {
	name = strings.ToUpper(strings.ReplaceAll(name, ".", "__"))
	if prefix != "" {
		return prefix + "__" + name
	}
	return name
}

func copyConfiguration[T any](value T) (T, error) {
	var zero T
	copy, err := copyValue(reflect.ValueOf(&value).Elem(), 0)
	if err != nil {
		return zero, err
	}
	return copy.Interface().(T), nil
}

// Reflection is confined to cold-path ownership copying, not schema resolution.
// Cyclic/deep graphs and opaque mutable fields are rejected rather than shared.
func copyValue(value reflect.Value, depth int) (reflect.Value, error) {
	if depth > 64 {
		return reflect.Value{}, fault.New(fault.Invalid, "configuration contains a cycle or excessive nesting")
	}
	typ := value.Type()
	if immutableConfigurationType(typ) {
		return value, nil
	} // Native value types retain only immutable interned state.
	switch value.Kind() {
	case reflect.Bool, reflect.String, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Float32, reflect.Float64:
		return value, nil
	case reflect.Struct:
		out := reflect.New(typ).Elem()
		out.Set(value)
		for i := 0; i < value.NumField(); i++ {
			if typ.Field(i).PkgPath != "" {
				if mutableType(typ.Field(i).Type) {
					return reflect.Value{}, fault.New(fault.Invalid, "configuration has an opaque mutable field in "+typ.String())
				}
				continue
			}
			child, err := copyValue(value.Field(i), depth+1)
			if err != nil {
				return reflect.Value{}, err
			}
			out.Field(i).Set(child)
		}
		return out, nil
	case reflect.Pointer, reflect.Interface:
		if value.IsNil() {
			return reflect.Zero(typ), nil
		}
		child, err := copyValue(value.Elem(), depth+1)
		if err != nil {
			return reflect.Value{}, err
		}
		if value.Kind() == reflect.Pointer {
			out := reflect.New(typ.Elem())
			out.Elem().Set(child)
			return out, nil
		}
		out := reflect.New(typ).Elem()
		out.Set(child)
		return out, nil
	case reflect.Map:
		if typ.Key().Kind() != reflect.String {
			return reflect.Value{}, fault.New(fault.Invalid, "configuration maps require string keys")
		}
		if value.IsNil() {
			return reflect.Zero(typ), nil
		}
		out := reflect.MakeMapWithSize(typ, value.Len())
		iterator := value.MapRange()
		for iterator.Next() {
			child, err := copyValue(iterator.Value(), depth+1)
			if err != nil {
				return reflect.Value{}, err
			}
			out.SetMapIndex(iterator.Key(), child)
		}
		return out, nil
	case reflect.Array, reflect.Slice:
		if value.Kind() == reflect.Slice && value.IsNil() {
			return reflect.Zero(typ), nil
		}
		var out reflect.Value
		if value.Kind() == reflect.Array {
			out = reflect.New(typ).Elem()
		} else {
			out = reflect.MakeSlice(typ, value.Len(), value.Len())
		}
		for i := 0; i < value.Len(); i++ {
			child, err := copyValue(value.Index(i), depth+1)
			if err != nil {
				return reflect.Value{}, err
			}
			out.Index(i).Set(child)
		}
		return out, nil
	default:
		return reflect.Value{}, fault.New(fault.Invalid, fmt.Sprintf("configuration type %s cannot be copied safely", typ))
	}
}

func mutableType(typ reflect.Type) bool {
	if immutableConfigurationType(typ) {
		return false
	}
	switch typ.Kind() {
	case reflect.Pointer, reflect.Slice, reflect.Map, reflect.Interface, reflect.Func, reflect.Chan, reflect.UnsafePointer:
		return true
	case reflect.Array:
		// Empty type-marker arrays contain no value or mutable reference.
		return typ.Len() > 0 && mutableType(typ.Elem())
	case reflect.Struct:
		for i := 0; i < typ.NumField(); i++ {
			if mutableType(typ.Field(i).Type) {
				return true
			}
		}
	}
	return false
}

func immutableConfigurationType(typ reflect.Type) bool {
	return typ == reflect.TypeFor[time.Time]() || typ == reflect.TypeFor[netip.Prefix]()
}
