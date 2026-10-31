package email

import (
	"context"
	"fmt"
	"github.com/weiloon1234/Foundry-Go/internal/emaildiag"
	"log/slog"
	"slices"
)

// IdempotencyKey identifies one logical submission. Reuse only for an identical
// message. Provider support and retention are documented by each adapter.
type IdempotencyKey string

// Validate checks that a nonempty key contains printable ASCII and is at most 256 bytes; an empty
// key is allowed.
func (k IdempotencyKey) Validate() error {
	if len(k) > 256 {
		return emaildiag.Failure(Construction)
	}
	for _, c := range k {
		if c < 33 || c > 126 {
			return emaildiag.Failure(Construction)
		}
	}
	return nil
}

// Format writes a fixed label without exposing the key.
func (IdempotencyKey) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("email idempotency key")) }

// SendOptions configures a send's idempotency key.
type SendOptions struct{ IdempotencyKey IdempotencyKey }

// ResolvedAttachment owns a bounded snapshot. Bytes returns a copy, never a
// reader into a storage resource. Drivers must not retain borrowed calls.
type ResolvedAttachment struct {
	reference Attachment
	data      []byte
}

// Reference returns the attachment reference metadata.
func (a ResolvedAttachment) Reference() Attachment { return a.reference }

// Bytes returns a copy of the attachment bytes.
func (a ResolvedAttachment) Bytes() []byte { return slices.Clone(a.data) }

// Size returns the attachment byte count.
func (a ResolvedAttachment) Size() int { return len(a.data) }

// Format writes a fixed label without exposing attachment data.
func (ResolvedAttachment) Format(s fmt.State, _ rune) {
	_, _ = s.Write([]byte("resolved email attachment"))
}

// Outbound can only be constructed by Mailer after validation and resolution.
// Driver implementations receive this immutable submission snapshot.
// A structured submission (see StructuredDriver) carries no MIME document.
type Outbound struct {
	message     Message
	attachments []ResolvedAttachment
	mime        []byte
	size        int
	structured  bool
	key         IdempotencyKey
}

// Message returns the outbound message.
func (o Outbound) Message() Message { return o.message }

// Attachments returns a copy of the resolved attachment slice.
func (o Outbound) Attachments() []ResolvedAttachment { return slices.Clone(o.attachments) }

// MIME returns a copy of the encoded MIME bytes.
func (o Outbound) MIME() []byte { return slices.Clone(o.mime) }

// Size is the encoded MIME size, or the estimated encoded size of bodies and
// attachments for a structured submission.
func (o Outbound) Size() int { return o.size }

// IdempotencyKey returns the submission's idempotency key.
func (o Outbound) IdempotencyKey() IdempotencyKey { return o.key }

// Validate checks that the outbound value has positive size and either MIME data or structured
// submission enabled.
func (o Outbound) Validate() error {
	if len(o.mime) == 0 && !o.structured || o.size <= 0 {
		return emaildiag.Failure(Construction)
	}
	return nil
}

// StructuredDriver is optional. A provider API that submits native fields
// (bodies, headers, attachments) rather than the rendered MIME document
// reports true, so Mailer skips MIME rendering; Outbound.MIME is then empty.
// SMTP, SES, Cloudflare, memory and log drivers receive the full MIME document.
type StructuredDriver interface {
	// StructuredSubmission reports whether the driver accepts structured submissions.
	StructuredSubmission() bool
}

// Format writes a fixed label without exposing message data.
func (Outbound) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("outbound email")) }

// Receipt means provider acceptance, never inbox delivery. SMTP may have no ID.
type Receipt struct{ MessageID string }

// Validate checks the receipt's message identifier, returning an ambiguous failure for invalid
// text.
func (r Receipt) Validate() error {
	if len(r.MessageID) > 512 || !headerText(r.MessageID) {
		return emaildiag.Failure(Ambiguous)
	}
	return nil
}

// Format writes a fixed label without exposing receipt data.
func (Receipt) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("email acceptance receipt")) }

// Result reports acceptance and whether an observer failed after sending.
type Result struct {
	Receipt        Receipt
	Accepted       bool
	ObserverFailed bool
}

// Driver performs one submission. It must classify failures, honor context,
// avoid implicit retries, and return only after releasing its I/O. Mailer borrows
// it; the application closes transport resources after Mailer.Done.
type Driver interface {
	// Send submits an outbound message and returns its acceptance receipt or failure.
	Send(context.Context, Outbound) (Receipt, error)
}

// DriverFunc adapts a function to Driver.
type DriverFunc func(context.Context, Outbound) (Receipt, error)

// Send calls the driver function with the outbound message.
func (f DriverFunc) Send(ctx context.Context, o Outbound) (Receipt, error) { return f(ctx, o) }

// LogValue returns a fixed log label without exposing the key.
func (IdempotencyKey) LogValue() slog.Value { return slog.StringValue("email idempotency key") }

// LogValue returns a fixed log label without exposing receipt data.
func (Receipt) LogValue() slog.Value { return slog.StringValue("email acceptance receipt") }
