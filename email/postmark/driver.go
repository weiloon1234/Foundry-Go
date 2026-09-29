// Package postmark submits through Postmark's single-email API.
package postmark

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/weiloon1234/Foundry-Go/email"
	"github.com/weiloon1234/Foundry-Go/email/internal/httptransport"
	"github.com/weiloon1234/Foundry-Go/secret"
)

type Config struct {
	HTTP          email.HTTPConfig
	ServerToken   secret.String
	MessageStream string
}

func (Config) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("Postmark configuration")) }

type Driver struct {
	client *httptransport.Client
	token  secret.String
	stream string
}

func New(config Config) (*Driver, error) {
	if !httptransport.Token(config.ServerToken.Reveal()) || config.MessageStream != "" && !httptransport.Token(config.MessageStream) || len(config.MessageStream) > 128 {
		return nil, email.Construction
	}
	c, err := httptransport.New(config.HTTP, "https://api.postmarkapp.com")
	if err != nil {
		return nil, err
	}
	return &Driver{client: c, token: config.ServerToken, stream: config.MessageStream}, nil
}
func (d *Driver) Close() {
	if d != nil {
		d.client.Close()
	}
}
func (Driver) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("Postmark driver")) }

// StructuredSubmission reports that this provider API sends native fields, so
// the mailer skips MIME rendering.
func (*Driver) StructuredSubmission() bool { return true }

type attachment struct {
	Name, Content, ContentType string
	ContentID                  string `json:",omitempty"`
}
type header struct{ Name, Value string }

func (d *Driver) Send(ctx context.Context, out email.Outbound) (email.Receipt, error) {
	if d == nil || d.client == nil || out.Validate() != nil {
		return email.Receipt{}, email.Construction
	}
	m := out.Message()
	body := struct {
		From, To, Subject                    string
		Cc, Bcc, ReplyTo, TextBody, HtmlBody string
		MessageStream                        string       `json:",omitempty"`
		Headers                              []header     `json:",omitempty"`
		Attachments                          []attachment `json:",omitempty"`
	}{From: m.From().Header(), To: strings.Join(httptransport.Strings(m.To()), ","), Cc: strings.Join(httptransport.Strings(m.CC()), ","), Bcc: strings.Join(httptransport.Strings(m.BCC()), ","), ReplyTo: strings.Join(httptransport.Strings(m.ReplyAddresses()), ","), Subject: m.Subject(), TextBody: m.TextBody(), HtmlBody: m.HTMLBody(), MessageStream: d.stream}
	headers := m.Headers()
	keys := make([]string, 0, len(headers))
	for k := range headers {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		body.Headers = append(body.Headers, header{k, headers[k]})
	}
	for _, a := range out.Attachments() {
		ref := a.Reference()
		cid := ""
		if ref.ContentID != "" {
			cid = "cid:" + ref.ContentID
		}
		body.Attachments = append(body.Attachments, attachment{ref.Filename, base64.StdEncoding.EncodeToString(a.Bytes()), string(ref.ContentType), cid})
	}
	data, err := json.Marshal(body)
	if err != nil {
		return email.Receipt{}, email.Construction
	}
	r, err := d.client.Request(ctx, "/email", "application/json", data)
	if err != nil {
		return email.Receipt{}, err
	}
	r.Header.Set("X-Postmark-Server-Token", d.token.Reveal())
	status, data, err := d.client.Do(r)
	if err != nil {
		return email.Receipt{}, err
	}
	var result struct {
		MessageID string
		ErrorCode *int
	}
	decoded := httptransport.JSON(data, &result) == nil
	if decoded && result.ErrorCode != nil && *result.ErrorCode == 100 {
		return email.Receipt{}, email.Transient
	}
	if err := httptransport.Status(status); err != nil {
		return email.Receipt{}, err
	}
	if !decoded || result.ErrorCode == nil {
		return email.Receipt{}, email.Ambiguous
	}
	if *result.ErrorCode != 0 {
		if *result.ErrorCode == 101 {
			return email.Receipt{}, email.Ambiguous
		}
		return email.Receipt{}, email.Permanent
	}
	if result.MessageID == "" {
		return email.Receipt{}, email.Ambiguous
	}
	receipt := email.Receipt{MessageID: result.MessageID}
	return receipt, receipt.Validate()
}
