package validation

import (
	"maps"
	"reflect"
	"slices"
	"strconv"
)

// MapKey is a native JSON object key: ordinary or named text or integers.
// Keys with custom JSON/text codecs are rejected because their wire names are
// not their native values.
type MapKey interface{ ~string | Integer }

// EachKey checks every key of a map. Entries are visited in ascending key order,
// so diagnostics are deterministic; each issue uses the entry's path, such as
// /labels/en. A nil or empty map has no keys. Metadata is server-only.
func EachKey[M ~map[K]V, K MapKey, V any](rules ...Rule[K]) Rule[M] {
	if scalarWireTransform[K]() {
		return failed[M](invalid("map validation requires native string or integer keys"))
	}
	child := All(rules...)
	return lift(Description{Kind: EachKeyKind, ServerOnly: true}, child, func(s *execution, input M, depth int) {
		for _, key := range sortedKeys(s, input) {
			s.enter(mapKeyText(key))
			child.run(s, key, depth+1)
			s.leave()
			if s.err != nil || s.truncated {
				return
			}
		}
	})
}

// EachValue checks every value of a map in ascending key order and reports at
// the entry's path. Use EachKey for key constraints.
func EachValue[M ~map[K]V, K MapKey, V any](rules ...Rule[V]) Rule[M] {
	if scalarWireTransform[K]() {
		return failed[M](invalid("map validation requires native string or integer keys"))
	}
	child := All(rules...)
	return lift(Description{Kind: EachValueKind, ServerOnly: true}, child, func(s *execution, input M, depth int) {
		for _, key := range sortedKeys(s, input) {
			s.enter(mapKeyText(key))
			child.run(s, input[key], depth+1)
			s.leave()
			if s.err != nil || s.truncated {
				return
			}
		}
	})
}

// sortedKeys refuses maps larger than the remaining work budget before sorting.
func sortedKeys[M ~map[K]V, K MapKey, V any](s *execution, input M) []K {
	if len(input) > s.remainingChecks() {
		s.err = &LimitError{}
		return nil
	}
	return slices.Sorted(maps.Keys(input))
}

func mapKeyText[K MapKey](key K) string {
	value := reflect.ValueOf(key)
	switch value.Kind() {
	case reflect.String:
		return value.String()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return strconv.FormatInt(value.Int(), 10)
	default:
		return strconv.FormatUint(value.Uint(), 10)
	}
}
