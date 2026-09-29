// Package jsonwire owns bounded, lossless JSON parsing for typed values and transport.
package jsonwire

import (
	"bytes"
	"encoding/json"
	"encoding/json/jsontext"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/weiloon1234/Foundry-Go/fault"
)

const (
	MaxBytes = 1 << 20
	MaxDepth = 64
	MaxNodes = 10000
)

func invalid() error {
	return fault.New(fault.Invalid, "invalid JSON syntax, representation or resource bound")
}

// Bound failures name the exceeded limit. The messages are static and safe for
// redacted diagnostics; they never include document content.
func byteBound() error  { return fault.New(fault.Invalid, "JSON document exceeds its byte bound") }
func depthBound() error { return fault.New(fault.Invalid, "JSON document exceeds its depth bound") }
func nodeBound() error  { return fault.New(fault.Invalid, "JSON document exceeds its node bound") }

// Parse rejects duplicate object keys, lossy Unicode and PostgreSQL text NUL.
// Numbers retain exact canonical decimal text, never an intermediate float.
func Parse(data []byte) (string, any, error) {
	root, err := parse(data, Limits{Bytes: MaxBytes, Depth: MaxDepth, Nodes: MaxNodes}, true)
	if err != nil {
		return "", nil, err
	}
	encoded, err := json.Marshal(root)
	if err != nil || len(encoded) > MaxBytes {
		return "", nil, invalid()
	}
	return string(encoded), root, nil
}

// Decode reads a transport document without storage-specific normalization.
// Numbers retain their exact input lexeme as json.Number. Escaped NUL is valid
// JSON and remains intact; codecs and application validation can reject it.
// Callers bound the body before supplying its bytes. No canonical copy is made.
func Decode(data []byte, limits Limits) (any, error) {
	return parse(data, limits, false)
}

func parse(data []byte, limits Limits, storage bool) (any, error) {
	return parsePolicy(data, limits, storage, storage)
}

// Canonical preserves transport strings (including escaped NUL), sorts object
// names, normalizes exact decimal numbers and retains array order. It shares the
// same parser and budgets as Decode and storage Parse.
func Canonical(data []byte, limits Limits) ([]byte, error) {
	root, err := parsePolicy(data, limits, false, true)
	if err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(root)
	if err != nil || len(encoded) > limits.Bytes {
		return nil, invalid()
	}
	return encoded, nil
}

// newDecoder reads the caller's bytes in place (a bytes.Buffer is not copied).
// Duplicate names are detected by the tree builder with the same exact-name map.
func newDecoder(data []byte) *jsontext.Decoder {
	return jsontext.NewDecoder(bytes.NewBuffer(data), jsontext.AllowDuplicateNames(true))
}

func precheck(data []byte, limits Limits, storage bool) error {
	if !limits.valid() || len(data) == 0 {
		return invalid()
	}
	if len(data) > limits.Bytes {
		return byteBound()
	}
	if !utf8.Valid(data) || !validEscapes(data, storage) {
		return invalid()
	}
	return nil
}

func parsePolicy(data []byte, limits Limits, storage, canonical bool) (any, error) {
	if err := precheck(data, limits, storage); err != nil {
		return nil, err
	}
	decoder := newDecoder(data)
	budget := parseBudget{limits: limits, storage: storage, canonical: canonical}
	root, err := read(decoder, 0, &budget)
	if err != nil {
		return nil, err
	}
	if _, err := decoder.ReadToken(); err != io.EOF {
		return nil, invalid()
	}
	return root, nil
}

// Measure bounds a document already produced by a validating encoder, such as
// encoding/json/v2 with duplicate names and invalid UTF-8 rejected. It rechecks
// bytes, UTF-8 and escapes, then counts depth and nodes exactly as Decode does,
// in one allocation-free pass without building a tree. It does not re-validate
// general JSON syntax; use Decode for untrusted input.
func Measure(data []byte, limits Limits) error {
	if err := precheck(data, limits, false); err != nil {
		return err
	}
	var objects [MaxDepth + 1]bool
	depth, nodes := 0, 0
	name := false
	value := func() error {
		if nodes >= limits.Nodes {
			return nodeBound()
		}
		nodes++
		if depth > limits.Depth {
			return depthBound()
		}
		return nil
	}
	for i := 0; i < len(data); {
		switch c := data[i]; c {
		case ' ', '\t', '\n', '\r', ':':
			i++
		case ',':
			name = depth > 0 && objects[depth-1]
			i++
		case '{', '[':
			if err := value(); err != nil {
				return err
			}
			if depth >= len(objects) {
				return depthBound()
			}
			objects[depth] = c == '{'
			depth++
			name = c == '{'
			i++
		case '}', ']':
			if depth == 0 {
				return invalid()
			}
			depth--
			name = false
			i++
		case '"':
			if name {
				// An object name consumes a node but is not depth-checked.
				if nodes >= limits.Nodes {
					return nodeBound()
				}
				nodes++
				name = false
			} else if err := value(); err != nil {
				return err
			}
			for i++; i < len(data) && data[i] != '"'; i++ {
				if data[i] == '\\' {
					i++
				}
			}
			i++
		default:
			if err := value(); err != nil {
				return err
			}
			for i < len(data) && !delimiter(data[i]) {
				i++
			}
		}
	}
	if depth != 0 || nodes == 0 {
		return invalid()
	}
	return nil
}

func delimiter(c byte) bool {
	switch c {
	case ',', ']', '}', ':', ' ', '\t', '\n', '\r':
		return true
	}
	return false
}

type parseBudget struct {
	nodes, numberBytes int
	limits             Limits
	storage            bool
	canonical          bool
}

func (b *parseBudget) takeNode() bool {
	if b.nodes >= b.limits.Nodes {
		return false
	}
	b.nodes++
	return true
}

func read(decoder *jsontext.Decoder, depth int, budget *parseBudget) (any, error) {
	if depth > budget.limits.Depth {
		return nil, depthBound()
	}
	if !budget.takeNode() {
		return nil, nodeBound()
	}
	token, err := decoder.ReadToken()
	if err != nil {
		return nil, invalid()
	}
	switch token.Kind() {
	case '{':
		object := make(map[string]any)
		for decoder.PeekKind() != '}' {
			if !budget.takeNode() {
				return nil, nodeBound()
			}
			keyToken, err := decoder.ReadToken()
			if err != nil || keyToken.Kind() != '"' {
				return nil, invalid()
			}
			key := keyToken.String()
			if budget.storage && strings.ContainsRune(key, 0) {
				return nil, invalid()
			}
			if _, exists := object[key]; exists {
				return nil, invalid()
			}
			child, err := read(decoder, depth+1, budget)
			if err != nil {
				return nil, err
			}
			object[key] = child
		}
		if _, err := decoder.ReadToken(); err != nil {
			return nil, invalid()
		}
		return object, nil
	case '[':
		array := make([]any, 0)
		for decoder.PeekKind() != ']' {
			child, err := read(decoder, depth+1, budget)
			if err != nil {
				return nil, err
			}
			array = append(array, child)
		}
		if _, err := decoder.ReadToken(); err != nil {
			return nil, invalid()
		}
		return array, nil
	case '0':
		number := token.String()
		if budget.canonical {
			var err error
			number, err = normalizeNumber(number)
			if err != nil {
				return nil, err
			}
		}
		if len(number) > budget.limits.Bytes-budget.numberBytes {
			return nil, byteBound()
		}
		budget.numberBytes += len(number)
		return json.Number(number), nil
	case '"':
		text := token.String()
		if budget.storage && strings.ContainsRune(text, 0) {
			return nil, invalid()
		}
		return text, nil
	case 't':
		return true, nil
	case 'f':
		return false, nil
	case 'n':
		return nil, nil
	default:
		return nil, invalid()
	}
}

// encoding/json replaces lone UTF-16 surrogates with U+FFFD. Reject those
// inputs before decoding so stored and transport values never change identity.
func validEscapes(data []byte, rejectNUL bool) bool {
	quoted := false
	for i := 0; i < len(data); i++ {
		if data[i] == '"' {
			quoted = !quoted
			continue
		}
		if !quoted || data[i] != '\\' {
			continue
		}
		i++
		if i >= len(data) {
			return false
		}
		if data[i] != 'u' {
			continue
		}
		unit, ok := hexUnit(data, i+1)
		if !ok || rejectNUL && unit == 0 {
			return false
		}
		i += 4
		if unit >= 0xdc00 && unit <= 0xdfff {
			return false
		}
		if unit >= 0xd800 && unit <= 0xdbff {
			if i+2 >= len(data) || data[i+1] != '\\' || data[i+2] != 'u' {
				return false
			}
			low, ok := hexUnit(data, i+3)
			if !ok || low < 0xdc00 || low > 0xdfff {
				return false
			}
			i += 6
		}
	}
	return !quoted
}
func hexUnit(data []byte, start int) (uint16, bool) {
	if start+4 > len(data) {
		return 0, false
	}
	var result uint16
	for _, c := range data[start : start+4] {
		result *= 16
		switch {
		case c >= '0' && c <= '9':
			result += uint16(c - '0')
		case c >= 'a' && c <= 'f':
			result += uint16(c - 'a' + 10)
		case c >= 'A' && c <= 'F':
			result += uint16(c - 'A' + 10)
		default:
			return 0, false
		}
	}
	return result, true
}
