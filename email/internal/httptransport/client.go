// Package httptransport owns the shared one-request, bounded-response policy.
package httptransport

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/weiloon1234/Foundry-Go/email"
	sharedtransport "github.com/weiloon1234/Foundry-Go/internal/httptransport"
)

const MaxResponseBytes = 64 << 10
const MaxRequestBytes = 48 << 20

type Client struct {
	base   string
	client *http.Client
	owned  *http.Transport
}

func New(config email.HTTPConfig, endpoint string) (*Client, error) {
	if config.Timeout <= 0 || config.Timeout > 10*time.Minute {
		return nil, email.Construction
	}
	if config.Endpoint != "" {
		endpoint = config.Endpoint
	}
	u, err := url.Parse(endpoint)
	if err != nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Opaque != "" {
		return nil, email.Construction
	}
	ip := net.ParseIP(u.Hostname())
	loopback := u.Hostname() == "localhost" || ip != nil && ip.IsLoopback()
	if u.Scheme != "https" && !(u.Scheme == "http" && loopback) {
		return nil, email.Construction
	}
	var owned *http.Transport
	transport := config.Transport
	if transport == nil {
		owned = sharedtransport.New(sharedtransport.Config{ConnectTimeout: config.Timeout, RequestTimeout: config.Timeout, MaxIdleConnections: 16, MaxIdlePerHost: 16, MaxResponseHeaderBytes: MaxResponseBytes})
		transport = owned
	}
	return &Client{base: strings.TrimRight(u.String(), "/"), client: &http.Client{Transport: transport, Timeout: config.Timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, owned: owned}, nil
}
func (c *Client) Close() {
	if c != nil && c.owned != nil {
		c.owned.CloseIdleConnections()
	}
}
func (c *Client) Request(ctx context.Context, path, contentType string, data []byte) (*http.Request, error) {
	if c == nil || ctx == nil || len(data) > MaxRequestBytes {
		return nil, email.Construction
	}
	if ctx.Err() != nil {
		return nil, email.Transient
	}
	r, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+path, bytes.NewReader(data))
	if err != nil {
		return nil, email.Construction
	}
	// net/http may replay POST with Idempotency-Key when GetBody is present.
	// Adapters promise exactly one transport attempt per Send.
	r.GetBody = nil
	r.Header.Set("Content-Type", contentType)
	r.Header.Set("Accept", "application/json")
	return r, nil
}
func (c *Client) Do(request *http.Request) (int, []byte, error) {
	if request.Context().Err() != nil {
		return 0, nil, email.Transient
	}
	r, err := c.client.Do(request)
	if r != nil && r.Body != nil {
		defer r.Body.Close()
	}
	if err != nil || r == nil || r.Body == nil {
		return 0, nil, email.Ambiguous
	}
	data, err := io.ReadAll(io.LimitReader(r.Body, MaxResponseBytes+1))
	if err != nil || len(data) > MaxResponseBytes {
		return r.StatusCode, nil, email.Ambiguous
	}
	return r.StatusCode, data, nil
}

// Status is conservative: server failures can follow acceptance. A documented
// provider rejection can refine this classification after inspecting its code.
func Status(status int) error {
	switch {
	case status >= 200 && status < 300:
		return nil
	case status == 429:
		return email.Transient
	case status == 408 || status >= 500:
		return email.Ambiguous
	default:
		return email.Permanent
	}
}
func JSON(data []byte, target any) error {
	if len(data) == 0 || json.Unmarshal(data, target) != nil {
		return email.Ambiguous
	}
	return nil
}
func Token(text string) bool {
	if text == "" || len(text) > 4096 {
		return false
	}
	for _, c := range text {
		if c < 33 || c > 126 {
			return false
		}
	}
	return true
}
func Strings(addresses []email.Address) []string {
	if len(addresses) == 0 {
		return nil
	}
	result := make([]string, len(addresses))
	for i, a := range addresses {
		result[i] = a.Header()
	}
	return result
}
