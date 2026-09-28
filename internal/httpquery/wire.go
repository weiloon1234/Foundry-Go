// Package httpquery owns bounded query-string parsing and URL generation.
// It does not bind fields or interpret scalar values; those belong to typed
// HTTP declarations. Query text uses net/url's form escaping, not path escaping.
package httpquery

import (
	"context"
	"net/url"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/weiloon1234/Foundry-Go/fault"
)

// Limits bounds encoded bytes and parameter slots. During parsing an empty
// slot between ampersands still counts toward Pairs. During encoding Pairs
// also bounds the number of map keys, including keys with no values.
type Limits struct {
	Bytes int
	Pairs int
}

func (l Limits) Validate() error {
	if l.Bytes <= 0 || l.Pairs <= 0 {
		return fault.New(fault.Invalid, "invalid query transport limits")
	}
	return nil
}

func checkContext(ctx context.Context, limits Limits) error {
	if err := limits.Validate(); err != nil {
		return err
	}
	if ctx == nil {
		return fault.New(fault.Invalid, "query transport requires a context")
	}
	return ctx.Err()
}

func invalidInput() error { return fault.New(fault.Invalid, "invalid query input") }
func exceeded() error     { return fault.New(fault.Invalid, "query transport limit exceeded") }

// Parse accepts a raw query without its leading question mark. It preserves
// repeated values and distinguishes an omitted key from a present empty value.
// As with net/url, a bare key has an empty value and empty slots are ignored.
// Invalid escapes, unescaped semicolons and invalid UTF-8 reject the entire
// query. Names and values never appear in error messages.
//
// Bytes and Pairs are checked before allocating decoded strings or containers.
// Decoded strings may share immutable input storage. Typed bindings decide
// whether a repeated key, unknown name, empty value or control character is valid.
func Parse(ctx context.Context, raw string, limits Limits) (url.Values, error) {
	if err := checkContext(ctx, limits); err != nil {
		return nil, err
	}
	if len(raw) > limits.Bytes {
		return nil, exceeded()
	}
	if raw != "" && strings.Count(raw, "&") >= limits.Pairs {
		return nil, exceeded()
	}
	values := make(url.Values)
	for raw != "" {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		var pair string
		pair, raw, _ = strings.Cut(raw, "&")
		if pair == "" {
			continue
		}
		if strings.ContainsRune(pair, ';') {
			return nil, invalidInput()
		}
		keyText, valueText, _ := strings.Cut(pair, "=")
		key, err := url.QueryUnescape(keyText)
		if err != nil || !utf8.ValidString(key) {
			return nil, invalidInput()
		}
		value, err := url.QueryUnescape(valueText)
		if err != nil || !utf8.ValidString(value) {
			return nil, invalidInput()
		}
		values[key] = append(values[key], value)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return values, nil
}

// Encode returns a query without a leading question mark. Keys are sorted and
// each key's value order is preserved, matching url.Values.Encode. Nil and empty
// value slices omit that key. Every failure returns an empty string.
//
// Input must remain unchanged until return. Native lengths and pair counts are
// checked before UTF-8 inspection or escaping. Escaping one component may use up
// to three times its native byte length; retained output never exceeds Bytes.
func Encode(ctx context.Context, values url.Values, limits Limits) (string, error) {
	if err := checkContext(ctx, limits); err != nil {
		return "", err
	}
	if len(values) > limits.Pairs {
		return "", exceeded()
	}
	remaining, pairs := limits.Bytes, 0
	for name, entries := range values {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		if len(entries) > limits.Pairs-pairs {
			return "", exceeded()
		}
		pairs += len(entries)
		for _, entry := range entries {
			if len(name) > remaining || len(entry) > remaining-len(name) {
				return "", exceeded()
			}
			remaining -= len(name) + len(entry)
			if !utf8.ValidString(name) || !utf8.ValidString(entry) {
				return "", fault.New(fault.Invalid, "invalid query output")
			}
		}
	}
	keys := make([]string, 0, len(values))
	for name, entries := range values {
		if len(entries) != 0 {
			keys = append(keys, name)
		}
	}
	slices.Sort(keys)
	var output strings.Builder
	for _, name := range keys {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		escapedName := url.QueryEscape(name)
		for _, entry := range values[name] {
			if err := ctx.Err(); err != nil {
				return "", err
			}
			escapedValue := url.QueryEscape(entry)
			remaining = limits.Bytes - output.Len()
			separator := 1 // The equals sign is always present.
			if output.Len() != 0 {
				separator++
			}
			if separator > remaining || len(escapedName) > remaining-separator || len(escapedValue) > remaining-separator-len(escapedName) {
				return "", exceeded()
			}
			if output.Len() != 0 {
				output.WriteByte('&')
			}
			output.WriteString(escapedName)
			output.WriteByte('=')
			output.WriteString(escapedValue)
		}
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return output.String(), nil
}
