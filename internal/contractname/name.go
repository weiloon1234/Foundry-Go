// Package contractname provides stable ASCII names for generated schema symbols.
package contractname

import (
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"strings"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/fault"
)

// maxReadableBytes bounds a readable name. A longer name is truncated and
// receives the identity digest, because a shortened name could mislead.
const maxReadableBytes = 64

// Schemas names every schema of one document readably and deterministically.
// A type's own Go name, including generic arguments (Page_Item for
// Page[Item]), is used when it is unique in the document and not reserved by
// the target language. Colliding names are qualified with trailing package
// path elements (billing.Invoice becomes Billing_Invoice); only a remaining
// collision uses Symbol's identity digest. Names contain no package path unless
// they need one, so moving a package keeps its names. A new declaration whose
// name collides qualifies both names; clients that must be immune to that can
// select types by identity through the generated ContractTypes index.
func Schemas(types []contract.Type, reserved func(string) bool) (map[contract.TypeID]string, error) {
	ids := make([]contract.TypeID, 0, len(types))
	for _, typ := range types {
		ids = append(ids, typ.ID)
	}
	slices.Sort(ids)
	ids = slices.Compact(ids)
	result, used := make(map[contract.TypeID]string, len(ids)), make(map[string]contract.TypeID, len(ids))
	available := func(name string) bool { _, taken := used[name]; return !taken && (reserved == nil || !reserved(name)) }
	pending := ids
	for level := 0; len(pending) != 0; level++ {
		candidates := make(map[string][]contract.TypeID)
		var names []string
		for _, id := range pending {
			name, ok := readable(string(id), level)
			if !ok {
				continue
			}
			if candidates[name] == nil {
				names = append(names, name)
			}
			candidates[name] = append(candidates[name], id)
		}
		if len(names) == 0 {
			break
		}
		slices.Sort(names)
		for _, name := range names {
			if owners := candidates[name]; len(owners) == 1 && available(name) {
				used[name], result[owners[0]] = owners[0], name
			}
		}
		pending = slices.DeleteFunc(pending, func(id contract.TypeID) bool { _, named := result[id]; return named })
	}
	for _, id := range pending {
		name := Symbol(string(id))
		if !available(name) {
			return nil, fault.New(fault.Conflict, "generated schema names collide")
		}
		used[name], result[id] = id, name
	}
	return result, nil
}

// readable returns the name of identity qualified by level trailing package
// path elements. It reports false when no such name exists: the identity has
// fewer package elements, or its readable form is anonymous or too long.
func readable(identity string, level int) (string, bool) {
	var prefix []string
	for {
		kind, rest, found := strings.Cut(identity, ":")
		if !found || !lowerWord(kind) {
			break
		}
		if kind == "go" {
			return "", false // An anonymous type identity is a digest.
		}
		prefix, identity = append(prefix, kind), rest
	}
	words := typeWords(identity)
	if len(words) == 0 {
		return "", false
	}
	if level > 0 {
		elements := packageElements(identity)
		if level > len(elements) {
			return "", false
		}
		words = append(slices.Clone(elements[len(elements)-level:]), words...)
	}
	words = append(prefix, words...)
	var name strings.Builder
	for _, word := range words {
		word = sanitize(word)
		if word == "" {
			continue
		}
		if name.Len() != 0 {
			name.WriteByte('_')
		}
		name.WriteString(strings.ToUpper(word[:1]) + word[1:])
	}
	result := name.String()
	if result == "" || len(result) > maxReadableBytes {
		return "", false
	}
	if result[0] >= '0' && result[0] <= '9' {
		result = "Type_" + result
	}
	return result, true
}

func lowerWord(text string) bool {
	if text == "" {
		return false
	}
	for _, r := range text {
		if r < 'a' || r > 'z' {
			return false
		}
	}
	return true
}

// typeWords reads a Go type spelling, keeping each type's own name without its
// package path: pkg/a.Page[pkg/b.Item] is Page, Item; []x.Item is List, Item.
// Quoted struct tags are skipped.
func typeWords(text string) []string {
	var words []string
	for i := 0; i < len(text); {
		switch c := text[i]; {
		case c == '"' || c == '`':
			i = skipQuoted(text, i)
		case c == '[' && i+1 < len(text) && text[i+1] == ']':
			words = append(words, "List")
			i += 2
		case c == '[' && i+1 < len(text) && text[i+1] >= '0' && text[i+1] <= '9':
			end := i + 1
			for end < len(text) && text[end] >= '0' && text[end] <= '9' {
				end++
			}
			if end < len(text) && text[end] == ']' {
				words = append(words, "Array"+text[i+1:end])
				i = end + 1
			} else {
				i++
			}
		case c == '*':
			words = append(words, "Ptr")
			i++
		case atomByte(c):
			end := i
			for end < len(text) && atomByte(text[end]) {
				end++
			}
			atom := text[i:end]
			if dot := strings.LastIndexByte(atom, '.'); dot >= 0 {
				atom = atom[dot+1:]
			}
			if atom != "" {
				words = append(words, atom)
			}
			i = end
		default:
			i++
		}
	}
	return words
}

// packageElements are the package path elements of the first qualified type in
// a spelling, for example billing and invoices for
// []example.com/billing/invoices.Invoice.
func packageElements(text string) []string {
	for i := 0; i < len(text); {
		if text[i] == '"' || text[i] == '`' {
			i = skipQuoted(text, i)
			continue
		}
		if !atomByte(text[i]) {
			i++
			continue
		}
		end := i
		for end < len(text) && atomByte(text[end]) {
			end++
		}
		atom := text[i:end]
		i = end
		dot := strings.LastIndexByte(atom, '.')
		if dot < 0 {
			continue
		}
		var elements []string
		for _, element := range strings.Split(atom[:dot], "/") {
			if element = sanitize(element); element != "" {
				elements = append(elements, element)
			}
		}
		return elements
	}
	return nil
}

func skipQuoted(text string, start int) int {
	quote := text[start]
	for i := start + 1; i < len(text); i++ {
		if text[i] == '\\' && quote != '`' {
			i++
			continue
		}
		if text[i] == quote {
			return i + 1
		}
	}
	return len(text)
}

// atomByte admits import path and identifier bytes. Other Unicode identifier
// bytes are replaced during sanitizing.
func atomByte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '.' || c == '/' || c == '-' || c == '~' || c >= 0x80
}

func sanitize(word string) string {
	var result strings.Builder
	for _, r := range word {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
			result.WriteRune(r)
		} else {
			result.WriteByte('_')
		}
	}
	return strings.Trim(result.String(), "_")
}

// Symbol is a stable digest-qualified name for one identity. It never depends
// on other declarations; use it where names must survive additions.
func Symbol(identity string) string {
	tail := identity
	if i := strings.LastIndexAny(tail, "/."); i >= 0 {
		tail = tail[i+1:]
	}
	var name strings.Builder
	for _, r := range tail {
		if name.Len() >= 40 {
			break
		}
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' {
			name.WriteRune(r)
		} else {
			name.WriteByte('_')
		}
	}
	stem := strings.Trim(name.String(), "_")
	if stem == "" {
		stem = "Type"
	}
	if stem[0] >= '0' && stem[0] <= '9' {
		stem = "Type_" + stem
	}
	digest := sha256.Sum256([]byte(identity))
	return strings.ToUpper(stem[:1]) + stem[1:] + "_" + hex.EncodeToString(digest[:8])
}
