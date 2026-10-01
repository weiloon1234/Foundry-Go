// Package migrate declares immutable, versioned schema changes and validates
// migration history. Models describe current types; these definitions preserve
// historical changes and are never inferred or applied during application boot.
package migrate

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/dependency"
	"github.com/weiloon1234/Foundry-Go/internal/identifier"
)

// Origin identifies the application, framework, or plugin owning a migration.
type Origin string

// ID is a stable migration identifier within an origin. Sortable numeric prefixes
// are conventional, but explicit Requires dependencies take precedence.
type ID string

// Version records the release that introduced a migration, not the currently
// installed framework/plugin version. Changing it changes historical identity.
type Version string

// Key identifies one migration without ambiguous delimiter concatenation.
type Key struct {
	Origin Origin `json:"origin"`
	ID     ID     `json:"id"`
}

// ExecutionMode selects atomic execution or individually journaled statements.
// The zero value preserves the historical transactional checksum and behavior.
type ExecutionMode string

const (
	Transactional    ExecutionMode = ""
	NonTransactional ExecutionMode = "nontransactional"
)

// Definition is the handwritten or generated source of an immutable migration.
// SQL entries execute in order using Mode (transactional by default). Requires
// references declared migrations; New snapshots all slices and computes hashes.
//
// Down optionally reverses the migration for an explicit Postgres.Rollback.
// It is excluded from the checksum, so adding or correcting Down never drifts
// applied history, and it must be valid inside one transaction: a
// nontransactional migration cannot declare Down. Without Down a migration is
// irreversible and rollback refuses to cross it.
type Definition struct {
	Mode     ExecutionMode
	Key      Key
	Version  Version
	SQL      []string
	Down     []string
	Requires []Key
}

// Clone returns a copy that shares no SQL, Down or Requires storage with d.
// Use it wherever a caller-owned definition is snapshotted before New.
func (d Definition) Clone() Definition {
	d.SQL = slices.Clone(d.SQL)
	d.Down = slices.Clone(d.Down)
	d.Requires = slices.Clone(d.Requires)
	return d
}

// Entry is immutable migration metadata, returned as a caller-owned snapshot.
// Checksum covers origin, ID, introduced version, ordered SQL and dependencies.
type Entry struct {
	Mode       ExecutionMode `json:"mode,omitempty"`
	Key        Key           `json:"key"`
	Version    Version       `json:"version"`
	Checksum   Checksum      `json:"checksum"`
	Requires   []Key         `json:"requires,omitempty"`
	Reversible bool          `json:"reversible,omitempty"`
}

type migration struct {
	entry      Entry
	statements []string
	down       []string
}

// Registry owns validated definitions in deterministic dependency order. It is
// safe to share across goroutines; returned metadata and plans are copies.
type Registry struct {
	ordered []migration
	byKey   map[Key]int
}

func validKey(key Key) bool {
	return validName(string(key.Origin)) && validName(string(key.ID))
}

func validName(name string) bool {
	return identifier.Semantic(name)
}

// New validates definitions before any database operation. Duplicate keys,
// invalid dependencies, and cycles are errors. Independent nodes sort by ID,
// then origin. Dependency lists are canonicalized; SQL ordering is preserved.
func New(definitions ...Definition) (*Registry, error) {
	byKey := make(map[Key]migration, len(definitions))
	for _, definition := range definitions {
		if (definition.Mode != Transactional && definition.Mode != NonTransactional) || !validKey(definition.Key) || !validName(string(definition.Version)) || len(definition.SQL) == 0 {
			return nil, fault.New(fault.Invalid, "invalid migration definition")
		}
		if _, exists := byKey[definition.Key]; exists {
			return nil, fault.New(fault.Duplicate, "migration key is duplicated: "+keyLabel(definition.Key))
		}
		statements := append([]string(nil), definition.SQL...)
		down := append([]string(nil), definition.Down...)
		for _, statement := range append(slices.Clone(statements), down...) {
			if strings.TrimSpace(statement) == "" || !utf8.ValidString(statement) || strings.ContainsRune(statement, 0) {
				return nil, fault.New(fault.Invalid, "invalid SQL in migration "+keyLabel(definition.Key))
			}
		}
		if len(down) != 0 && definition.Mode == NonTransactional {
			return nil, fault.New(fault.Invalid, "nontransactional migration "+keyLabel(definition.Key)+" cannot declare Down SQL")
		}
		requires := append([]Key(nil), definition.Requires...)
		sortKeys(requires)
		for i, key := range requires {
			if !validKey(key) || key == definition.Key {
				return nil, fault.New(fault.Invalid, "invalid migration dependency in "+keyLabel(definition.Key))
			}
			if i > 0 && key == requires[i-1] {
				return nil, fault.New(fault.Duplicate, "migration dependency is duplicated")
			}
		}
		// A length-aware encoding prevents concatenation collisions between SQL
		// statements, metadata, and dependency fields. Hash format is versioned.
		canonical := struct {
			Format   int
			Key      Key
			Version  Version
			SQL      []string
			Requires []Key
			Mode     ExecutionMode `json:",omitempty"`
		}{1, definition.Key, definition.Version, statements, requires, definition.Mode}
		encoded, err := json.Marshal(canonical)
		if err != nil {
			return nil, fault.Wrap(fault.Internal, "cannot encode migration definition", err)
		}
		byKey[definition.Key] = migration{entry: Entry{Mode: definition.Mode, Key: definition.Key, Version: definition.Version, Checksum: Checksum(sha256.Sum256(encoded)), Requires: requires, Reversible: len(down) != 0}, statements: statements, down: down}
	}
	keys := make([]Key, 0, len(byKey))
	for key := range byKey {
		keys = append(keys, key)
	}
	sortKeys(keys)
	ordered, failure := dependency.Order(keys, func(key Key) ([]Key, bool) { item, exists := byKey[key]; return item.entry.Requires, exists })
	if failure != nil {
		if failure.Kind == dependency.Cycle {
			return nil, fault.New(fault.Cycle, "migration dependency cycle at "+keyLabel(failure.Key))
		}
		return nil, fault.New(fault.Missing, "migration dependency is not registered: "+keyLabel(failure.Key))
	}
	registry := &Registry{byKey: make(map[Key]int, len(byKey))}
	for _, key := range ordered {
		item := byKey[key]
		registry.byKey[key] = len(registry.ordered)
		registry.ordered = append(registry.ordered, item)
	}
	return registry, nil
}

// Entries returns metadata in execution order without exposing owned slices.
func (r *Registry) Entries() []Entry {
	entries := make([]Entry, len(r.ordered))
	for i, item := range r.ordered {
		entries[i] = copyEntry(item.entry)
	}
	return entries
}

// Definition returns an owned copy of a registered definition, including its
// SQL and Down statements, for read-only inspection such as migrate show.
func (r *Registry) Definition(key Key) (Definition, bool) {
	index, exists := r.byKey[key]
	if !exists {
		return Definition{}, false
	}
	item := r.ordered[index]
	return Definition{Mode: item.entry.Mode, Key: item.entry.Key, Version: item.entry.Version, SQL: item.statements, Down: item.down, Requires: item.entry.Requires}.Clone(), true
}

func copyEntry(entry Entry) Entry {
	entry.Requires = append([]Key(nil), entry.Requires...)
	return entry
}
func sortKeys(keys []Key) {
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].ID != keys[j].ID {
			return keys[i].ID < keys[j].ID
		}
		return keys[i].Origin < keys[j].Origin
	})
}
func keyLabel(key Key) string { return fmt.Sprintf("(%s, %s)", key.Origin, key.ID) }
