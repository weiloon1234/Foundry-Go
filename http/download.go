package http

import (
	"context"
	"io"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
)

// DownloadContent describes one opened seekable representation. Ownership of
// Body transfers to Foundry when the source returns, including alongside an
// error. Before returning, the source owns cleanup of any resources it opens.
// Source reads must honor the context supplied to DownloadSource.
type DownloadContent struct {
	Body      io.ReadSeekCloser
	Name      string
	MediaType MediaType
	Modified  time.Time
	EntityTag EntityTag
}

// DownloadSource opens content after a successful endpoint handler. It is not
// evaluated during route construction or when input/authorization fails.
type DownloadSource func(context.Context) (DownloadContent, error)

// Download is a deferred seekable file representation, not a JSON response DTO.
// Copies retain the source callback but open separate request-owned content.
type Download struct {
	err    error
	source DownloadSource
	filePresentation
	tag value.Optional[EntityTag]
}

func DownloadFrom(source DownloadSource) Download {
	return Download{source: source, filePresentation: defaultFilePresentation()}
}

// WithName overrides display metadata only; it never changes the source path.
func (d Download) WithName(name string) Download { d.name = value.Set(name); return d }
func (d Download) WithDisposition(disposition Disposition) Download {
	d.disposition = disposition
	return d
}
func (d Download) WithMediaType(media MediaType) Download { d.media = value.Set(media); return d }
func (d Download) WithEntityTag(tag EntityTag) Download   { d.tag = value.Set(tag); return d }

// Validate checks the declaration without opening or reading its source.
func (d Download) Validate() error {
	if d.err != nil {
		return d.err
	}
	if d.source == nil {
		return fault.New(fault.Invalid, "download requires a source")
	}
	if err := d.filePresentation.validate(); err != nil {
		return err
	}
	if tag, supplied := d.tag.Get(); supplied {
		if err := tag.Validate(); err != nil {
			return err
		}
	}
	return nil
}

// MarshalJSON prevents accidental serialization of a deferred file response.
func (Download) MarshalJSON() ([]byte, error) {
	return nil, fault.New(fault.Invalid, "download requires a file response contract")
}
