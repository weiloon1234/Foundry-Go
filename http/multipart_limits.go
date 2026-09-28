package http

import (
	"math"

	"github.com/weiloon1234/Foundry-Go/fault"
)

// MultipartLimits bounds the complete encoded body, individual files, buffered
// non-file fields, part counts and simultaneously open uploaded-file readers.
// Bytes includes MIME framing and headers. FieldsBytes bounds retained text and
// JSON bytes together; JSON parts additionally use EndpointLimits.Body's depth,
// node, step and issue limits. The standard MIME parser also owns its header
// limits; HeaderBytes bounds accepted normalized part headers after parsing,
// not the native parser's allocation before that check.
type MultipartLimits struct {
	Bytes, FileBytes                             int64
	Parts, Files, Readers                        int
	HeaderBytes, FieldBytes, FieldsBytes, Issues int
}

func DefaultMultipartLimits() MultipartLimits {
	body := DefaultServerConfig().MaxBodyBytes
	return MultipartLimits{Bytes: body, FileBytes: body, Parts: 128, Files: 16, Readers: 16, HeaderBytes: DefaultServerConfig().MaxHeaderBytes, FieldBytes: 64 << 10, FieldsBytes: 256 << 10, Issues: 16}
}

func (l MultipartLimits) Validate() error {
	if l.Bytes <= 0 || l.FileBytes <= 0 || l.FileBytes > l.Bytes || l.Parts <= 0 || l.Files <= 0 || l.Files > l.Parts || l.Readers <= 0 || l.HeaderBytes <= 0 || l.FieldBytes <= 0 || l.FieldsBytes < l.FieldBytes || int64(l.FieldsBytes) > l.Bytes || int64(l.FieldBytes) == math.MaxInt64 || l.Issues <= 0 {
		return fault.New(fault.Invalid, "invalid multipart resource limits")
	}
	return nil
}
