// Package mailgun submits multipart messages through Mailgun's v3 API.
package mailgun

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime"
	"mime/multipart"
	"net/textproto"
	"net/url"
	"slices"
	"strings"

	"github.com/weiloon1234/Foundry-Go/email"
	"github.com/weiloon1234/Foundry-Go/email/internal/httptransport"
	"github.com/weiloon1234/Foundry-Go/secret"
)

type Config struct {
	HTTP   email.HTTPConfig
	APIKey secret.String
	Domain string
}

func (Config) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("Mailgun configuration")) }

type Driver struct {
	client *httptransport.Client
	token  secret.String
	domain string
}

func New(config Config) (*Driver, error) {
	if !httptransport.Token(config.APIKey.Reveal()) || config.Domain == "" || len(config.Domain) > 253 {
		return nil, email.Construction
	}
	for _, c := range config.Domain {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '.') {
			return nil, email.Construction
		}
	}
	c, err := httptransport.New(config.HTTP, "https://api.mailgun.net")
	if err != nil {
		return nil, err
	}
	return &Driver{client: c, token: config.APIKey, domain: config.Domain}, nil
}
func (d *Driver) Close() {
	if d != nil {
		d.client.Close()
	}
}
func (Driver) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("Mailgun driver")) }

// StructuredSubmission reports that this provider API sends native fields, so
// the mailer skips MIME rendering.
func (*Driver) StructuredSubmission() bool { return true }
func (d *Driver) Send(ctx context.Context, out email.Outbound) (email.Receipt, error) {
	if d == nil || d.client == nil || out.Validate() != nil {
		return email.Receipt{}, email.Construction
	}
	m := out.Message()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fields := [][2]string{{"from", m.From().Header()}, {"subject", m.Subject()}}
	if m.TextBody() != "" {
		fields = append(fields, [2]string{"text", m.TextBody()})
	}
	if m.HTMLBody() != "" {
		fields = append(fields, [2]string{"html", m.HTMLBody()})
	}
	for _, group := range []struct {
		name      string
		addresses []email.Address
	}{{"to", m.To()}, {"cc", m.CC()}, {"bcc", m.BCC()}} {
		for _, a := range group.addresses {
			fields = append(fields, [2]string{group.name, a.Header()})
		}
	}
	if reply := m.ReplyAddresses(); len(reply) > 0 {
		fields = append(fields, [2]string{"h:Reply-To", strings.Join(httptransport.Strings(reply), ",")})
	}
	headers := m.Headers()
	keys := make([]string, 0, len(headers))
	for k := range headers {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		fields = append(fields, [2]string{"h:" + k, headers[k]})
	}
	headerBytes := 0
	for _, field := range fields {
		if strings.HasPrefix(field[0], "h:") {
			headerBytes += len(field[0]) + len(field[1])
		}
	}
	if headerBytes > 16<<10 {
		return email.Receipt{}, email.Construction
	}
	for _, field := range fields {
		if mw.WriteField(field[0], field[1]) != nil {
			return email.Receipt{}, email.Construction
		}
	}
	for _, a := range out.Attachments() {
		ref := a.Reference()
		field, filename := "attachment", ref.Filename
		// Mailgun identifies inline objects by their multipart filename.
		if ref.ContentID != "" {
			field, filename = "inline", ref.ContentID
		}
		header := textproto.MIMEHeader{"Content-Disposition": {mime.FormatMediaType("form-data", map[string]string{"name": field, "filename": filename})}, "Content-Type": {string(ref.ContentType)}}
		part, err := mw.CreatePart(header)
		if err != nil {
			return email.Receipt{}, email.Construction
		}
		if _, err = part.Write(a.Bytes()); err != nil {
			return email.Receipt{}, email.Construction
		}
	}
	if mw.Close() != nil || body.Len() > httptransport.MaxRequestBytes {
		return email.Receipt{}, email.Construction
	}
	r, err := d.client.Request(ctx, "/v3/"+url.PathEscape(d.domain)+"/messages", mw.FormDataContentType(), body.Bytes())
	if err != nil {
		return email.Receipt{}, err
	}
	r.SetBasicAuth("api", d.token.Reveal())
	status, data, err := d.client.Do(r)
	if err != nil {
		return email.Receipt{}, err
	}
	if err := httptransport.Status(status); err != nil {
		return email.Receipt{}, err
	}
	var result struct {
		ID string `json:"id"`
	}
	if json.Unmarshal(data, &result) != nil || result.ID == "" {
		return email.Receipt{}, email.Ambiguous
	}
	receipt := email.Receipt{MessageID: result.ID}
	return receipt, receipt.Validate()
}
