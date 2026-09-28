package websocket

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/fault"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Target distinguishes a whole-channel subscription from a concrete room.
// The same value type is retained through authorization and incoming handlers.
type Target[R any] struct{ Room value.Optional[R] }

func Room[R any](room R) Target[R]   { return Target[R]{Room: value.Set(room)} }
func WholeChannel[R any]() Target[R] { return Target[R]{} }

type Rooms[R any] struct{ codec foundryhttp.PathCodec[R] }

// DefineRooms reuses the existing text codec and its same-value metadata.
func DefineRooms[R any](codec foundryhttp.PathCodec[R]) Rooms[R] { return Rooms[R]{codec: codec} }
func (r Rooms[R]) Validate() error                               { _, err := foundryhttp.DescribePathCodec(r.codec); return err }
func (r Rooms[R]) Description() (foundryhttp.URLScalarInfo, error) {
	return foundryhttp.DescribePathCodec(r.codec)
}
func (r Rooms[R]) decode(ctx context.Context, text *string) (Target[R], error) {
	if text == nil {
		return WholeChannel[R](), nil
	}
	var result R
	err := callback.Isolated("WebSocket room codec", func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !validRoom(*text) {
			return Malformed
		}
		var err error
		result, err = r.codec.Parse(*text)
		if err != nil {
			return err
		}
		canonical, err := r.codec.Format(result)
		if err != nil {
			return err
		}
		if canonical != *text {
			return fault.New(fault.Invalid, "room key is not canonical")
		}
		return ctx.Err()
	})
	if err != nil {
		return Target[R]{}, err
	}
	return Room(result), nil
}
func (r Rooms[R]) encode(ctx context.Context, room R) (string, error) {
	if err := r.Validate(); err != nil {
		return "", err
	}
	var text string
	err := callback.Isolated("WebSocket room codec", func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		var err error
		text, err = r.codec.Format(room)
		if err != nil {
			return err
		}
		if !validRoom(text) {
			return fault.New(fault.Invalid, "invalid room key")
		}
		_, err = r.decode(ctx, &text)
		return err
	})
	return text, err
}
