package config

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/weiloon1234/Foundry-Go/fault"
)

const fileSuffix = "_FILE"

// environmentValue reads NAME or NAME_FILE. Errors name the variable, never
// its value or the file contents.
func environmentValue(lookup Lookup, env string) (string, string, bool, error) {
	raw, direct := lookup(env)
	path, indirect := lookup(env + fileSuffix)
	switch {
	case direct && indirect:
		return "", "", false, fault.New(fault.Invalid, "both "+env+" and "+env+fileSuffix+" are set")
	case indirect:
		value, err := readValueFile(path)
		if err != nil {
			return "", "", false, fault.Wrap(fault.Invalid, "cannot read "+env+fileSuffix, err)
		}
		return value, "environment-file:" + env, true, nil
	default:
		return raw, "environment:" + env, direct, nil
	}
}

// readValueFile reads one bounded regular file, such as a mounted secret, and
// removes exactly one trailing newline.
func readValueFile(path string) (string, error) {
	if path == "" || strings.ContainsRune(path, 0) {
		return "", fault.New(fault.Invalid, "configuration file path is empty or invalid")
	}
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fault.New(fault.Invalid, "configuration value file must be a regular file")
	}
	data, err := io.ReadAll(io.LimitReader(file, MaxTableBytes+1))
	if err != nil {
		return "", err
	}
	if len(data) > MaxTableBytes {
		return "", fault.New(fault.Invalid, "configuration value file exceeds its byte limit")
	}
	text := string(data)
	if trimmed, ok := strings.CutSuffix(text, "\n"); ok {
		text = strings.TrimSuffix(trimmed, "\r")
	}
	return text, nil
}

type entryOverride struct {
	env, entry string
	path       []string
	value      string
}

// mergeEntryOverrides applies TABLE__ENTRY__FIELD[__NESTED...] variables to the
// JSON object text of a named collection. Values stay text, exactly as file
// layers provide them, so the element schema's decoders own every conversion.
func mergeEntryOverrides(environ []string, table, base string, mergeable bool) (string, bool, error) {
	prefix := table + "__"
	var overrides []entryOverride
	direct := make(map[string]bool)
	for _, pair := range environ {
		env, value, ok := strings.Cut(pair, "=")
		if !ok || !strings.HasPrefix(env, prefix) {
			continue
		}
		name, fromFile := strings.CutSuffix(env, fileSuffix)
		segments := strings.Split(strings.TrimPrefix(name, prefix), "__")
		if len(segments) < 2 {
			return "", false, fault.New(fault.Invalid, "named configuration override "+env+" requires an entry and field")
		}
		for i, segment := range segments {
			if segment == "" || strings.IndexFunc(segment, func(c rune) bool { return !(c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_') }) >= 0 || i == 0 && (strings.HasPrefix(segment, "_") || strings.HasSuffix(segment, "_")) {
				return "", false, fault.New(fault.Invalid, "invalid named configuration override "+env)
			}
			segments[i] = strings.ToLower(segment)
		}
		if fromFile {
			text, err := readValueFile(value)
			if err != nil {
				return "", false, fault.Wrap(fault.Invalid, "cannot read "+env, err)
			}
			value = text
		}
		if direct[name] {
			return "", false, fault.New(fault.Invalid, "both "+name+" and "+name+fileSuffix+" are set")
		}
		direct[name] = true
		overrides = append(overrides, entryOverride{env: env, entry: segments[0], path: segments[1:], value: value})
	}
	if len(overrides) == 0 {
		return "", false, nil
	}
	if base == "" {
		if !mergeable {
			return "", false, fault.New(fault.Invalid, "per-entry overrides for "+table+" require the collection from a file or "+table)
		}
		base = "{}"
	}
	decoder := json.NewDecoder(strings.NewReader(base))
	decoder.UseNumber()
	decoded, err := readObjectValue(decoder, 0)
	if err != nil {
		return "", false, fault.New(fault.Invalid, "named configuration "+table+" is not an object")
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return "", false, fault.New(fault.Invalid, "named configuration "+table+" requires one object")
	}
	root, ok := decoded.(map[string]any)
	if !ok {
		return "", false, fault.New(fault.Invalid, "named configuration "+table+" is not an object")
	}
	sort.Slice(overrides, func(i, j int) bool { return overrides[i].env < overrides[j].env })
	for _, override := range overrides {
		node := root
		for _, key := range append([]string{override.entry}, override.path[:len(override.path)-1]...) {
			child, exists := node[key]
			if !exists {
				child = make(map[string]any)
				node[key] = child
			}
			next, ok := child.(map[string]any)
			if !ok {
				return "", false, fault.New(fault.Invalid, "named configuration override "+override.env+" conflicts with a value")
			}
			node = next
		}
		node[override.path[len(override.path)-1]] = override.value
	}
	merged, err := json.Marshal(root)
	if err != nil {
		return "", false, err
	}
	return string(merged), true, nil
}
