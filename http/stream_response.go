package http

import (
	"context"
	"io"

	"github.com/weiloon1234/Foundry-Go/fault"
)

// StreamResponse keeps the handler result typed as Stream. Declared media,
// byte limits and owned metadata share DownloadResponse's file contract.
// Ranges are ignored and Accept-Ranges is none; no validator is fabricated.
func StreamResponse(media MediaType, additional ...MediaType) Response[Stream] {
	descriptor := &fileResponse{media: append([]MediaType{media}, additional...), stream: true}
	return Response[Stream]{kind: payloadStream, status: 200, file: descriptor, prepareFile: descriptor.prepareStream}
}
func (f *fileResponse) prepareStream(ctx context.Context, stream Stream, limits FileResponseLimits) (*preparedFile, error) {
	if err := stream.Validate(); err != nil {
		return nil, InternalError.WithCause(err)
	}
	content, prepared, err := openFileSource(ctx, "HTTP stream source", stream.source, func(c StreamContent) io.ReadCloser { return c.Body })
	if err != nil {
		return prepared, err
	}
	prepared.stream = true
	prepared.flush = stream.progressive
	prepared.limit = limits.Bytes
	prepared.length = content.Length
	if length, ok := content.Length.Get(); ok && (length < 0 || length > limits.Bytes) {
		return prepared, InternalError.WithCause(fault.New(fault.Internal, "stream length exceeds file response limits"))
	}
	prepared.media, prepared.disposition, err = stream.filePresentation.prepare(f, content.Name, content.MediaType)
	if err != nil {
		return prepared, InternalError.WithCause(err)
	}
	return prepared, nil
}
