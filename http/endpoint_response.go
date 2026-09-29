package http

import (
	"context"
	"io"
	stdhttp "net/http"
	"strconv"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/fault"
)

// prepare runs only after the handler succeeded. JSON encoding is bounded CPU
// work, so it is detached from cancellation: a deadline cannot turn a completed
// success into a timeout. Credential responses are the exception and are never
// disclosed after the request context ended. A file source opens and transfers under the live
// request context; after an expired server deadline it is detached like JSON,
// but a client that already disconnected never causes a source to open.
func (r Response[R]) prepare(ctx context.Context, input R, limits EndpointLimits) (preparedResponse, error) {
	if r.kind == payloadDownload || r.kind == payloadStream {
		// After an expired deadline the source opens and transfers without it,
		// still ending on client disconnect or forced shutdown.
		source, release := completedContext(ctx)
		file, err := r.prepareFile(source, input, limits.Files)
		return preparedResponse{file: file, release: release}, err
	}
	if r.kind == payloadEmpty {
		return preparedResponse{}, nil
	}
	if r.kind == payloadEvents {
		events, err := r.events.prepare(input, limits.Response)
		if err != nil {
			return preparedResponse{}, InternalError.WithCause(fault.Wrap(fault.Internal, "event stream is invalid", err))
		}
		return preparedResponse{events: events}, nil
	}
	if r.kind == payloadRedirect {
		location, err := r.redirectTarget(input)
		if err != nil {
			return preparedResponse{}, InternalError.WithCause(fault.Wrap(fault.Internal, "redirect target is invalid", err))
		}
		return preparedResponse{location: location}, nil
	}
	status, err := r.selectedStatus(input)
	if err != nil {
		return preparedResponse{}, err
	}
	encoding := context.WithoutCancel(ctx)
	if r.credentials {
		// Credential responses deliver one-time secrets (tokens, MFA material).
		// They stay withheld once the request context ended, even after the
		// issuing handler completed; the client obtains a fresh credential.
		encoding = ctx
	}
	data, err := r.json.Encode(encoding, input, limits.Response)
	if err != nil {
		cause := err
		if encoded, ok := err.(*contract.EncodeError); ok {
			cause = encoded.Unwrap()
		}
		if canceled := ctx.Err(); canceled != nil && cause == canceled {
			return preparedResponse{}, Unavailable.WithCause(err)
		}
		return preparedResponse{}, InternalError.WithCause(fault.Wrap(fault.Internal, "typed JSON response failed its contract or EndpointLimits.Response bounds", err))
	}
	return preparedResponse{data: data, status: status}, nil
}

func (r Response[R]) write(w stdhttp.ResponseWriter, request *stdhttp.Request, prepared preparedResponse) error {
	if prepared.file != nil {
		return prepared.file.write(w, request)
	}
	if prepared.events != nil {
		return prepared.events(w, request)
	}
	data := prepared.data
	header := w.Header()
	clearResponseRepresentation(header)
	if r.kind == payloadJSON {
		header.Set("Content-Type", "application/json")
		header.Set("Content-Length", strconv.Itoa(len(data)))
		header.Set("X-Content-Type-Options", "nosniff")
	} else if r.status == stdhttp.StatusResetContent || r.kind == payloadRedirect {
		header.Set("Content-Length", "0")
	}
	applyResponseHeaders(header, prepared.headers)
	if r.kind == payloadRedirect {
		header.Set("Location", prepared.location)
	}
	w.WriteHeader(prepared.statusOr(r.status))
	if request.Method == stdhttp.MethodHead || r.kind == payloadEmpty || r.kind == payloadRedirect {
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
