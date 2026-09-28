package raw

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/value"
)

// Decoder preserves the command's concrete result type. Custom callbacks must be
// deterministic, concurrency-safe and return owned values. Execution owns their
// lifetime and catches panic/Goexit without abandoning uncooperative callbacks.
type Decoder[R any] struct {
	decode func(context.Context, Reply) (R, error)
}

func DecodeWith[R any](fn func(context.Context, Reply) (R, error)) Decoder[R] { return Decoder[R]{fn} }
func DecodeInt64() Decoder[int64] {
	return DecodeWith(func(ctx context.Context, r Reply) (int64, error) { return r.Integer() })
}
func DecodeUint64() Decoder[uint64] {
	return DecodeWith(func(ctx context.Context, r Reply) (uint64, error) {
		v, err := r.Integer()
		if err != nil {
			return 0, err
		}
		if v < 0 {
			return 0, replyError()
		}
		return uint64(v), nil
	})
}
func DecodeBool() Decoder[bool] {
	return DecodeWith(func(ctx context.Context, r Reply) (bool, error) {
		v, err := r.Integer()
		if err != nil {
			return false, err
		}
		if v != 0 && v != 1 {
			return false, replyError()
		}
		return v == 1, nil
	})
}
func DecodeString() Decoder[string] {
	return DecodeWith(func(ctx context.Context, r Reply) (string, error) { return r.Text() })
}
func DecodeBytes() Decoder[[]byte] {
	return DecodeWith(func(ctx context.Context, r Reply) ([]byte, error) {
		v, err := r.Text()
		if err != nil {
			return nil, err
		}
		return []byte(v), nil
	})
}
func DecodeArray[R any](element Decoder[R]) Decoder[[]R] {
	return DecodeWith(func(ctx context.Context, r Reply) ([]R, error) {
		if element.decode == nil {
			return nil, replyError()
		}
		items, err := r.Array()
		if err != nil {
			return nil, err
		}
		result := make([]R, len(items))
		for i, x := range items {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			v, err := element.decode(ctx, x)
			if err != nil {
				return nil, err
			}
			result[i] = v
		}
		return result, nil
	})
}
func DecodeNullable[R any](element Decoder[R]) Decoder[value.Nullable[R]] {
	return DecodeWith(func(ctx context.Context, r Reply) (value.Nullable[R], error) {
		if element.decode == nil {
			return value.Null[R](), replyError()
		}
		if r.IsNull() {
			return value.Null[R](), nil
		}
		v, err := element.decode(ctx, r)
		if err != nil {
			return value.Null[R](), err
		}
		return value.Of(v), nil
	})
}
