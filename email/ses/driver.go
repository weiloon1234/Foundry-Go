// Package ses submits MIME through SES SendRawEmail using the existing AWS SDK
// credential and SigV4 interfaces. It does not install a second AWS client stack.
package ses

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"github.com/weiloon1234/Foundry-Go/email"
	"github.com/weiloon1234/Foundry-Go/email/internal/httptransport"
)

type Config struct {
	HTTP             email.HTTPConfig
	Region           string
	Credentials      aws.CredentialsProvider
	ConfigurationSet string
}

func (Config) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("SES configuration")) }

type Driver struct {
	client                   *httptransport.Client
	region, configurationSet string
	credentials              aws.CredentialsProvider
	signer                   *v4.Signer
}

var regionPattern = regexp.MustCompile(`^[a-z]{2}(?:-[a-z]+)+-[0-9]+$`)

func New(config Config) (*Driver, error) {
	if !regionPattern.MatchString(config.Region) || len(config.Region) > 64 || config.Credentials == nil || len(config.ConfigurationSet) > 64 {
		return nil, email.Construction
	}
	for _, c := range config.ConfigurationSet {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return nil, email.Construction
		}
	}
	suffix := "amazonaws.com"
	if strings.HasPrefix(config.Region, "cn-") {
		suffix += ".cn"
	}
	c, err := httptransport.New(config.HTTP, "https://email."+config.Region+"."+suffix)
	if err != nil {
		return nil, err
	}
	return &Driver{client: c, region: config.Region, configurationSet: config.ConfigurationSet, credentials: config.Credentials, signer: v4.NewSigner()}, nil
}
func (d *Driver) Close() {
	if d != nil {
		d.client.Close()
	}
}
func (Driver) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("SES driver")) }
func (d *Driver) Send(ctx context.Context, out email.Outbound) (email.Receipt, error) {
	if d == nil || d.client == nil || out.Validate() != nil || out.Size() > 10<<20 || ctx == nil {
		return email.Receipt{}, email.Construction
	}
	if ctx.Err() != nil {
		return email.Receipt{}, email.Transient
	}
	m := out.Message()
	values := url.Values{"Action": {"SendRawEmail"}, "Version": {"2010-12-01"}, "Source": {m.From().Mailbox()}, "RawMessage.Data": {base64.StdEncoding.EncodeToString(out.MIME())}}
	for i, a := range m.EnvelopeRecipients() {
		values.Set("Destinations.member."+strconv.Itoa(i+1), a.Mailbox())
	}
	if d.configurationSet != "" {
		values.Set("ConfigurationSetName", d.configurationSet)
	}
	data := []byte(values.Encode())
	r, err := d.client.Request(ctx, "/", "application/x-www-form-urlencoded", data)
	if err != nil {
		return email.Receipt{}, err
	}
	r.Header.Set("Accept", "application/xml")
	credentials, err := d.credentials.Retrieve(ctx)
	if err != nil {
		return email.Receipt{}, email.Transient
	}
	if credentials.AccessKeyID == "" || credentials.SecretAccessKey == "" {
		return email.Receipt{}, email.Construction
	}
	hash := sha256.Sum256(data)
	if d.signer.SignHTTP(ctx, credentials, r, hex.EncodeToString(hash[:]), "ses", d.region, time.Now().UTC()) != nil {
		return email.Receipt{}, email.Construction
	}
	status, data, err := d.client.Do(r)
	if err != nil {
		return email.Receipt{}, err
	}
	var failure struct {
		Code string `xml:"Error>Code"`
	}
	if status < 200 || status >= 300 {
		if xml.Unmarshal(data, &failure) == nil {
			switch failure.Code {
			case "Throttling", "ThrottlingException", "ServiceUnavailable":
				return email.Receipt{}, email.Transient
			case "MessageRejected", "MailFromDomainNotVerifiedException", "ConfigurationSetDoesNotExistException", "AccountSendingPausedException", "ConfigurationSetSendingPausedException":
				return email.Receipt{}, email.Permanent
			}
		}
		return email.Receipt{}, httptransport.Status(status)
	}
	var result struct {
		ID string `xml:"SendRawEmailResult>MessageId"`
	}
	if xml.Unmarshal(data, &result) != nil || result.ID == "" {
		return email.Receipt{}, email.Ambiguous
	}
	receipt := email.Receipt{MessageID: result.ID}
	return receipt, receipt.Validate()
}
