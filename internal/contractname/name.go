// Package contractname provides stable ASCII names for generated schema symbols.
package contractname

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"strings"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
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
	typesByID := make(map[contract.TypeID]contract.Type, len(types))
	for _, typ := range types {
		ids = append(ids, typ.ID)
		typesByID[typ.ID] = typ
	}
	slices.Sort(ids)
	ids = slices.Compact(ids)

	parents := make(map[contract.TypeID][]parentRef)
	for _, typ := range types {
		if typ.Kind == contract.ObjectKind {
			for _, p := range typ.Properties {
				if p.Type != "" {
					parents[p.Type] = append(parents[p.Type], parentRef{owner: typ.ID, field: p.Name})
					if child, ok := typesByID[p.Type]; ok && child.Kind == contract.ArrayKind && child.Element != "" {
						itemName := p.Name
						if strings.HasSuffix(itemName, "s") && len(itemName) > 1 {
							itemName = strings.TrimSuffix(itemName, "s")
						} else {
							itemName += "Item"
						}
						parents[child.Element] = append(parents[child.Element], parentRef{owner: typ.ID, field: itemName})
					}
				}
			}
		} else if typ.Kind == contract.ArrayKind && typ.Element != "" {
			parents[typ.Element] = append(parents[typ.Element], parentRef{owner: typ.ID, field: "Item"})
		} else if typ.Kind == contract.MapKind && typ.Element != "" {
			parents[typ.Element] = append(parents[typ.Element], parentRef{owner: typ.ID, field: "Value"})
		}
	}
	for id, refs := range parents {
		slices.SortFunc(refs, func(a, b parentRef) int {
			if c := strings.Compare(string(a.owner), string(b.owner)); c != 0 {
				return c
			}
			return strings.Compare(a.field, b.field)
		})
		parents[id] = refs
	}

	res := &resolver{typesByID: typesByID, parents: parents}
	result, used := make(map[contract.TypeID]string, len(ids)), make(map[string]contract.TypeID, len(ids))
	available := func(name string) bool { _, taken := used[name]; return !taken && (reserved == nil || !reserved(name)) }
	pending := ids
	for level := 0; len(pending) != 0; level++ {
		candidates := make(map[string][]contract.TypeID)
		var names []string
		for _, id := range pending {
			name, ok := res.readable(id, level, 0)
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
		// Hashing stays only as the absolute last resort when a type
		// is anonymous with no structural provenance or all qualification levels collide.
		name := Symbol(string(id))
		if !available(name) {
			return nil, safeerror.NewFault(fault.Conflict, "generated schema names collide")
		}
		used[name], result[id] = id, name
	}
	return result, nil
}

type parentRef struct {
	owner contract.TypeID
	field string
}

type resolver struct {
	typesByID map[contract.TypeID]contract.Type
	parents   map[contract.TypeID][]parentRef
}

// readable has a finite sequence of qualification levels. Structural names
// exhaust their candidates when their components do, allowing Schemas to reach
// the identity digest even when every readable candidate collides or is reserved.
func (r *resolver) readable(id contract.TypeID, level int, depth int) (string, bool) {
	if depth > 10 {
		return "", false
	}
	if id == "string" {
		if level == 0 {
			return "String", true
		}
		if level == 1 {
			return "GoString", true
		}
		return "", false
	}
	if id == "error" {
		if level == 0 {
			return "Error", true
		}
		if level == 1 {
			return "GoError", true
		}
		return "", false
	}
	if modelType, outerPkg, ok := parseModelID(string(id)); ok {
		return r.readableModelID(modelType, outerPkg, level, depth)
	}
	if strings.HasPrefix(string(id), "go:") {
		typ, hasTyp := r.typesByID[id]
		if !hasTyp {
			return "", false
		}
		switch typ.Kind {
		case contract.ArrayKind:
			elemName, ok := r.readable(typ.Element, level, depth+1)
			if !ok {
				return "", false
			}
			if length, hasLen := typ.Length.Get(); hasLen {
				res := fmt.Sprintf("Array_%d_%s", length, elemName)
				if len(res) > maxReadableBytes {
					return "", false
				}
				return res, true
			}
			res := "List_" + elemName
			if len(res) > maxReadableBytes {
				return "", false
			}
			return res, true

		case contract.MapKind:
			isLocale := typ.Key != nil && (strings.HasSuffix(string(typ.Key.Value.ID), "LocaleID") || strings.Contains(string(typ.Key.Value.ID), "Locale"))
			valName, valOK := r.readable(typ.Element, level, depth+1)
			if isLocale {
				if !valOK {
					return "", false
				}
				res := "LocaleMap_" + valName
				if len(res) > maxReadableBytes {
					return "", false
				}
				return res, true
			}
			keyID := contract.TypeID("string")
			if typ.Key != nil && typ.Key.Value.ID != "" {
				keyID = typ.Key.Value.ID
			}
			keyName, keyOK := r.readable(keyID, level, depth+1)
			if !keyOK && !valOK {
				return "", false
			}
			// One component can have fewer qualifiers than the other. Reuse its
			// base name only while the other still has a candidate at this level.
			if !keyOK {
				keyName, keyOK = r.readable(keyID, 0, depth+1)
			}
			if !valOK {
				valName, valOK = r.readable(typ.Element, 0, depth+1)
			}
			if !keyOK || !valOK {
				return "", false
			}
			res := fmt.Sprintf("Map_%s_%s", keyName, valName)
			if len(res) > maxReadableBytes {
				return "", false
			}
			return res, true

		case contract.AliasKind:
			targetName, ok := r.readable(typ.Element, level, depth+1)
			if !ok {
				return "", false
			}
			if typ.Nullable {
				res := "Nullable_" + targetName
				if len(res) > maxReadableBytes {
					return "", false
				}
				return res, true
			}
			return targetName, true

		case contract.ObjectKind:
			refs := r.parents[id]
			if len(refs) > 0 {
				primary := refs[0]
				ownerName, ok := r.readable(primary.owner, level, depth+1)
				if !ok {
					return "", false
				}
				fieldWord := sanitize(primary.field)
				if fieldWord != "" {
					fieldWord = strings.ToUpper(fieldWord[:1]) + fieldWord[1:]
				}
				res := ownerName + fieldWord
				if len(res) == 0 || len(res) > maxReadableBytes {
					return "", false
				}
				return res, true
			}
		}
		return "", false
	}
	return readable(string(id), level)
}

func parseModelID(id string) (modelType, outerPkg string, ok bool) {
	if !strings.HasSuffix(id, "]") {
		return "", "", false
	}
	open := strings.IndexByte(id, '[')
	if open < 0 {
		return "", "", false
	}
	outer := id[:open]
	dot := strings.LastIndexByte(outer, '.')
	if dot < 0 {
		return "", "", false
	}
	typeName := outer[dot+1:]
	pkg := outer[:dot]
	if typeName != "ID" {
		return "", "", false
	}
	if !(pkg == "model" || strings.HasSuffix(pkg, "/model") || strings.HasSuffix(pkg, ".model")) {
		return "", "", false
	}
	modelType = id[open+1 : len(id)-1]
	return modelType, pkg, true
}

func (r *resolver) readableModelID(modelType, outerPkg string, level int, depth int) (string, bool) {
	words := typeWords(modelType)
	if len(words) == 0 {
		return "", false
	}
	baseModel := words[len(words)-1]
	base := sanitize(baseModel) + "ID"
	if base == "ID" {
		return "", false
	}
	base = strings.ToUpper(base[:1]) + base[1:]
	if level == 0 {
		return base, true
	}
	modelElements := packageElements(modelType)
	if level <= len(modelElements) {
		elements := modelElements[len(modelElements)-level:]
		res := buildQualifiedName(elements, base)
		return res, res != ""
	}
	outerElements := packageElements(outerPkg)
	outerLevel := level - len(modelElements)
	if outerLevel <= len(outerElements) {
		elements := append(slices.Clone(outerElements[len(outerElements)-outerLevel:]), modelElements...)
		res := buildQualifiedName(elements, base)
		return res, res != ""
	}
	return "", false
}

func buildQualifiedName(elements []string, stem string) string {
	var name strings.Builder
	for _, element := range elements {
		word := sanitize(element)
		if word == "" {
			continue
		}
		if name.Len() != 0 {
			name.WriteByte('_')
		}
		name.WriteString(strings.ToUpper(word[:1]) + word[1:])
	}
	if stem != "" {
		if name.Len() != 0 {
			name.WriteByte('_')
		}
		name.WriteString(stem)
	}
	result := name.String()
	if result == "" || len(result) > maxReadableBytes {
		return ""
	}
	if result[0] >= '0' && result[0] <= '9' {
		result = "Type_" + result
	}
	return result
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
