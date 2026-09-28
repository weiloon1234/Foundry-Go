package workscope

import (
	"context"
	"io"

	"github.com/weiloon1234/Foundry-Go/fault"
)

// Reader guards a borrowed stream's progress, byte counts and cancellation. It
// never closes the source or starts a goroutine. Call it within Run when a
// service must retain ownership of abnormal or uncooperative reader callbacks.
func Reader(ctx context.Context, source io.Reader) io.Reader {
	return &reader{ctx: ctx, source: source}
}

type reader struct {
	ctx    context.Context
	source io.Reader
	empty  int
}

func (r *reader) Read(p []byte) (int, error) {
	if r.ctx == nil || r.source == nil {
		return 0, fault.New(fault.Invalid, "stream requires a context and reader")
	}
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := r.source.Read(p)
	if n < 0 || n > len(p) {
		return 0, fault.New(fault.Invalid, "reader returned an invalid byte count")
	}
	if n == 0 && err == nil && len(p) > 0 {
		r.empty++
		if r.empty >= 100 {
			return 0, io.ErrNoProgress
		}
	} else {
		r.empty = 0
	}
	if canceled := r.ctx.Err(); canceled != nil {
		return n, canceled
	}
	return n, err
}
