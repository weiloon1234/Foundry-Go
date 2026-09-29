package http

import (
	"context"
	"io"
	"mime"
	"slices"
	"strings"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
)

// FileResponseInfo describes declared media and native seek/range capability.
// File content has no implicit JSON schema. MediaTypes is an owned snapshot.
type FileResponseInfo struct {
	MediaTypes []MediaType `json:"media_types"`
	Seekable   bool        `json:"seekable"`
}

type fileResponse struct {
	media  []MediaType
	stream bool
}

// DownloadResponse retains Download as the handler's concrete result. At least
// one concrete media type is declared; every opened representation must match.
// The normal status is 200; native conditions and ranges can produce 206 or 304.
func DownloadResponse(media MediaType, additional ...MediaType) Response[Download] {
	descriptor := &fileResponse{media: append([]MediaType{media}, additional...)}
	return Response[Download]{kind: payloadDownload, status: 200, file: descriptor, prepareFile: descriptor.prepareDownload}
}

func (f *fileResponse) Validate() error {
	if f == nil || len(f.media) == 0 || len(f.media) > 16 {
		return fault.New(fault.Invalid, "file response requires bounded declared media types")
	}
	seen := make(map[string]struct{}, len(f.media))
	for _, media := range f.media {
		if err := media.Validate(); err != nil {
			return err
		}
		normalized := normalizedFileMedia(media)
		if _, duplicate := seen[normalized]; duplicate {
			return fault.New(fault.Invalid, "duplicate file response media type")
		}
		seen[normalized] = struct{}{}
	}
	return nil
}

func normalizedFileMedia(media MediaType) string {
	name, parameters, err := mime.ParseMediaType(string(media))
	if err != nil {
		return ""
	}
	if charset, ok := parameters["charset"]; ok {
		parameters["charset"] = strings.ToLower(charset)
	}
	return mime.FormatMediaType(name, parameters)
}
func (f *fileResponse) description() FileResponseInfo {
	media := slices.Clone(f.media)
	for i, v := range media {
		media[i] = MediaType(normalizedFileMedia(v))
	}
	return FileResponseInfo{MediaTypes: media, Seekable: !f.stream}
}
func (f *fileResponse) accepts(media MediaType) bool {
	normalized := normalizedFileMedia(media)
	for _, declared := range f.media {
		if normalized == normalizedFileMedia(declared) {
			return true
		}
	}
	return false
}

type preparedFile struct {
	reader       *fileReader
	media        MediaType
	disposition  HeaderValue
	modified     time.Time
	tag          EntityTag
	stream       bool
	flush        bool
	length       value.Optional[int64]
	limit        int64
	cacheControl HeaderValue
	varyAccept   bool
}

func (f *fileResponse) prepareDownload(ctx context.Context, download Download, limits FileResponseLimits) (*preparedFile, error) {
	if err := download.Validate(); err != nil {
		return nil, InternalError.WithCause(err)
	}
	content, prepared, err := openFileSource(ctx, "HTTP download source", download.source, func(c DownloadContent) io.ReadCloser { return c.Body })
	if err != nil {
		return prepared, err
	}
	prepared.reader.maxBytes = limits.Bytes
	if tag, ok := download.tag.Get(); ok {
		content.EntityTag = tag
	}
	if err := content.EntityTag.Validate(); err != nil {
		return prepared, InternalError.WithCause(err)
	}
	prepared.media, prepared.disposition, err = download.filePresentation.prepare(f, content.Name, content.MediaType)
	if err != nil {
		return prepared, InternalError.WithCause(err)
	}
	_, err = prepared.reader.Seek(0, io.SeekEnd)
	if err != nil {
		return prepared, fileReadError(ctx, prepared.reader.Failure())
	}
	if _, err := prepared.reader.Seek(0, io.SeekStart); err != nil {
		return prepared, fileReadError(ctx, prepared.reader.Failure())
	}
	prepared.modified = content.Modified
	prepared.tag = content.EntityTag
	return prepared, nil
}

// File sources are read after the handler succeeded: an expired deadline is
// the server's own budget (503), never a slow-client 408.
func fileReadError(ctx context.Context, cause error) error {
	if cause != nil && (cause == ctx.Err() || cause == context.DeadlineExceeded || cause == context.Canceled) {
		return Unavailable.WithCause(cause)
	}
	return InternalError.WithCause(cause)
}
