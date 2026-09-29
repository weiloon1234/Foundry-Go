package log

import (
	"context"
	"log/slog"
	"strings"

	"github.com/weiloon1234/Foundry-Go/email"
)

// DefaultPreviewBytes bounds each logged body.
const DefaultPreviewBytes = 64 << 10

// PreviewDriver is a development transport: it simulates acceptance and logs
// the rendered message (sender, recipients, subject, locale, headers, text and
// HTML bodies and attachment metadata) so developers can read mail without a
// provider. Header values whose names suggest credentials (authorization,
// token, secret, key, password, signature, cookie, session) are redacted and
// attachment bytes are never logged. It records private content: use it only
// in local and development environments.
type PreviewDriver struct {
	logger   *slog.Logger
	maxBytes int
}

// NewPreview logs through logger; maxBytes bounds each body (zero selects
// DefaultPreviewBytes).
func NewPreview(logger *slog.Logger, maxBytes int) (*PreviewDriver, error) {
	if maxBytes == 0 {
		maxBytes = DefaultPreviewBytes
	}
	if logger == nil || maxBytes < 1 || maxBytes > 1<<20 {
		return nil, email.Construction
	}
	return &PreviewDriver{logger: logger, maxBytes: maxBytes}, nil
}

var sensitiveHeaderWords = []string{"authorization", "token", "secret", "key", "password", "signature", "cookie", "session", "auth"}

// RedactHeader reports the logged form of one header value.
func RedactHeader(name, value string) string {
	lower := strings.ToLower(name)
	for _, word := range sensitiveHeaderWords {
		if strings.Contains(lower, word) {
			return "[redacted]"
		}
	}
	return value
}

func (d *PreviewDriver) Send(ctx context.Context, out email.Outbound) (email.Receipt, error) {
	if d == nil || d.logger == nil || ctx == nil || out.Validate() != nil {
		return email.Receipt{}, email.Construction
	}
	if ctx.Err() != nil {
		return email.Receipt{}, email.Transient
	}
	m := out.Message()
	addresses := func(list []email.Address) []string {
		result := make([]string, len(list))
		for i, address := range list {
			result[i] = address.Mailbox()
		}
		return result
	}
	headers := make(map[string]string)
	for name, value := range m.Headers() {
		headers[name] = RedactHeader(name, value)
	}
	type attachment struct {
		Filename    string `json:"filename"`
		ContentType string `json:"content_type"`
		ContentID   string `json:"content_id,omitempty"`
		Bytes       int    `json:"bytes"`
	}
	var attachments []attachment
	for _, item := range out.Attachments() {
		reference := item.Reference()
		attachments = append(attachments, attachment{Filename: reference.Filename, ContentType: string(reference.ContentType), ContentID: reference.ContentID, Bytes: item.Size()})
	}
	bound := func(text string) string {
		if len(text) <= d.maxBytes {
			return text
		}
		return strings.ToValidUTF8(text[:d.maxBytes], "") + "…[truncated]"
	}
	d.logger.LogAttrs(ctx, slog.LevelInfo, "email preview",
		slog.String("from", m.From().Mailbox()), slog.Any("to", addresses(m.To())), slog.Any("cc", addresses(m.CC())),
		slog.Int("bcc", len(m.BCC())), slog.String("subject", m.Subject()), slog.String("locale", string(m.LocaleID())),
		slog.Any("headers", headers), slog.String("text", bound(m.TextBody())), slog.String("html", bound(m.HTMLBody())),
		slog.Any("attachments", attachments), slog.Int("bytes", out.Size()))
	return email.Receipt{}, nil
}
