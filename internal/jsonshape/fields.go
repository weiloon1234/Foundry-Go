// Package jsonshape owns JSON field/tag promotion for runtime validation and
// source generation. Adapters supply reflection or go/types metadata.
package jsonshape

import (
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/weiloon1234/Foundry-Go/internal/jsonwire"
)

// Field describes one Go struct field before JSON promotion.
type Field[T comparable] struct {
	Name, Tag           string
	Type                T
	Anonymous, Exported bool
}

// Adapter supplies the same structural facts from reflection and go/types.
type Adapter[T comparable] struct {
	NumFields func(T) (int, bool)
	// AllowStruct optionally restricts which structures may be expanded. Keep
	// NumFields factual so unexported embedded structs retain JSON promotion.
	AllowStruct  func(T) bool
	Field        func(T, int) Field[T]
	Pointer      func(T) (T, bool)
	Optional     func(T) bool
	QuotedScalar func(T) bool
}

// Property is a winning JSON property and its original Go field declaration.
type Property[T comparable] struct {
	Name, GoName     string
	Type             T
	Optional, Quoted bool
	Index            []int
}

type candidate[T comparable] struct {
	Property[T]
	tagged bool
	depth  int
}

// Collect applies shallow/tagged dominance, rejecting ambiguous properties.
// Work is bounded even when embedded declarations form a diamond or a cycle.
func Collect[T comparable](typ T, adapter Adapter[T]) ([]Property[T], error) {
	type embedded struct {
		typ       T
		optional  bool
		depth     int
		ancestors []T
		index     []int
	}
	pending := []embedded{{typ: typ}}
	candidates := make(map[string][]candidate[T])
	nodes := 0
	for len(pending) > 0 {
		current := pending[0]
		pending = pending[1:]
		if adapter.AllowStruct != nil && !adapter.AllowStruct(current.typ) {
			return nil, invalid()
		}
		if current.depth > jsonwire.MaxDepth {
			return nil, invalid()
		}
		count, ok := adapter.NumFields(current.typ)
		if !ok {
			return nil, invalid()
		}
		if count > jsonwire.MaxNodes-nodes {
			return nil, invalid()
		}
		for i := 0; i < count; i++ {
			field := adapter.Field(current.typ, i)
			nodes++
			if nodes > jsonwire.MaxNodes {
				return nil, invalid()
			}
			raw := field.Tag
			if raw == "-" {
				continue
			}
			name, options, _ := strings.Cut(raw, ",")
			if name != "" && !validTagName(name) {
				return nil, invalid()
			}
			optional, quoted := current.optional, false
			for _, option := range strings.Split(options, ",") {
				switch option {
				case "":
				case "omitempty", "omitzero":
					optional = true
				case "string":
					quoted = true
				default:
					return nil, invalid()
				}
			}
			base, pointer := adapter.Pointer(field.Type)
			if quoted && !adapter.QuotedScalar(base) {
				return nil, invalid()
			}
			_, structure := adapter.NumFields(base)
			if !field.Exported && (!field.Anonymous || !structure) {
				continue
			}
			index := append(slices.Clone(current.index), i)
			if field.Anonymous && name == "" && structure {
				if base == current.typ || slices.Contains(current.ancestors, base) {
					continue
				}
				ancestors := append(slices.Clone(current.ancestors), current.typ)
				pending = append(pending, embedded{base, optional || pointer, current.depth + 1, ancestors, index})
				continue
			}
			tagged := name != ""
			if name == "" {
				name = field.Name
			}
			optional = optional || adapter.Optional(field.Type)
			p := Property[T]{name, field.Name, field.Type, optional, quoted, index}
			candidates[name] = append(candidates[name], candidate[T]{p, tagged, current.depth})
		}
	}
	names := make([]string, 0, len(candidates))
	for name := range candidates {
		names = append(names, name)
	}
	slices.Sort(names)
	result := make([]Property[T], 0, len(names))
	for _, name := range names {
		options := candidates[name]
		winner, count := options[0], 1
		for _, candidate := range options[1:] {
			if candidate.depth < winner.depth || (candidate.depth == winner.depth && candidate.tagged && !winner.tagged) {
				winner, count = candidate, 1
			} else if candidate.depth == winner.depth && candidate.tagged == winner.tagged {
				count++
			}
		}
		if count != 1 {
			return nil, invalid()
		}
		result = append(result, winner.Property)
	}
	return result, nil
}

func invalid() error { return fmt.Errorf("invalid or ambiguous JSON field declaration") }

// Go's JSON compatibility mode may suppress malformed-tag errors and use a
// partial name. Reject those declarations so generated paths cannot disagree
// with value validation. Commas delimit options and are already separated.
func validTagName(name string) bool {
	return utf8.ValidString(name) && !strings.ContainsAny(name, "\\'\"`\x00")
}
