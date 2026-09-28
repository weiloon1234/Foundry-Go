package value

import (
	"context"
	"reflect"
	"unicode/utf8"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/jsonshape"
)

// JSONKeyLimits bounds names supplied for one JSON object. Bytes counts decoded
// name bytes; Keys bounds entries. Both limits are positive. Formatting probes
// additionally allow only the bounded escaping overhead of a canonical name.
type JSONKeyLimits struct{ Bytes, Keys int }

func (l JSONKeyLimits) Validate() error {
	if l.Bytes <= 0 || l.Keys <= 0 {
		return fault.New(fault.Invalid, "invalid JSON key limits")
	}
	return nil
}

// SupportsJSONKeys checks whether K supports the framework's native map codecs.
// It invokes no value methods. A comparable type alone does not imply JSON
// object-key support; Go's JSON/text method rules still apply.
func SupportsJSONKeys[K comparable]() bool {
	supported, _ := jsonshape.MapKeyCapabilities(reflect.TypeFor[K]())
	return supported
}

// CheckJSONKeys validates object names before decoding their containing value.
// It preserves K, rejects noncanonical aliases and duplicate decoded identities,
// and shares native codec rules with typed storage snapshots. Callers keep the
// names unchanged until return. Codecs must be deterministic, bounded and must
// not retain borrowed input. Cancellation waits for active codec work to finish.
func CheckJSONKeys[K comparable](ctx context.Context, keys []string, limits JSONKeyLimits) error {
	if ctx == nil {
		return fault.New(fault.Invalid, "JSON keys require a context")
	}
	if err := limits.Validate(); err != nil {
		return err
	}
	if !SupportsJSONKeys[K]() || len(keys) > limits.Keys {
		return fault.New(fault.Invalid, "invalid JSON object keys")
	}
	remaining := limits.Bytes
	for _, key := range keys {
		if !utf8.ValidString(key) || len(key) > remaining {
			return fault.New(fault.Invalid, "invalid JSON object keys")
		}
		remaining -= len(key)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	err := callback.Isolated("validate JSON object keys", func() error {
		seen := make(map[any]struct{}, len(keys))
		for _, key := range keys {
			if err := ctx.Err(); err != nil {
				return err
			}
			// JSON quoting expands at most six bytes per input byte, plus the
			// one-entry map envelope. Detect overflow before constructing it.
			if len(key) > (int(^uint(0)>>1)-16)/6 {
				return invalidJSON()
			}
			decoded, err := parseJSONMapKey(ctx, key, reflect.TypeFor[K](), 6*len(key)+16)
			if err != nil {
				return err
			}
			identity := decoded.Interface()
			if _, duplicate := seen[identity]; duplicate {
				return invalidJSON()
			}
			seen[identity] = struct{}{}
		}
		return nil
	})
	// Do not call arbitrary codec error methods to classify failures.
	if failure, ok := err.(*fault.Error); ok && (failure.Code() == fault.Panicked || failure.Code() == fault.Internal) {
		return fault.Wrap(fault.Internal, "JSON key codec failed", err)
	}
	if canceled := ctx.Err(); canceled != nil {
		return canceled
	}
	if err != nil {
		return fault.Wrap(fault.Invalid, "invalid JSON object keys", err)
	}
	return nil
}
