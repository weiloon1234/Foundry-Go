package http

import (
	"fmt"
	"mime/multipart"
	"net/textproto"
	"strings"
)

// TransferBytes bounds a successful body, including multipart byte-range
// framing. Bytes alone bounds the representation; MIME framing can be larger
// than a small file. net/http falls back to the full representation when the
// sum of requested ranges exceeds its size. The standard MIME writer measures
// framing with its maximum supported boundary, so exporters do not recreate it.
func (info FileResponseInfo) TransferBytes(limits FileResponseLimits) (int64, error) {
	if err := limits.Validate(); err != nil {
		return 0, err
	}
	if err := (&fileResponse{media: info.MediaTypes, stream: !info.Seekable}).Validate(); err != nil {
		return 0, err
	}
	if !info.Seekable {
		return limits.Bytes, nil
	}
	var overhead int64
	for _, media := range info.MediaTypes {
		count := new(metadataByteCounter)
		writer := multipart.NewWriter(count)
		// RFC 2046 and mime/multipart.Writer.SetBoundary permit at most 70 bytes.
		if err := writer.SetBoundary(strings.Repeat("x", 70)); err != nil {
			return 0, err
		}
		header := textproto.MIMEHeader{"Content-Type": {string(media)}, "Content-Range": {fmt.Sprintf("bytes %d-%d/%d", limits.Bytes, limits.Bytes, limits.Bytes)}}
		for range limits.Ranges {
			if _, err := writer.CreatePart(header); err != nil {
				return 0, err
			}
		}
		if err := writer.Close(); err != nil {
			return 0, err
		}
		overhead = max(overhead, count.bytes)
	}
	return limits.Bytes + overhead, nil
}

type metadataByteCounter struct{ bytes int64 }

func (counter *metadataByteCounter) Write(data []byte) (int, error) {
	counter.bytes += int64(len(data))
	return len(data), nil
}
