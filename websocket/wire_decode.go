package websocket

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"unicode/utf8"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/identifier"
)

var errWireEnvelope = fault.New(fault.Invalid, "invalid wire envelope")

// wireMember decodes one registered object member from the shared decoder.
type wireMember func(*json.Decoder) error

func into(target any) wireMember {
	return func(decoder *json.Decoder) error { return decoder.Decode(target) }
}

// decodeObject strictly decodes one top-level JSON object in a single pass. It
// rejects invalid UTF-8, unpaired surrogate escapes, duplicate or unknown
// members and trailing data. Present reports each decoded member so callers can
// distinguish an explicit null from omission. Nested values are decoded by their
// registered targets; payload documents are validated later by their contract.
func decodeObject(data []byte, maxBytes int, member func(string) wireMember) (map[string]bool, error) {
	if len(data) == 0 || len(data) > maxBytes || !utf8.Valid(data) || !pairedSurrogates(data) {
		return nil, errWireEnvelope
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	if token, err := decoder.Token(); err != nil || token != json.Delim('{') {
		return nil, errWireEnvelope
	}
	present := make(map[string]bool, 8)
	for decoder.More() {
		token, err := decoder.Token()
		name, ok := token.(string)
		if err != nil || !ok || present[name] {
			return present, errWireEnvelope
		}
		decode := member(name)
		if decode == nil {
			return present, errWireEnvelope
		}
		present[name] = true
		if err := decode(decoder); err != nil {
			return present, errWireEnvelope
		}
	}
	if token, err := decoder.Token(); err != nil || token != json.Delim('}') {
		return present, errWireEnvelope
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return present, errWireEnvelope
	}
	return present, nil
}

// maxLeadingMembers bounds the members leadingRequestID inspects.
const maxLeadingMembers = 4

// leadingRequestID recovers a valid request ID from the first members of a
// frame, skipping only scalar members before it, without decoding the rest.
// Generated clients send "v", "action" and "id" first. Anything else, such as
// an ID after a nested value, yields no ID.
func leadingRequestID(data []byte) RequestID {
	decoder := json.NewDecoder(bytes.NewReader(data))
	if token, err := decoder.Token(); err != nil || token != json.Delim('{') {
		return ""
	}
	for range maxLeadingMembers {
		key, err := decoder.Token()
		name, ok := key.(string)
		if err != nil || !ok {
			return ""
		}
		value, err := decoder.Token()
		if err != nil {
			return ""
		}
		if _, nested := value.(json.Delim); nested {
			return ""
		}
		if name == "id" {
			if text, ok := value.(string); ok && identifier.Semantic(text) {
				return RequestID(text)
			}
			return ""
		}
	}
	return ""
}

// pairedSurrogates rejects \u escapes that encode an unpaired UTF-16 surrogate,
// which a standard decoder would silently replace. Backslashes occur only in
// JSON strings, so a linear scan of escapes is exact for valid documents.
func pairedSurrogates(data []byte) bool {
	for i := 0; i < len(data); i++ {
		if data[i] != '\\' {
			continue
		}
		i++
		if i >= len(data) || data[i] != 'u' {
			continue
		}
		value, ok := hex4(data, i+1)
		if !ok {
			continue // Malformed escapes fail ordinary decoding.
		}
		i += 4
		switch {
		case value >= 0xdc00 && value <= 0xdfff:
			return false
		case value >= 0xd800 && value <= 0xdbff:
			if i+2 >= len(data) || data[i+1] != '\\' || data[i+2] != 'u' {
				return false
			}
			low, ok := hex4(data, i+3)
			if !ok || low < 0xdc00 || low > 0xdfff {
				return false
			}
			i += 6
		}
	}
	return true
}
func hex4(data []byte, offset int) (rune, bool) {
	if offset+4 > len(data) {
		return 0, false
	}
	var value rune
	for _, b := range data[offset : offset+4] {
		value <<= 4
		switch {
		case b >= '0' && b <= '9':
			value |= rune(b - '0')
		case b >= 'a' && b <= 'f':
			value |= rune(b-'a') + 10
		case b >= 'A' && b <= 'F':
			value |= rune(b-'A') + 10
		default:
			return 0, false
		}
	}
	return value, true
}
