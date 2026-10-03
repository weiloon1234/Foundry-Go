// Package cloudflare submits MIME through Cloudflare Email Service's REST API.
// Provider acceptance does not guarantee delivery to every envelope recipient.
package cloudflare

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/weiloon1234/Foundry-Go/email"
	"github.com/weiloon1234/Foundry-Go/email/internal/httptransport"
	"github.com/weiloon1234/Foundry-Go/secret"
)

type Config struct {
	HTTP      email.HTTPConfig
	AccountID string
	Token     secret.String
}

func (Config) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("Cloudflare email configuration")) }

type Driver struct {
	client *httptransport.Client
	path   string
	token  secret.String
}

// New validates configuration without I/O. AccountID is the 32-character
// hexadecimal Cloudflare account identifier; Token needs Email Sending access.
func New(config Config) (*Driver, error) {
	if len(config.AccountID) != 32 || !httptransport.Token(config.Token.Reveal()) {
		return nil, email.Construction
	}
	if _, err := hex.DecodeString(config.AccountID); err != nil {
		return nil, email.Construction
	}
	client, err := httptransport.New(config.HTTP, "https://api.cloudflare.com/client/v4")
	if err != nil {
		return nil, err
	}
	return &Driver{client: client, path: "/accounts/" + config.AccountID + "/email/sending/send_raw", token: config.Token}, nil
}

func (d *Driver) Close() {
	if d != nil {
		d.client.Close()
	}
}
func (Driver) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("Cloudflare email driver")) }

func (d *Driver) Send(ctx context.Context, out email.Outbound) (email.Receipt, error) {
	// Use the general sending limit, including MIME encoding and attachments.
	// The provider's larger verified-destination-only allowance is not assumed.
	if d == nil || d.client == nil || ctx == nil || out.Validate() != nil || out.Size() > 5<<20 {
		return email.Receipt{}, email.Construction
	}
	if ctx.Err() != nil {
		return email.Receipt{}, email.Transient
	}
	wire := out.MIME()
	if len(wire) == 0 {
		return email.Receipt{}, email.Construction
	}
	m := out.Message()
	recipients := m.EnvelopeRecipients()
	body := struct {
		From        string   `json:"from"`
		Recipients  []string `json:"recipients"`
		MIMEMessage string   `json:"mime_message"`
	}{From: m.From().Mailbox(), Recipients: make([]string, len(recipients)), MIMEMessage: string(wire)}
	for i, recipient := range recipients {
		body.Recipients[i] = recipient.Mailbox()
	}
	data, err := json.Marshal(body)
	if err != nil {
		return email.Receipt{}, email.Construction
	}
	r, err := d.client.Request(ctx, d.path, "application/json", data)
	if err != nil {
		return email.Receipt{}, err
	}
	r.Header.Set("Authorization", "Bearer "+d.token.Reveal())
	status, data, err := d.client.Do(r)
	if err != nil {
		return email.Receipt{}, err
	}
	var response struct {
		Success *bool `json:"success"`
		Errors  []struct {
			Code int `json:"code"`
		} `json:"errors"`
		Result *struct {
			MessageID string `json:"message_id"`
		} `json:"result"`
	}
	decoded := httptransport.JSON(data, &response) == nil
	// Only the documented authentication-service rejection refines a 503.
	// An unknown server failure may have happened after submission.
	if status == 503 && decoded && response.Success != nil && !*response.Success && response.Result == nil && len(response.Errors) == 1 && response.Errors[0].Code == 10100 {
		return email.Receipt{}, email.Transient
	}
	if err := httptransport.Status(status); err != nil {
		return email.Receipt{}, err
	}
	if !decoded || response.Success == nil || !*response.Success || len(response.Errors) != 0 || response.Result == nil || response.Result.MessageID == "" {
		return email.Receipt{}, email.Ambiguous
	}
	// Bounced or suppressed recipients do not undo acceptance. Retrying the
	// entire message could duplicate delivery to recipients already accepted.
	receipt := email.Receipt{MessageID: response.Result.MessageID}
	return receipt, receipt.Validate()
}
