package http

import (
	"context"
	"io"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
)

// The body belongs to the source until return. Once transferred, retain it even
// alongside an error so endpoint cleanup can close it before releasing inputs.
func openFileSource[T any](ctx context.Context, operation string, source func(context.Context) (T, error), body func(T) io.ReadCloser) (T, *preparedFile, error) {
	var content T
	// Sources open after the handler succeeded: an expired deadline here is
	// the server's own budget, not a slow client.
	if err := ctx.Err(); err != nil {
		return content, nil, Unavailable.WithCause(err)
	}
	var returned error
	owned := callback.Isolated(operation, func() error { content, returned = source(ctx); return nil })
	prepared := &preparedFile{}
	if transferred := body(content); transferred != nil {
		reader, err := newFileReader(ctx, transferred)
		if err != nil {
			return content, nil, InternalError.WithCause(err)
		}
		prepared.reader = reader
	}
	if owned != nil {
		return content, prepared, InternalError.WithCause(owned)
	}
	if returned != nil {
		return content, prepared, returned
	}
	if err := ctx.Err(); err != nil {
		return content, prepared, Unavailable.WithCause(err)
	}
	if prepared.reader == nil {
		return content, prepared, InternalError.WithCause(fault.New(fault.Internal, "file source returned no body"))
	}
	return content, prepared, nil
}
