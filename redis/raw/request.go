package raw

import (
	"strings"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/keyspace"
)

// Request is an immutable adapter invocation. Only a command containing a typed
// key, or a script constructor requiring a key, can produce one.
type Request struct {
	name, source string
	args         []Argument
	invalid      bool
}

func (r Request) append(arg Argument) Request {
	if r.invalid {
		return r
	}
	if arg.validate() != nil || len(r.args) >= MaxArguments || len(arg.text) > MaxRequestBytes-r.Bytes() {
		r.invalid = true
		r.args = nil
		return r
	}
	args := make([]Argument, len(r.args)+1)
	copy(args, r.args)
	args[len(r.args)] = arg
	r.args = args
	return r
}
func (r Request) Bytes() int {
	n := len(r.name) + len(r.source)
	for _, a := range r.args {
		n += len(a.text)
	}
	return n
}
func (r Request) Validate(ns keyspace.Namespace, l Limits) error {
	if err := ns.Validate(); err != nil {
		return err
	}
	if err := l.Validate(); err != nil {
		return err
	}
	if r.invalid || r.name == "" || len(r.args) > l.Arguments || r.Bytes() > l.RequestBytes {
		return fault.New(fault.Invalid, "invalid or oversized raw Redis request")
	}
	keys := 0
	for _, a := range r.args {
		if err := a.validate(); err != nil {
			return err
		}
		if a.key.text != "" {
			if a.key.Namespace() != ns {
				return fault.New(fault.Invalid, "raw Redis keys must belong to the configured namespace")
			}
			keys++
		}
	}
	if keys == 0 {
		return fault.New(fault.Invalid, "raw Redis requests require a scoped key")
	}
	return nil
}
func (r Request) IsScript() bool { return r.source != "" }
func (r Request) Source() string { return r.source }

// Arguments returns owned command arguments (including its name) or script ARGV.
func (r Request) Arguments() []string {
	result := make([]string, 0, len(r.args)+1)
	if !r.IsScript() {
		result = append(result, r.name)
	}
	for _, a := range r.args {
		if !r.IsScript() || a.key.text == "" {
			result = append(result, a.text)
		}
	}
	return result
}
func (r Request) Keys() []Key {
	var keys []Key
	for _, a := range r.args {
		if a.key.text != "" {
			keys = append(keys, a.key)
		}
	}
	return keys
}
func validCommand(name string) bool {
	if len(name) == 0 || len(name) > 64 {
		return false
	}
	for _, c := range name {
		if !(c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '.' || c == '_' || c == '-') {
			return false
		}
	}
	// These commands alter pooled connection state or bypass the explicit script/
	// transaction constructors. Dedicated subscription and lifecycle APIs own them.
	switch name {
	case "AUTH", "SELECT", "HELLO", "CLIENT", "QUIT", "RESET", "MONITOR", "SUBSCRIBE", "PSUBSCRIBE", "SSUBSCRIBE", "UNSUBSCRIBE", "PUNSUBSCRIBE", "SUNSUBSCRIBE", "WATCH", "UNWATCH", "MULTI", "EXEC", "DISCARD", "ASKING", "READONLY", "READWRITE", "SYNC", "PSYNC", "REPLCONF", "EVAL", "EVALSHA", "EVAL_RO", "EVALSHA_RO":
		return false
	}
	return true
}

// Builder cannot execute until Key produces a Command. Prefix arguments support
// commands such as XGROUP CREATE whose key is not the first argument.
type Builder[R any] struct {
	request Request
	decoder Decoder[R]
}

func NewCommand[R any](name string, decoder Decoder[R]) Builder[R] {
	if len(name) > 64 {
		return Builder[R]{request: Request{invalid: true}, decoder: decoder}
	}
	name = strings.Clone(strings.ToUpper(name))
	return Builder[R]{Request{name: name, invalid: !validCommand(name)}, decoder}
}
func (b Builder[R]) Arg(arg Argument) Builder[R] { b.request = b.request.append(arg); return b }
func (b Builder[R]) Key(key Key) Command[R] {
	return Command[R]{b.request.append(keyArgument(key)), b.decoder}
}

// Command owns immutable arguments and a concrete result decoder. Lua source and
// raw arguments are trusted; declared key checks are not a sandbox for that code.
type Command[R any] struct {
	request Request
	decoder Decoder[R]
}

func (c Command[R]) Arg(arg Argument) Command[R] { c.request = c.request.append(arg); return c }
func (c Command[R]) Key(key Key) Command[R]      { c.request = c.request.append(keyArgument(key)); return c }

// NewScript uses a single EVAL with typed KEYS and explicit ARGV, without script
// cache fallback. It can also be queued in a Pipeline like any other command.
func NewScript[R any](source string, key Key, decoder Decoder[R]) Command[R] {
	r := Request{name: "EVAL", invalid: len(source) == 0 || len(source) > MaxScriptBytes}
	if !r.invalid {
		r.source = strings.Clone(source)
	}
	return Command[R]{r.append(keyArgument(key)), decoder}
}
