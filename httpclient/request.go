package httpclient

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/weiloon1234/Foundry-Go/internal/httptoken"
	"github.com/weiloon1234/Foundry-Go/secret"
)

// Request is an immutable client-bound request. Builders retain errors until Do
// or Stream, so invalid requests never invoke body factories or transports.
type Request struct {
	client  *Client
	method  string
	url     *url.URL
	headers http.Header
	body    Body
	retry   RetryPolicy
	err     error
}

func (c *Client) Request(method, path string) Request {
	r := Request{client: c, method: method}
	if c == nil || len(method) > 32 || !httptoken.Valid(method) || method != strings.ToUpper(method) || method == http.MethodConnect {
		r.err = invalid()
		return r
	}
	r.url, r.err = requestURL(c.base, path)
	if r.err != nil {
		return r
	}
	r.headers = c.config.Headers.Clone()
	r.retry = c.config.Retry
	return r
}
func (c *Client) Get(path string) Request  { return c.Request(http.MethodGet, path) }
func (c *Client) Post(path string) Request { return c.Request(http.MethodPost, path) }
func (r Request) Header(name, value string) Request {
	if err := r.Validate(); err != nil {
		r.err = err
		return r
	}
	r.headers = r.headers.Clone()
	r.headers.Set(name, value)
	r.headers, r.err = copyHeaders(r.headers, r.client.config.HeaderBytes, true)
	return r
}
func (r Request) Bearer(token secret.String) Request {
	if err := r.Validate(); err != nil {
		r.err = err
		return r
	}
	if !bearerValue(token.Reveal()) {
		r.err = invalid()
		return r
	}
	return r.Header("Authorization", "Bearer "+token.Reveal())
}
func (r Request) QueryPair(name, value string) Request {
	if err := r.Validate(); err != nil {
		r.err = err
		return r
	}
	if len(name) == 0 || len(name)+len(value) > maxURLBytes {
		r.err = invalid()
		return r
	}
	u := *r.url
	values, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		r.err = invalid()
		return r
	}
	values.Add(name, value)
	u.RawQuery = values.Encode()
	if len(u.String()) > maxURLBytes {
		r.err = invalid()
		return r
	}
	r.url = &u
	return r
}
func (r Request) WithBody(body Body) Request {
	if err := r.Validate(); err != nil {
		r.err = err
		return r
	}
	if body.err != nil || body.length > r.client.config.RequestBytes {
		r.err = invalid()
		return r
	}
	r.body = body
	return r
}
func (r Request) WithRetry(policy RetryPolicy) Request {
	if err := r.Validate(); err != nil {
		r.err = err
		return r
	}
	if err := policy.Validate(); err != nil {
		r.err = err
		return r
	}
	r.retry = policy
	return r
}
func (r Request) Validate() error {
	if r.err != nil {
		return r.err
	}
	if r.client == nil || r.url == nil {
		return invalid()
	}
	return r.client.config.Destination.checkURL(r.url)
}
func (r Request) URL() string {
	if r.url == nil {
		return ""
	}
	return r.url.String()
}
func (r Request) Method() string               { return r.method }
func (Request) Format(state fmt.State, _ rune) { _, _ = state.Write([]byte("HTTP request")) }
