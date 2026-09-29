// Package resend submits through Resend's single-email HTTP API. Idempotency
// keys are forwarded unchanged; provider retention is 24 hours, not unlimited.
package resend

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"

	"github.com/weiloon1234/Foundry-Go/email"
	"github.com/weiloon1234/Foundry-Go/email/internal/httptransport"
	"github.com/weiloon1234/Foundry-Go/secret"
)

type Config struct {
	HTTP  email.HTTPConfig
	Token secret.String
}

func (Config) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("Resend configuration")) }

type Driver struct {
	client *httptransport.Client
	token  secret.String
}

func New(config Config) (*Driver, error) {
	if !httptransport.Token(config.Token.Reveal()) {
		return nil, email.Construction
	}
	c, err := httptransport.New(config.HTTP, "https://api.resend.com")
	if err != nil {
		return nil, err
	}
	return &Driver{client: c, token: config.Token}, nil
}
func (d *Driver) Close() {
	if d != nil {
		d.client.Close()
	}
}
func (Driver) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("Resend driver")) }

// StructuredSubmission reports that this provider API sends native fields, so
// the mailer skips MIME rendering.
func (*Driver) StructuredSubmission() bool { return true }

type attachment struct {
	Filename    string `json:"filename"`
	Content     string `json:"content"`
	ContentType string `json:"content_type"`
	ContentID   string `json:"content_id,omitempty"`
}

func (d *Driver) Send(ctx context.Context, out email.Outbound) (email.Receipt, error) {
	if d == nil || d.client == nil || out.Validate() != nil {
		return email.Receipt{}, email.Construction
	}
	m := out.Message()
	body := struct {
		From        string            `json:"from"`
		To          []string          `json:"to"`
		CC          []string          `json:"cc,omitempty"`
		BCC         []string          `json:"bcc,omitempty"`
		ReplyTo     []string          `json:"reply_to,omitempty"`
		Subject     string            `json:"subject"`
		Text        string            `json:"text,omitempty"`
		HTML        string            `json:"html,omitempty"`
		Headers     map[string]string `json:"headers,omitempty"`
		Attachments []attachment      `json:"attachments,omitempty"`
	}{From: m.From().Header(), To: httptransport.Strings(m.To()), CC: httptransport.Strings(m.CC()), BCC: httptransport.Strings(m.BCC()), ReplyTo: httptransport.Strings(m.ReplyAddresses()), Subject: m.Subject(), Text: m.TextBody(), HTML: m.HTMLBody(), Headers: m.Headers()}
	for _, a := range out.Attachments() {
		ref := a.Reference()
		body.Attachments = append(body.Attachments, attachment{ref.Filename, base64.StdEncoding.EncodeToString(a.Bytes()), string(ref.ContentType), ref.ContentID})
	}
	data, err := json.Marshal(body)
	if err != nil {
		return email.Receipt{}, email.Construction
	}
	r, err := d.client.Request(ctx, "/emails", "application/json", data)
	if err != nil {
		return email.Receipt{}, err
	}
	r.Header.Set("Authorization", "Bearer "+d.token.Reveal())
	if out.IdempotencyKey() != "" {
		r.Header.Set("Idempotency-Key", string(out.IdempotencyKey()))
	}
	status, data, err := d.client.Do(r)
	if err != nil {
		return email.Receipt{}, err
	}
	if status == 409 {
		var conflict struct {
			Name string `json:"name"`
		}
		if httptransport.JSON(data, &conflict) == nil && conflict.Name == "concurrent_idempotent_requests" {
			return email.Receipt{}, email.Transient
		}
	}
	if err := httptransport.Status(status); err != nil {
		return email.Receipt{}, err
	}
	var result struct {
		ID string `json:"id"`
	}
	if httptransport.JSON(data, &result) != nil || result.ID == "" {
		return email.Receipt{}, email.Ambiguous
	}
	receipt := email.Receipt{MessageID: result.ID}
	return receipt, receipt.Validate()
}
