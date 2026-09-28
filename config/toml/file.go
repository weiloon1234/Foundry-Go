package toml

import (
	"errors"
	"os"

	"github.com/weiloon1234/Foundry-Go/config"
	"github.com/weiloon1234/Foundry-Go/fault"
)

// LoadFile reads and closes one bounded TOML file, then applies environment and
// typed overrides through the existing schema. Previously supplied file layers
// precede this file. An empty Options.Name uses the safe label "configuration";
// paths and file contents never enter normal error messages or provenance.
// Regular files are required. This helper does not load .env or alter os.Environ.
func LoadFile[T any](path string, schema *config.Schema[T], defaults T, inputs config.Inputs[T], options Options) (T, config.Report, error) {
	fail := func(cause error) (T, config.Report, error) {
		return *new(T), config.Report{}, fault.Wrap(fault.Invalid, "cannot load TOML configuration file", cause)
	}
	if schema == nil || path == "" {
		return fail(fault.New(fault.Invalid, "configuration file requires a path and schema"))
	}
	info, err := os.Stat(path)
	if err != nil {
		return fail(err)
	}
	if !info.Mode().IsRegular() {
		return fail(fault.New(fault.Invalid, "configuration input must be a regular file"))
	}
	file, err := os.Open(path)
	if err != nil {
		return fail(err)
	}
	// Recheck the opened descriptor; a replaced path must not masquerade as the
	// regular file inspected above. Configuration paths are operator controlled.
	info, err = file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return fail(errors.Join(err, file.Close(), fault.New(fault.Invalid, "configuration input must be a regular file")))
	}
	if options.Name == "" {
		options.Name = "configuration"
	}
	layer, decodeErr := Decode(file, schema, options)
	if err := errors.Join(decodeErr, file.Close()); err != nil {
		return fail(err)
	}
	inputs.Files = append(append([]config.Values(nil), inputs.Files...), layer)
	return schema.Load(defaults, inputs)
}
