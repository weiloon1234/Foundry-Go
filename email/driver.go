package email

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
)

// IdempotencyKey identifies one logical submission. Reuse only for an identical
// message. Provider support and retention are documented by each adapter.
type IdempotencyKey string

func (k IdempotencyKey) Validate() error {
	if len(k) > 256 {
		return Construction
	}
	for _, c := range k {
		if c < 33 || c > 126 {
			return Construction
		}
	}
	return nil
}
func (IdempotencyKey) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("email idempotency key")) }

type SendOptions struct{ IdempotencyKey IdempotencyKey }

// ResolvedAttachment owns a bounded snapshot. Bytes returns a copy, never a
// reader into a storage resource. Drivers must not retain borrowed calls.
type ResolvedAttachment struct {
	reference Attachment
	data      []byte
}

func (a ResolvedAttachment) Reference() Attachment { return a.reference }
func (a ResolvedAttachment) Bytes() []byte         { return slices.Clone(a.data) }
func (a ResolvedAttachment) Size() int             { return len(a.data) }
func (ResolvedAttachment) Format(s fmt.State, _ rune) {
	_, _ = s.Write([]byte("resolved email attachment"))
}

// Outbound can only be constructed by Mailer after validation and resolution.
// Driver implementations receive this immutable submission snapshot.
type Outbound struct {
	message     Message
	attachments []ResolvedAttachment
	mime        []byte
	key         IdempotencyKey
}

func (o Outbound) Message() Message                  { return o.message }
func (o Outbound) Attachments() []ResolvedAttachment { return slices.Clone(o.attachments) }
func (o Outbound) MIME() []byte                      { return slices.Clone(o.mime) }
func (o Outbound) Size() int                         { return len(o.mime) }
func (o Outbound) IdempotencyKey() IdempotencyKey    { return o.key }
func (o Outbound) Validate() error {
	if len(o.mime) == 0 {
		return Construction
	}
	return nil
}
func (Outbound) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("outbound email")) }

// Receipt means provider acceptance, never inbox delivery. SMTP may have no ID.
type Receipt struct{ MessageID string }

func (r Receipt) Validate() error {
	if len(r.MessageID) > 512 || !headerText(r.MessageID) {
		return Ambiguous
	}
	return nil
}
func (Receipt) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("email acceptance receipt")) }

type Result struct {
	Receipt        Receipt
	Accepted       bool
	ObserverFailed bool
}

// Driver performs one submission. It must classify failures, honor context,
// avoid implicit retries, and return only after releasing its I/O. Mailer borrows
// it; the application closes transport resources after Mailer.Done.
type Driver interface {
	Send(context.Context, Outbound) (Receipt, error)
}
type DriverFunc func(context.Context, Outbound) (Receipt, error)

func (f DriverFunc) Send(ctx context.Context, o Outbound) (Receipt, error) { return f(ctx, o) }

func (IdempotencyKey) LogValue() slog.Value { return slog.StringValue("email idempotency key") }
func (Receipt) LogValue() slog.Value        { return slog.StringValue("email acceptance receipt") }
