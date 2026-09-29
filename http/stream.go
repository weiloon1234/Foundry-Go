package http

import (
	"context"
	"io"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
)

// StreamContent is one finite, unseekable representation. Length is optional:
// unset means unknown, Set(0) means empty. A supplied length must match EOF.
// Ownership of Body transfers when StreamSource returns, even alongside an
// error. Reads must terminate or honor the source context. Foundry never
// abandons a callback or closes its body concurrently with a read.
type StreamContent struct {
	Body      io.ReadCloser
	Name      string
	MediaType MediaType
	Length    value.Optional[int64]
}

// StreamSource opens a representation only after its endpoint handler succeeds.
// HEAD opens metadata and closes Body without reading. Before returning, the
// source owns every resource it creates, including its own producer goroutines.
type StreamSource func(context.Context) (StreamContent, error)

// Stream is a deferred finite response that requires no Seek implementation.
// It has no range or cache-validator contract; use Download for those features.
// Copies share the source callback but open independently for each request.
type Stream struct {
	filePresentation
	source      StreamSource
	progressive bool
}

// Progressive flushes each chunk to the client as soon as the source produced
// and Foundry accepted it, for incremental output such as a long export or a
// log tail. Without it the native writer may buffer small chunks. Length and
// byte-limit checks are unchanged: a flushed prefix can still be aborted by a
// later failure, never completed early. Declare the route's WithTimeout for
// streams that outlive the kernel RequestTimeout.
func (s Stream) Progressive() Stream { s.progressive = true; return s }

func StreamFrom(source StreamSource) Stream {
	return Stream{source: source, filePresentation: defaultFilePresentation()}
}
func (s Stream) WithName(name string) Stream { s.name = value.Set(name); return s }
func (s Stream) WithDisposition(disposition Disposition) Stream {
	s.disposition = disposition
	return s
}
func (s Stream) WithMediaType(media MediaType) Stream { s.media = value.Set(media); return s }

// Validate checks metadata without opening or consuming the stream.
func (s Stream) Validate() error {
	if s.source == nil {
		return fault.New(fault.Invalid, "stream requires a source")
	}
	return s.filePresentation.validate()
}
func (Stream) MarshalJSON() ([]byte, error) {
	return nil, fault.New(fault.Invalid, "stream requires a file response contract")
}
