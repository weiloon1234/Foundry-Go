package events

import (
	"reflect"
	"slices"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/identifier"
)

type registration struct {
	schema    schema
	listeners []listener
}
type registry map[topicKey]registration

func newRegistry(declarations []Declaration) (registry, error) {
	result := make(registry)
	for _, declaration := range declarations {
		schema := declaration.schema
		if err := validateTopic(schema.key, schema.typ); err != nil {
			return nil, err
		}
		if schema.parse == nil {
			return nil, fault.New(fault.Invalid, "event declaration requires a payload decoder")
		}

		entry, found := result[schema.key]
		if found && entry.schema.typ != schema.typ {
			return nil, fault.New(fault.Duplicate, "event name and version have conflicting payload types")
		}
		if !found {
			entry.schema = schema
		}
		names := make(map[ListenerID]struct{}, len(entry.listeners)+len(declaration.listeners))
		for _, item := range entry.listeners {
			names[item.name] = struct{}{}
		}
		for _, item := range declaration.listeners {
			if err := validateListener(item.name, item.invoke != nil); err != nil {
				return nil, err
			}
			if _, found := names[item.name]; found {
				return nil, fault.New(fault.Duplicate, "event listener is already registered")
			}
			names[item.name] = struct{}{}
		}
		entry.listeners = append(slices.Clone(entry.listeners), declaration.listeners...)
		result[schema.key] = entry
	}
	return result, nil
}

func (r registry) lookup(key topicKey, typ reflect.Type) (registration, error) {
	entry, found := r[key]
	if !found {
		return registration{}, fault.New(fault.Missing, "event topic is not registered")
	}
	if entry.schema.typ != typ {
		return registration{}, fault.New(fault.Invalid, "event payload type does not match its registered topic")
	}
	return entry, nil
}

func validateTopic(key topicKey, typ reflect.Type) error {
	if !identifier.Semantic(string(key.name)) || key.version == 0 || typ == nil || typ.Kind() == reflect.Interface {
		return fault.New(fault.Invalid, "event requires a semantic name, nonzero schema version and concrete payload type")
	}
	return nil
}
