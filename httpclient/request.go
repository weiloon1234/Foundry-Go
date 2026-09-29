package httpclient

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"mime"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"slices"
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

// BasicAuth sets RFC 7617 Basic credentials. The user ID must not contain a
// colon or control characters; the password is revealed only into the header.
func (r Request) BasicAuth(user string, password secret.String) Request {
	if err := r.Validate(); err != nil {
		r.err = err
		return r
	}
	if user == "" || len(user) > 1024 || strings.ContainsRune(user, ':') || len(password.Reveal()) > 4096 || strings.ContainsFunc(user+password.Reveal(), func(c rune) bool { return c < 32 || c == 127 }) {
		r.err = invalid()
		return r
	}
	return r.Header("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(user+":"+password.Reveal())))
}

// Form encodes values as an application/x-www-form-urlencoded replayable body.
func (r Request) Form(values url.Values) Request {
	if err := r.Validate(); err != nil {
		r.err = err
		return r
	}
	return r.WithBody(Bytes([]byte(values.Encode()))).Header("Content-Type", "application/x-www-form-urlencoded")
}

// Part is one multipart/form-data part: a field value or a file.
type Part struct {
	name, filename string
	contentType    string
	data           []byte
}

// Field is a multipart text field.
func Field(name, value string) Part { return Part{name: name, data: []byte(value)} }

// File is a multipart file part. The filename is presentation metadata only.
func File(name, filename, contentType string, data []byte) Part {
	return Part{name: name, filename: filename, contentType: contentType, data: slices.Clone(data)}
}
func (Part) Format(state fmt.State, _ rune) { _, _ = state.Write([]byte("multipart part")) }

// Multipart encodes parts once as a replayable multipart/form-data body within
// the client's request byte limit and sets its Content-Type with the boundary.
func (r Request) Multipart(parts ...Part) Request {
	if err := r.Validate(); err != nil {
		r.err = err
		return r
	}
	if len(parts) == 0 || len(parts) > 256 {
		r.err = invalid()
		return r
	}
	var buffer bytes.Buffer
	writer := multipart.NewWriter(&buffer)
	for _, part := range parts {
		if part.name == "" || len(part.name) > 256 || len(part.filename) > 1024 || strings.ContainsFunc(part.name+part.filename+part.contentType, func(c rune) bool { return c < 32 || c == 127 }) {
			r.err = invalid()
			return r
		}
		header := make(textproto.MIMEHeader)
		disposition := `form-data; name="` + quoteEscaper.Replace(part.name) + `"`
		if part.filename != "" || part.contentType != "" {
			disposition += `; filename="` + quoteEscaper.Replace(part.filename) + `"`
			contentType := part.contentType
			if contentType == "" {
				contentType = "application/octet-stream"
			}
			if _, _, err := mime.ParseMediaType(contentType); err != nil {
				r.err = invalid()
				return r
			}
			header.Set("Content-Type", contentType)
		}
		header.Set("Content-Disposition", disposition)
		destination, err := writer.CreatePart(header)
		if err == nil {
			_, err = destination.Write(part.data)
		}
		if err != nil || int64(buffer.Len()) > r.client.config.RequestBytes {
			r.err = invalid()
			return r
		}
	}
	if err := writer.Close(); err != nil || int64(buffer.Len()) > r.client.config.RequestBytes {
		r.err = invalid()
		return r
	}
	return r.WithBody(Bytes(buffer.Bytes())).Header("Content-Type", writer.FormDataContentType())
}

var quoteEscaper = strings.NewReplacer("\\", "\\\\", `"`, "\\\"")
