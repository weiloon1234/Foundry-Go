package http

import (
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Shared display metadata never changes a source path or source ownership.
type filePresentation struct {
	disposition Disposition
	name        value.Optional[string]
	media       value.Optional[MediaType]
}

func defaultFilePresentation() filePresentation {
	return filePresentation{disposition: DispositionAttachment}
}
func (p filePresentation) validate() error {
	if err := p.disposition.Validate(); err != nil {
		return err
	}
	if media, ok := p.media.Get(); ok {
		return media.Validate()
	}
	return nil
}
func (p filePresentation) prepare(f *fileResponse, name string, media MediaType) (MediaType, HeaderValue, error) {
	if override, ok := p.name.Get(); ok {
		name = override
	}
	if override, ok := p.media.Get(); ok {
		media = override
	}
	if err := media.Validate(); err != nil {
		return "", "", err
	}
	if !f.accepts(media) {
		return "", "", fault.New(fault.Internal, "file response media type is not declared")
	}
	disposition, err := p.disposition.HeaderValue(name)
	return MediaType(normalizedFileMedia(media)), disposition, err
}
