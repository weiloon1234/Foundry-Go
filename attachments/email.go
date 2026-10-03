package attachments

import "github.com/weiloon1234/Foundry-Go/email"

// EmailAttachment snapshots this ready file into email's durable reference.
// No bytes are read. Authorize the file before use and retain its object for
// queued delivery. The mailer must have access to the same configured disk.
// Attachment promotes this method from its embedded File.
func (f File[M]) EmailAttachment() (email.Attachment, error) {
	if f.IsZero() {
		return email.Attachment{}, invalid()
	}
	reference := email.Attachment{Disk: f.disk, Key: f.key, Filename: f.info.OriginalName, ContentType: f.info.MediaType, Version: f.version}
	if reference.Version == "" {
		reference.IfMatch = f.etag
	}
	if reference.Validate() != nil || reference.Version == "" && reference.IfMatch == "" {
		return email.Attachment{}, invalid()
	}
	return reference, nil
}

// EmailAttachment accepts a loaded, present single-file slot directly through
// Message.AttachStored. An unloaded or absent slot is an error.
func (o One[M]) EmailAttachment() (email.Attachment, error) {
	if !o.loaded {
		return email.Attachment{}, notLoaded()
	}
	if o.file == nil {
		return email.Attachment{}, invalid()
	}
	return o.file.EmailAttachment()
}
