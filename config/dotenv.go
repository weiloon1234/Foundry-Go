package config

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

// EnvFile selects one explicit dotenv document for Inputs. It must be a regular
// UTF-8 file of at most 1 MiB, without NUL bytes or duplicate keys. Values use
// literal quoting, with no interpolation, shell execution or export keyword.
// Schema.Load owns opening and closing the file and never mutates os.Environ.
//
// Example: config.Inputs[Settings]{EnvFile: config.EnvFile{Path: path, Name: "local.env"}, Environment: os.LookupEnv}
type EnvFile struct {
	// Path selects an operator-controlled file; empty disables this input.
	// The path never enters formatted errors or provenance.
	Path string
	// Name is a trusted, credential-free display name for the env file.
	// Empty uses ".env". Provenance includes this name, line number and key.
	Name string
}

const maxEnvBytes = 1 << 20

type envFileValue struct {
	text string
	line int
}

type envDocument struct {
	name   string
	values map[string]envFileValue
}

func (d envDocument) lookup(environment Lookup, key string) (string, string, bool) {
	if environment != nil {
		if text, present := environment(key); present {
			return text, "", true
		}
	}
	value, present := d.values[key]
	if !present {
		return "", "", false
	}
	return value.text, fmt.Sprintf("env-file:%s:%d:%s", d.name, value.line, key), true
}

func readEnvFile(input EnvFile) (envDocument, error) {
	if input.Path == "" {
		return envDocument{}, nil
	}
	if input.Name == "" {
		input.Name = ".env"
	}
	fail := func(message string, cause error) (envDocument, error) {
		message = "environment file " + input.Name + ": " + message
		if cause != nil {
			return envDocument{}, safeerror.WrapFault(fault.Invalid, message, cause)
		}
		return envDocument{}, safeerror.NewFault(fault.Invalid, message)
	}
	if strings.TrimSpace(input.Name) == "" {
		return envDocument{}, safeerror.NewFault(fault.Invalid, "environment file source needs a name")
	}
	info, err := os.Stat(input.Path)
	if err != nil {
		return fail("cannot inspect input", err)
	}
	if !info.Mode().IsRegular() {
		return fail("input must be a regular file", nil)
	}
	file, err := os.Open(input.Path)
	if err != nil {
		return fail("cannot open input", err)
	}
	// Recheck the opened descriptor, as with the TOML file adapter.
	info, statErr := file.Stat()
	if statErr != nil || !info.Mode().IsRegular() {
		return fail("input must be a regular file", safeerror.Join("operation failed", statErr, file.Close()))
	}
	data, readErr := io.ReadAll(io.LimitReader(file, maxEnvBytes+1))
	if err := safeerror.Join("operation failed", readErr, file.Close()); err != nil {
		return fail("cannot read input", err)
	}
	if len(data) > maxEnvBytes {
		return fail("input exceeds 1 MiB", nil)
	}
	if !utf8.Valid(data) || bytes.IndexByte(data, 0) >= 0 {
		return fail("input must contain UTF-8 text without NUL bytes", nil)
	}
	values := make(map[string]envFileValue)
	for index, raw := range strings.Split(string(data), "\n") {
		line := strings.Trim(strings.TrimSuffix(raw, "\r"), " \t")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, found := strings.Cut(line, "=")
		key = strings.Trim(key, " \t")
		if !found || !validEnvKey(key) {
			return fail(fmt.Sprintf("line %d: expected KEY=VALUE", index+1), nil)
		}
		if _, exists := values[key]; exists {
			return fail(fmt.Sprintf("line %d, key %s: duplicate key", index+1, key), nil)
		}
		value, err := envValue(strings.TrimLeft(value, " \t"))
		if err != nil {
			return envDocument{}, safeerror.Prefix(fmt.Sprintf("environment file %s: line %d, key %s", input.Name, index+1, key), err)
		}
		values[key] = envFileValue{text: value, line: index + 1}
	}
	return envDocument{name: input.Name, values: values}, nil
}

func validEnvKey(key string) bool {
	if key == "" {
		return false
	}
	for i, c := range []byte(key) {
		if c != '_' && !(c >= 'A' && c <= 'Z') && !(c >= 'a' && c <= 'z') && !(i > 0 && c >= '0' && c <= '9') {
			return false
		}
	}
	return true
}

func envValue(raw string) (string, error) {
	if strings.ContainsRune(raw, '\r') {
		return "", safeerror.NewFault(fault.Invalid, "bare carriage return is unsupported")
	}
	if raw == "" {
		return "", nil
	}
	if raw[0] != '\'' && raw[0] != '"' {
		for i, c := range raw {
			if c == '#' && (i == 0 || raw[i-1] == ' ' || raw[i-1] == '\t') {
				return strings.TrimRight(raw[:i], " \t"), nil
			}
		}
		return strings.TrimRight(raw, " \t"), nil
	}
	quote := raw[0]
	var value strings.Builder
	for i := 1; i < len(raw); i++ {
		switch {
		case raw[i] == quote:
			tail := raw[i+1:]
			comment := strings.Trim(tail, " \t")
			if tail != "" && tail[0] != ' ' && tail[0] != '\t' {
				return "", safeerror.NewFault(fault.Invalid, "expected whitespace after quoted value")
			}
			if comment != "" && !strings.HasPrefix(comment, "#") {
				return "", safeerror.NewFault(fault.Invalid, "unexpected text after quoted value")
			}
			return value.String(), nil
		case quote == '"' && raw[i] == '\\':
			i++
			if i >= len(raw) || raw[i] != '\\' && raw[i] != '"' {
				return "", safeerror.NewFault(fault.Invalid, "unsupported escape in double-quoted value")
			}
			value.WriteByte(raw[i])
		default:
			value.WriteByte(raw[i])
		}
	}
	return "", safeerror.NewFault(fault.Invalid, "unterminated quoted value")
}
