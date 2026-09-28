// Package jsonwire owns bounded, lossless JSON parsing for typed values and transport.
package jsonwire

import (
	"bytes"
	"encoding/json"
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

func parsePolicy(data []byte, limits Limits, storage, canonical bool) (any, error) {
	if !limits.valid() || len(data) == 0 || len(data) > limits.Bytes || !utf8.Valid(data) || !validEscapes(data, storage) {
		return nil, invalid()
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	budget := parseBudget{limits: limits, storage: storage, canonical: canonical}
	root, err := read(decoder, 0, &budget)
	if err != nil {
		return nil, err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return nil, invalid()
	}
	return root, nil
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

func read(decoder *json.Decoder, depth int, budget *parseBudget) (any, error) {
	if depth > budget.limits.Depth || !budget.takeNode() {
		return nil, invalid()
	}
	token, err := decoder.Token()
	if err != nil {
		return nil, invalid()
	}
	switch v := token.(type) {
	case json.Delim:
		switch v {
		case '{':
			object := make(map[string]any)
			for decoder.More() {
				if !budget.takeNode() {
					return nil, invalid()
				}
				keyToken, err := decoder.Token()
				if err != nil {
					return nil, invalid()
				}
				key, ok := keyToken.(string)
				if !ok || budget.storage && strings.ContainsRune(key, 0) {
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
			if close, err := decoder.Token(); err != nil || close != json.Delim('}') {
				return nil, invalid()
			}
			return object, nil
		case '[':
			array := make([]any, 0)
			for decoder.More() {
				child, err := read(decoder, depth+1, budget)
				if err != nil {
					return nil, err
				}
				array = append(array, child)
			}
			if close, err := decoder.Token(); err != nil || close != json.Delim(']') {
				return nil, invalid()
			}
			return array, nil
		default:
			return nil, invalid()
		}
	case json.Number:
		number := string(v)
		if budget.canonical {
			var err error
			number, err = normalizeNumber(number)
			if err != nil {
				return nil, err
			}
		}
		if len(number) > budget.limits.Bytes-budget.numberBytes {
			return nil, invalid()
		}
		budget.numberBytes += len(number)
		return json.Number(number), nil
	case string:
		if budget.storage && strings.ContainsRune(v, 0) {
			return nil, invalid()
		}
		return v, nil
	case bool, nil:
		return v, nil
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
