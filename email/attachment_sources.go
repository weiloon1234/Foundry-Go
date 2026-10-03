package email

import (
	"context"
	"io"

	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/filename"
	"github.com/weiloon1234/Foundry-Go/internal/mediatype"
	"github.com/weiloon1234/Foundry-Go/internal/upload"
	"github.com/weiloon1234/Foundry-Go/internal/workscope"
	"github.com/weiloon1234/Foundry-Go/storage"
)

// UploadSource accepts a captured browser upload (http.UploadedFile) without
// email depending on HTTP. The caller owns the underlying file.
type UploadSource = upload.Source

// StoredAttachment supplies one pinned reference without I/O. Foundry's
// attachments.File, Attachment and loaded One slots implement this contract.
// The caller must authorize access to the file before attaching it.
type StoredAttachment interface {
	EmailAttachment() (Attachment, error)
}

// AttachUpload reads and closes one upload now, without persisting it. The
// returned message owns the bytes and survives request cleanup. Detected bytes
// determine its media type; the client type is only a compatible text hint.
// MaxDataAttachmentBytes bounds the read; mailer limits also apply at Send.
// Queueing/snapshotting requires stored references instead of runtime uploads.
func (m Message) AttachUpload(ctx context.Context, file UploadSource) (Message, error) {
	if ctx == nil || file == nil || len(m.attachments)+len(m.data) >= MaxAttachments {
		return Message{}, Construction
	}
	if ctx.Err() != nil {
		return Message{}, Transient
	}
	var attachment DataAttachment
	err := callback.Isolated("prepare email upload", func() (err error) {
		name := filename.StripInvisible(file.Name())
		if (Attachment{Filename: name, ContentType: "application/octet-stream"}).validateMetadata() != nil {
			return Construction
		}
		reader, err := file.Open(ctx)
		if reader != nil {
			defer func() {
				if closeErr := reader.Close(); closeErr != nil && err == nil {
					err = Construction
				}
			}()
		}
		if err != nil || reader == nil {
			return Construction
		}
		data, err := io.ReadAll(io.LimitReader(workscope.Reader(ctx, reader), MaxDataAttachmentBytes+1))
		if err != nil || len(data) == 0 || len(data) > MaxDataAttachmentBytes {
			return Construction
		}
		attachment = DataAttachment{metadata: Attachment{Filename: name, ContentType: storage.MediaType(mediatype.Detect(data, file.ClientContentType()))}, data: data}
		return attachment.metadata.validateMetadata()
	})
	if ctx.Err() != nil {
		return Message{}, Transient
	}
	if err != nil {
		return Message{}, Construction
	}
	return m.AttachData(attachment), nil
}

// AttachStored snapshots a stored file or a loaded single-file model slot into
// this message. It performs no reads; Send resolves bytes through the mailer's
// storage registry, using the captured version or ETag. Missing/unloaded files
// fail rather than silently omitting the attachment. Retain the stored object
// for the lifetime of queued mail; a reference does not prevent deletion.
func (m Message) AttachStored(file StoredAttachment) (Message, error) {
	if file == nil || len(m.attachments)+len(m.data) >= MaxAttachments {
		return Message{}, Construction
	}
	var attachment Attachment
	if err := callback.Isolated("prepare stored email attachment", func() error {
		var err error
		attachment, err = file.EmailAttachment()
		return err
	}); err != nil || attachment.Validate() != nil || attachment.Version == "" && attachment.IfMatch == "" {
		return Message{}, Construction
	}
	return m.Attach(attachment), nil
}
