package email

import (
	"encoding/json"
	"fmt"
	"github.com/weiloon1234/Foundry-Go/i18n"
	"log/slog"

	"github.com/weiloon1234/Foundry-Go/value"
)

// Snapshot is an explicit durable, rendered message. Capture it only after
// resolving the intended recipient. It contains private email content and must
// be protected like an outbox payload. It contains no readers or executable code.
// Unlike a source-data job, retrying a snapshot never rerenders or changes routes.
type Snapshot struct{ data value.JSON[snapshotWire] }

type snapshotWire struct {
	Version     uint8             `json:"version"`
	From        Address           `json:"from"`
	To          []Address         `json:"to"`
	CC          []Address         `json:"cc"`
	BCC         []Address         `json:"bcc"`
	ReplyTo     []Address         `json:"reply_to"`
	Subject     string            `json:"subject"`
	Text        string            `json:"text"`
	HTML        string            `json:"html"`
	Headers     map[string]string `json:"headers"`
	Attachments []Attachment      `json:"attachments"`
	Locale      i18n.LocaleID     `json:"locale,omitempty"`
}

// CaptureMessage freezes a validated message within value.JSONMaxBytes. Every
// attachment must pin Version or IfMatch; mutable storage names cannot enter a
// durable rendered message. Ordinary Message JSON serialization remains denied.
func CaptureMessage(message Message) (Snapshot, error) {
	if err := message.Validate(); err != nil {
		return Snapshot{}, err
	}
	for _, a := range message.attachments {
		if a.Version == "" && a.IfMatch == "" {
			return Snapshot{}, Construction
		}
	}
	// In-memory content is not durable; snapshots reference stored files.
	if len(message.data) > 0 || len(message.text)+len(message.html) > value.JSONMaxBytes {
		return Snapshot{}, Construction
	}
	wire := snapshotWire{1, message.from, message.to, message.cc, message.bcc, message.replyTo, message.subject, message.text, message.html, message.headers, message.attachments, message.locale}
	data, err := value.NewJSON(wire)
	if err != nil {
		return Snapshot{}, Construction
	}
	return Snapshot{data: data}, nil
}

func (s Snapshot) Message() (Message, error) {
	wire, err := s.data.Decode()
	if err != nil || wire.Version != 1 {
		return Message{}, Construction
	}
	message := Message{from: wire.From, to: wire.To, cc: wire.CC, bcc: wire.BCC, replyTo: wire.ReplyTo, subject: wire.Subject, text: wire.Text, html: wire.HTML, headers: wire.Headers, attachments: wire.Attachments, locale: wire.Locale}
	if err := message.Validate(); err != nil {
		return Message{}, err
	}
	for _, a := range message.attachments {
		if a.Version == "" && a.IfMatch == "" {
			return Message{}, Construction
		}
	}
	return message, nil
}
func (s Snapshot) MarshalJSON() ([]byte, error) {
	if _, err := s.Message(); err != nil {
		return nil, err
	}
	return s.data.MarshalJSON()
}
func (s *Snapshot) UnmarshalJSON(data []byte) error {
	if s == nil || len(data) > value.JSONMaxBytes {
		return Construction
	}
	var result Snapshot
	if err := json.Unmarshal(data, &result.data); err != nil {
		return Construction
	}
	if _, err := result.Message(); err != nil {
		return err
	}
	*s = result
	return nil
}
func (Snapshot) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("email snapshot")) }
func (Snapshot) LogValue() slog.Value       { return slog.StringValue("email snapshot") }
