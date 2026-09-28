package http

import (
	"context"
	"io"
	stdhttp "net/http"
	"strconv"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/fault"
)

func (r Response[R]) prepare(ctx context.Context, input R, limits EndpointLimits) (preparedResponse, error) {
	if r.kind == payloadDownload || r.kind == payloadStream {
		file, err := r.prepareFile(ctx, input, limits.Files)
		return preparedResponse{file: file}, err
	}
	if r.kind == payloadEmpty {
		return preparedResponse{}, nil
	}
	data, err := r.json.Encode(ctx, input, limits.Response)
	if err != nil {
		cause := err
		if encoded, ok := err.(*contract.EncodeError); ok {
			cause = encoded.Unwrap()
		}
		if canceled := ctx.Err(); canceled != nil && cause == canceled {
			return preparedResponse{}, RequestTimeout.WithCause(err)
		}
		return preparedResponse{}, InternalError.WithCause(err)
	}
	return preparedResponse{data: data}, nil
}

func (r Response[R]) write(w stdhttp.ResponseWriter, request *stdhttp.Request, prepared preparedResponse) error {
	if prepared.file != nil {
		return prepared.file.write(w, request)
	}
	data := prepared.data
	header := w.Header()
	clearResponseRepresentation(header)
	if r.kind == payloadJSON {
		header.Set("Content-Type", "application/json")
		header.Set("Content-Length", strconv.Itoa(len(data)))
		header.Set("X-Content-Type-Options", "nosniff")
	} else if r.status == stdhttp.StatusResetContent {
		header.Set("Content-Length", "0")
	}
	for _, item := range prepared.headers {
		header.Set(string(item.Name), string(item.Value))
	}
	w.WriteHeader(r.status)
	if request.Method == stdhttp.MethodHead || r.kind == payloadEmpty {
		return nil
	}
	written, err := w.Write(data)
	if err == nil && written != len(data) {
		err = io.ErrShortWrite
	}
	if err != nil {
		return fault.Wrap(fault.Internal, "typed HTTP response could not be written", err)
	}
	return nil
}
