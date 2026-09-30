package extensions

import (
	"context"
	"errors"
	"go/token"

	"github.com/weiloon1234/Foundry-Go/model"
)

// SlotBinding connects one generated model struct field to an extension
// declaration. Generation supplies it from the model's slot fields; Field names
// the Go field, which identifies the slot in eager loading and diagnostics.
// Reference derives the owner identity from a model value without I/O. Get and
// Set read and replace the slot on a model copy.
type SlotBinding[M any, K comparable, S any] struct {
	Field     string
	Reference func(M) model.Reference[M, K]
	Get       func(M) S
	Set       func(M, S) M
}

// Validate checks the generated binding without calling its functions.
func (b SlotBinding[M, K, S]) Validate() error {
	if !token.IsIdentifier(b.Field) || !token.IsExported(b.Field) || b.Reference == nil || b.Get == nil || b.Set == nil {
		return invalid("invalid model extension slot binding")
	}
	return nil
}

// SlotDescription is an owned, secret-free inspection snapshot of one model
// extension slot: its Go field, kind, stored name and declared policy bounds.
// Kinds are "text", "one", "many" and "value". Unused bounds are omitted.
type SlotDescription struct {
	Field    string   `json:"field"`
	Kind     string   `json:"kind"`
	Name     string   `json:"name"`
	Storage  string   `json:"storage"`
	MaxBytes int64    `json:"max_bytes,omitempty"`
	Require  string   `json:"require,omitempty"`
	Disk     string   `json:"disk,omitempty"`
	MaxFiles int      `json:"max_files,omitempty"`
	Accepted []string `json:"accepted,omitempty"`
	Variants []string `json:"variants,omitempty"`
	Version  uint32   `json:"version,omitempty"`
}

// OwnerDescription is an inspection snapshot of a registered owner identity.
type OwnerDescription struct {
	Name         OwnerName `json:"name"`
	Model        string    `json:"model"`
	StorageModel string    `json:"storage_model"`
}

// LoadInParts runs load over parents in parts of at most size, keeping their
// order. A part whose load reports limit, a store's row or byte bound, is
// halved and retried, so a batch bounded by retained bytes loads whenever each
// owner fits alone. Reads are side-effect free, so a retry repeats no effect.
func LoadInParts[M, S any](ctx context.Context, parents []M, size int, limit error, load func(context.Context, []M) ([]S, error)) ([]S, error) {
	size = max(size, 1)
	result := make([]S, 0, len(parents))
	for start := 0; start < len(parents); start += size {
		values, err := loadPart(ctx, parents[start:min(start+size, len(parents))], limit, load)
		if err != nil {
			return nil, err
		}
		result = append(result, values...)
	}
	return result, nil
}

func loadPart[M, S any](ctx context.Context, part []M, limit error, load func(context.Context, []M) ([]S, error)) ([]S, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	values, err := load(ctx, part)
	if err != nil {
		if len(part) < 2 || limit == nil || !errors.Is(err, limit) {
			return nil, err
		}
		half := len(part) / 2
		left, err := loadPart(ctx, part[:half], limit, load)
		if err != nil {
			return nil, err
		}
		right, err := loadPart(ctx, part[half:], limit, load)
		if err != nil {
			return nil, err
		}
		return append(left, right...), nil
	}
	if len(values) != len(part) {
		return nil, invalid("model extension slot load returned a different number of values")
	}
	return values, nil
}
