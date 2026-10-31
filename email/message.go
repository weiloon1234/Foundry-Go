package email

import (
	"fmt"
	"github.com/weiloon1234/Foundry-Go/internal/emaildiag"
	"log/slog"
	"maps"
	"net/textproto"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/weiloon1234/Foundry-Go/i18n"
	"github.com/weiloon1234/Foundry-Go/storage"
)

const MaxRecipients = 50
const MaxAttachments = 32
const MaxHeaders = 32

// Attachment references shared storage. Pin Version or IfMatch when queued so
// redelivery cannot silently send replacement content. Readers and OS paths
// never enter serialized email data.
type Attachment struct {
	Disk        storage.DiskID    `json:"disk"`
	Key         storage.ObjectKey `json:"key"`
	Filename    string            `json:"filename"`
	ContentType storage.MediaType `json:"content_type"`
	ContentID   string            `json:"content_id,omitempty"`
	Version     storage.VersionID `json:"version,omitempty"`
	IfMatch     storage.ETag      `json:"if_match,omitempty"`
}

// Validate checks storage identifiers, conditions, and attachment metadata.
func (a Attachment) Validate() error {
	for _, err := range []error{a.Disk.Validate(), a.Key.Validate(), a.Version.Validate(), a.IfMatch.Validate()} {
		if err != nil {
			return emaildiag.Failure(Construction)
		}
	}
	return a.validateMetadata()
}

// validateMetadata checks the fields shared with in-memory attachments.
func (a Attachment) validateMetadata() error {
	if a.ContentType.Validate() != nil {
		return emaildiag.Failure(Construction)
	}
	if a.Filename == "" || len(a.Filename) > 200 || !headerText(a.Filename) || strings.ContainsAny(a.Filename, "/\\") || a.Filename == "." || a.Filename == ".." {
		return emaildiag.Failure(Construction)
	}
	if len(a.ContentID) > 128 {
		return emaildiag.Failure(Construction)
	}
	for _, c := range a.ContentID {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("._-@", c)) {
			return emaildiag.Failure(Construction)
		}
	}
	return nil
}

// Format writes a fixed label without exposing attachment metadata.
func (Attachment) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("email attachment reference")) }

// LogValue returns a fixed log label without exposing attachment metadata.
func (Attachment) LogValue() slog.Value { return slog.StringValue("email attachment reference") }

// Message is immutable. Builders copy changed collections; no caller map or
// slice is retained. Validate runs again at the submission boundary.
type Message struct {
	from                 Address
	to, cc, bcc, replyTo []Address
	subject, text, html  string
	headers              map[string]string
	attachments          []Attachment
	data                 []DataAttachment
	locale               i18n.LocaleID
}

// MaxDataAttachmentBytes bounds one in-memory attachment; the mailer's
// MaxAttachmentBytes and MaxMessageBytes still apply.
const MaxDataAttachmentBytes = 10 << 20

// DataAttachment is in-memory content attached directly, for generated files
// such as a rendered invoice. It holds an owned copy of the bytes. It is not
// serializable: queued and snapshotted email must reference stored attachments.
type DataAttachment struct {
	metadata Attachment
	data     []byte
}

// NewDataAttachment copies and validates nonempty attachment data and metadata.
func NewDataAttachment(filename string, contentType storage.MediaType, data []byte) (DataAttachment, error) {
	result := DataAttachment{metadata: Attachment{Filename: filename, ContentType: contentType}, data: slices.Clone(data)}
	if len(data) == 0 || len(data) > MaxDataAttachmentBytes || result.metadata.validateMetadata() != nil {
		return DataAttachment{}, emaildiag.Failure(Construction)
	}
	return result, nil
}

// WithContentID returns an inline copy referenced as cid:id from HTML.
func (a DataAttachment) WithContentID(id string) (DataAttachment, error) {
	a.metadata.ContentID = id
	if id == "" || a.metadata.validateMetadata() != nil {
		return DataAttachment{}, emaildiag.Failure(Construction)
	}
	return a, nil
}

// Filename returns the attachment filename.
func (a DataAttachment) Filename() string { return a.metadata.Filename }

// ContentType returns the attachment's media type.
func (a DataAttachment) ContentType() storage.MediaType { return a.metadata.ContentType }

// ContentID returns the attachment's inline content identifier.
func (a DataAttachment) ContentID() string { return a.metadata.ContentID }

// Size returns the attachment byte count.
func (a DataAttachment) Size() int { return len(a.data) }

// Format writes a fixed label without exposing attachment data.
func (DataAttachment) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("email data attachment")) }

// LogValue returns a fixed log label without exposing attachment data.
func (DataAttachment) LogValue() slog.Value { return slog.StringValue("email data attachment") }

// NewMessage creates a message with a copy of the recipient slice; validation occurs separately.
func NewMessage(from Address, subject string, to ...Address) Message {
	return Message{from: from, subject: subject, to: slices.Clone(to)}
}

// Text returns a message copy with the supplied text body.
func (m Message) Text(text string) Message { m.text = text; return m }

// HTML returns a message copy with the supplied HTML body.
func (m Message) HTML(html string) Message { m.html = html; return m }

// Cc returns a message copy with carbon copy recipients appended.
func (m Message) Cc(addresses ...Address) Message {
	m.cc = append(slices.Clone(m.cc), addresses...)
	return m
}

// Bcc returns a message copy with blind carbon copy recipients appended.
func (m Message) Bcc(addresses ...Address) Message {
	m.bcc = append(slices.Clone(m.bcc), addresses...)
	return m
}

// ReplyTo returns a message copy with a copy of the supplied reply addresses.
func (m Message) ReplyTo(addresses ...Address) Message { m.replyTo = slices.Clone(addresses); return m }

// Header returns a message copy with a canonicalized header name and an independently owned header
// map.
func (m Message) Header(name, value string) Message {
	m.headers = maps.Clone(m.headers)
	if m.headers == nil {
		m.headers = make(map[string]string)
	}
	m.headers[textproto.CanonicalMIMEHeaderKey(name)] = value
	return m
}

// Attach returns a message copy with storage attachment references appended.
func (m Message) Attach(attachments ...Attachment) Message {
	m.attachments = append(slices.Clone(m.attachments), attachments...)
	return m
}

// AttachData adds in-memory attachments (see DataAttachment).
func (m Message) AttachData(attachments ...DataAttachment) Message {
	m.data = append(slices.Clone(m.data), attachments...)
	return m
}

// Locale records the message's language. MIME transports send it as the
// Content-Language header; LocalizedTemplate sets it when rendering.
func (m Message) Locale(locale i18n.LocaleID) Message { m.locale = locale; return m }

// LocaleID returns the message locale identifier.
func (m Message) LocaleID() i18n.LocaleID { return m.locale }

// DataAttachments returns a copy of the data attachment slice.
func (m Message) DataAttachments() []DataAttachment { return slices.Clone(m.data) }

// From returns the message sender.
func (m Message) From() Address { return m.from }

// To returns a copy of the primary recipient slice.
func (m Message) To() []Address { return slices.Clone(m.to) }

// CC returns a copy of the carbon copy recipient slice.
func (m Message) CC() []Address { return slices.Clone(m.cc) }

// BCC returns a copy of the blind carbon copy recipient slice.
func (m Message) BCC() []Address { return slices.Clone(m.bcc) }

// ReplyAddresses returns a copy of the reply address slice.
func (m Message) ReplyAddresses() []Address { return slices.Clone(m.replyTo) }

// Subject returns the message subject.
func (m Message) Subject() string { return m.subject }

// TextBody returns the text body.
func (m Message) TextBody() string { return m.text }

// HTMLBody returns the HTML body.
func (m Message) HTMLBody() string { return m.html }

// Headers returns a copy of the header map.
func (m Message) Headers() map[string]string { return maps.Clone(m.headers) }

// Attachments returns a copy of the storage attachment slice.
func (m Message) Attachments() []Attachment { return slices.Clone(m.attachments) }

// RecipientCount returns the total number of To, CC, and BCC addresses.
func (m Message) RecipientCount() int { return len(m.to) + len(m.cc) + len(m.bcc) }

// EnvelopeRecipients includes BCC. MIME headers deliberately exclude BCC.
func (m Message) EnvelopeRecipients() []Address {
	result := m.To()
	result = append(result, m.cc...)
	return append(result, m.bcc...)
}

// Format writes a fixed label without exposing message data.
func (Message) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("email message")) }

// MarshalJSON rejects accidental queuing of runtime messages. Queue a declared
// DTO containing template data and Attachment references through JobHandler.
func (Message) MarshalJSON() ([]byte, error) { return nil, emaildiag.Failure(Construction) }

// Validate checks sender, recipients, bodies, headers, locale, attachment limits, and inline
// content identifiers.
func (m Message) Validate() error {
	if m.from.Validate() != nil || len(m.to) == 0 || m.RecipientCount() > MaxRecipients || len(m.replyTo) > MaxRecipients || len(m.subject) > 2000 || !headerText(m.subject) || m.text == "" && m.html == "" || !utf8.ValidString(m.text) || !utf8.ValidString(m.html) {
		return emaildiag.Failure(Construction)
	}
	for _, group := range [][]Address{m.to, m.cc, m.bcc, m.replyTo} {
		for _, a := range group {
			if a.Validate() != nil {
				return emaildiag.Failure(Construction)
			}
		}
	}
	if len(m.attachments)+len(m.data) > MaxAttachments || len(m.headers) > MaxHeaders || m.locale != "" && m.locale.Validate() != nil {
		return emaildiag.Failure(Construction)
	}
	ids := make(map[string]bool)
	contentID := func(id string) bool {
		if id == "" {
			return true
		}
		if ids[id] || m.html == "" {
			return false
		}
		ids[id] = true
		return true
	}
	for _, a := range m.attachments {
		if a.Validate() != nil || !contentID(a.ContentID) {
			return emaildiag.Failure(Construction)
		}
	}
	for _, a := range m.data {
		if len(a.data) == 0 || a.metadata.validateMetadata() != nil || !contentID(a.metadata.ContentID) {
			return emaildiag.Failure(Construction)
		}
	}
	for k, v := range m.headers {
		if !validHeader(k, v) {
			return emaildiag.Failure(Construction)
		}
	}
	return nil
}
func validHeader(name, value string) bool {
	if name == "" || len(name) > 78 || len(value) > 512 || !headerText(value) {
		return false
	}
	for _, c := range name {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-') {
			return false
		}
	}
	name = strings.ToLower(name)
	if strings.HasPrefix(name, "content-") || strings.HasPrefix(name, "resent-") {
		return false
	}
	switch name {
	case "from", "sender", "to", "cc", "bcc", "reply-to", "subject", "date", "message-id", "mime-version", "return-path", "received", "dkim-signature", "authorization", "resend-idempotency-key":
		return false
	}
	return true
}
