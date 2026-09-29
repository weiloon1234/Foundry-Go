package storage

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Visibility is access intent. Foundry never changes object ACLs/bucket policy.
// Private disks reject stable public URLs even if their adapter has a CDN base.
type Visibility uint8

const (
	Private Visibility = iota
	Public
)

// PublicBase is an explicitly configured public bucket/CDN root. It is not
// inferred from an authenticated API endpoint and does not make a bucket public.
// Object keys are encoded once as literal path text, including percent signs.
// A literal '+' is encoded as %2B because some CDNs and S3-compatible servers
// decode a path '+' as a space.
type PublicBase struct{ address string }

func ParsePublicBase(address string) (PublicBase, error) {
	parsed, err := parseStorageURL(address)
	if err != nil {
		return PublicBase{}, err
	}
	if parsed.RawQuery != "" {
		return PublicBase{}, Failure(Invalid, SignOperation, NotApplicable, nil)
	}
	for _, part := range strings.Split(parsed.Path, "/") {
		if part == "." || part == ".." {
			return PublicBase{}, Failure(Invalid, SignOperation, NotApplicable, nil)
		}
	}
	return PublicBase{address: address}, nil
}
func (b PublicBase) IsZero() bool { return b.address == "" }
func (b PublicBase) URL(key ObjectKey) (string, error) {
	if err := key.Validate(); err != nil {
		return "", err
	}
	parsed, err := parseStorageURL(b.address)
	if err != nil {
		return "", err
	}
	escaped := strings.ReplaceAll((&url.URL{Path: key.String()}).EscapedPath(), "+", "%2B")
	raw := strings.TrimRight(parsed.EscapedPath(), "/") + "/" + escaped
	path, err := url.PathUnescape(raw)
	if err != nil {
		return "", Failure(Invalid, SignOperation, NotApplicable, err)
	}
	parsed.Path, parsed.RawPath = path, raw
	return parsed.String(), nil
}
func parseStorageURL(address string) (*url.URL, error) {
	if len(address) == 0 || len(address) > 32<<10 {
		return nil, Failure(Invalid, SignOperation, NotApplicable, nil)
	}
	parsed, err := url.Parse(address)
	if err != nil || parsed.Scheme != "https" && parsed.Scheme != "http" || parsed.Hostname() == "" || parsed.User != nil || parsed.Fragment != "" || parsed.Opaque != "" {
		return nil, Failure(Invalid, SignOperation, NotApplicable, err)
	}
	return parsed, nil
}

type URLProvider interface {
	PublicURL(context.Context, ObjectKey) (string, error)
}

// SignedURLProvider produces a read-only bearer URL. A URL remains usable until
// expiry/credential revocation even if the framework disk subsequently closes.
type SignedURLProvider interface {
	TemporaryURL(context.Context, ObjectKey, LinkOptions) (TemporaryURL, error)
}

// LinkOptions bounds one signed read. ResponseContentType and
// ResponseContentDisposition are signed provider response overrides (for
// example to force a download filename); use RFC 8187 encoding for non-ASCII
// filenames. They never change stored object metadata.
type LinkOptions struct {
	ExpiresIn                  time.Duration
	Version                    VersionID
	ResponseContentType        MediaType
	ResponseContentDisposition string
}

const MaxLinkLifetime = 7 * 24 * time.Hour

func (o LinkOptions) Validate() error {
	if o.ExpiresIn < time.Second || o.ExpiresIn > MaxLinkLifetime || !headerText(o.ResponseContentDisposition) {
		return Failure(Invalid, SignOperation, NotApplicable, nil)
	}
	if o.ResponseContentType != "" {
		if err := o.ResponseContentType.Validate(); err != nil {
			return Failure(Invalid, SignOperation, NotApplicable, err)
		}
	}
	return o.Version.Validate()
}

// SignedUploadProvider produces a presigned single-request upload. The exact
// size, media type, optional checksum metadata and optional absence condition
// are signed; the client must send UploadLink.Headers unchanged.
type SignedUploadProvider interface {
	TemporaryUploadURL(context.Context, ObjectKey, UploadLinkOptions) (UploadLink, error)
}

// UploadLinkOptions describes a direct client upload. ContentType and Size are
// required and exact; Size must be at least one byte (a zero length is not
// signed, so an empty link would accept any size). Checksum is recorded as the object's full SHA-256
// metadata; the provider does not verify it, but complete framework reads do.
// Condition supports IfAbsent only where the disk supports conditional create.
type UploadLinkOptions struct {
	ExpiresIn   time.Duration
	ContentType MediaType
	Size        int64
	Checksum    value.Optional[SHA256]
	Condition   WriteCondition
}

func (o UploadLinkOptions) Validate(maximum int64) error {
	if o.ExpiresIn < time.Second || o.ExpiresIn > MaxLinkLifetime || o.Condition.Match() != "" {
		return Failure(Invalid, SignOperation, NotApplicable, nil)
	}
	// SigV4 omits Content-Length from the signature when it is zero, so an
	// empty upload link would accept any size. Store empty objects with Put.
	if o.Size < 1 {
		return Failure(Invalid, SignOperation, NotApplicable, fault.New(fault.Invalid, "an upload link requires a size of at least one byte"))
	}
	if o.Size > maximum {
		return Failure(LimitExceeded, SignOperation, NotApplicable, nil)
	}
	if err := o.ContentType.Validate(); err != nil {
		return Failure(Invalid, SignOperation, NotApplicable, err)
	}
	return nil
}

// UploadLink is a signed bearer upload request. Formatting redacts it and
// implicit JSON fails; explicitly return URL, Method and Headers in an
// authorized response DTO. The object is written by the client, not by this
// process: confirm it with Stat (and its checksum) before relying on it.
type UploadLink struct {
	address string
	method  string
	headers http.Header
	expires time.Time
}

func NewUploadLink(address, method string, headers http.Header, expires time.Time) (UploadLink, error) {
	if _, err := parseStorageURL(address); err != nil {
		return UploadLink{}, err
	}
	if method != http.MethodPut || expires.IsZero() || expires.Year() < 1 || expires.Year() > 9999 || len(headers) > 32 {
		return UploadLink{}, Failure(Invalid, SignOperation, NotApplicable, nil)
	}
	for name, values := range headers {
		for _, value := range values {
			if !headerText(name) || !headerText(value) {
				return UploadLink{}, Failure(Invalid, SignOperation, NotApplicable, nil)
			}
		}
	}
	return UploadLink{address: address, method: method, headers: headers.Clone(), expires: expires.UTC().Round(0)}, nil
}
func (l UploadLink) URL() string              { return l.address }
func (l UploadLink) Method() string           { return l.method }
func (l UploadLink) Headers() http.Header     { return l.headers.Clone() }
func (l UploadLink) ExpiresAt() time.Time     { return l.expires }
func (UploadLink) String() string             { return "temporary storage upload URL" }
func (UploadLink) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("temporary storage upload URL")) }
func (UploadLink) LogValue() slog.Value       { return slog.StringValue("temporary storage upload URL") }
func (UploadLink) MarshalJSON() ([]byte, error) {
	return nil, Failure(Invalid, SignOperation, NotApplicable, nil)
}

// SignedFormProvider presigns a browser form upload (an HTML POST policy).
type SignedFormProvider interface {
	TemporaryUploadForm(context.Context, ObjectKey, UploadFormOptions) (UploadForm, error)
}

// UploadFormOptions constrains a direct browser form upload. The object key
// and ContentType are exact and the uploaded size must lie within
// [MinSize, MaxSize] (a signed content-length-range). Checksum, when set, is
// recorded as the object's full SHA-256 metadata; the provider does not verify
// it, but complete framework reads do. Form uploads take no write condition.
type UploadFormOptions struct {
	ExpiresIn        time.Duration
	ContentType      MediaType
	MinSize, MaxSize int64
	Checksum         value.Optional[SHA256]
}

func (o UploadFormOptions) Validate(maximum int64) error {
	if o.ExpiresIn < time.Second || o.ExpiresIn > MaxLinkLifetime || o.MinSize < 0 || o.MaxSize < 1 || o.MinSize > o.MaxSize {
		return Failure(Invalid, SignOperation, NotApplicable, nil)
	}
	if o.MaxSize > maximum {
		return Failure(LimitExceeded, SignOperation, NotApplicable, nil)
	}
	if err := o.ContentType.Validate(); err != nil {
		return Failure(Invalid, SignOperation, NotApplicable, err)
	}
	return nil
}

// FormField is one signed form value. Submit every field unchanged, then the
// file content last in the field named by UploadForm.FileField.
type FormField struct{ Name, Value string }

const (
	maxFormFields     = 32
	maxFormValueBytes = 8 << 10
)

// UploadForm is a signed bearer form upload. Formatting redacts it and implicit
// JSON fails; explicitly return URL, Fields and FileField in an authorized
// response DTO. The object is written by the browser, not by this process:
// confirm it with Stat (and its checksum) before relying on it.
type UploadForm struct {
	address string
	fields  []FormField
	expires time.Time
}

func NewUploadForm(address string, fields []FormField, expires time.Time) (UploadForm, error) {
	if _, err := parseStorageURL(address); err != nil {
		return UploadForm{}, err
	}
	if expires.IsZero() || expires.Year() < 1 || expires.Year() > 9999 || len(fields) == 0 || len(fields) > maxFormFields {
		return UploadForm{}, Failure(Invalid, SignOperation, NotApplicable, nil)
	}
	seen := make(map[string]bool, len(fields))
	for _, field := range fields {
		name := strings.ToLower(field.Name)
		if field.Name == "" || name == uploadFormFile || seen[name] || !headerText(field.Name) || !formText(field.Value) {
			return UploadForm{}, Failure(Invalid, SignOperation, NotApplicable, nil)
		}
		seen[name] = true
	}
	return UploadForm{address: address, fields: slices.Clone(fields), expires: expires.UTC().Round(0)}, nil
}

const uploadFormFile = "file"

func (f UploadForm) URL() string          { return f.address }
func (f UploadForm) Fields() []FormField  { return slices.Clone(f.fields) }
func (UploadForm) FileField() string      { return uploadFormFile }
func (f UploadForm) ExpiresAt() time.Time { return f.expires }
func (UploadForm) String() string         { return "temporary storage upload form" }
func (UploadForm) Format(s fmt.State, _ rune) {
	_, _ = s.Write([]byte("temporary storage upload form"))
}
func (UploadForm) LogValue() slog.Value { return slog.StringValue("temporary storage upload form") }
func (UploadForm) MarshalJSON() ([]byte, error) {
	return nil, Failure(Invalid, SignOperation, NotApplicable, nil)
}
func formText(text string) bool {
	if len(text) > maxFormValueBytes {
		return false
	}
	for _, c := range []byte(text) {
		if c < 0x20 || c > 0x7e {
			return false
		}
	}
	return true
}

// TemporaryURL deliberately redacts formatting and rejects implicit JSON.
// Explicitly call URL when returning a link in an authorized response DTO.
// ExpiresAt is an upper bound: revoked/expired credentials may invalidate it sooner.
type TemporaryURL struct {
	address string
	expires time.Time
}

func NewTemporaryURL(address string, expires time.Time) (TemporaryURL, error) {
	if _, err := parseStorageURL(address); err != nil {
		return TemporaryURL{}, err
	}
	if expires.IsZero() || expires.Year() < 1 || expires.Year() > 9999 {
		return TemporaryURL{}, Failure(Invalid, SignOperation, NotApplicable, nil)
	}
	return TemporaryURL{address: address, expires: expires.UTC().Round(0)}, nil
}
func (u TemporaryURL) URL() string              { return u.address }
func (u TemporaryURL) ExpiresAt() time.Time     { return u.expires }
func (TemporaryURL) String() string             { return "temporary storage URL" }
func (TemporaryURL) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("temporary storage URL")) }
func (TemporaryURL) LogValue() slog.Value       { return slog.StringValue("temporary storage URL") }
func (TemporaryURL) MarshalJSON() ([]byte, error) {
	return nil, Failure(Invalid, SignOperation, NotApplicable, nil)
}

func (d *Disk) PublicURL(ctx context.Context, key ObjectKey) (string, error) {
	if err := d.Validate(); err != nil {
		return "", err
	}
	if err := key.Validate(); err != nil {
		return "", err
	}
	if d.config.Visibility != Public {
		return "", Failure(Forbidden, SignOperation, NotApplicable, nil)
	}
	provider, ok := d.backend.(URLProvider)
	if !ok {
		return "", Failure(Unsupported, SignOperation, NotApplicable, nil)
	}
	op, release, err := d.begin(ctx, SignOperation)
	if err != nil {
		return "", err
	}
	defer release()
	var address string
	err = callback.Isolated("storage public URL", func() error { var err error; address, err = provider.PublicURL(op, key); return err })
	if err == nil {
		_, err = parseStorageURL(address)
	}
	if err = finish(SignOperation, op, err, NotApplicable); err != nil {
		return "", err
	}
	return address, nil
}
func (d *Disk) TemporaryURL(ctx context.Context, key ObjectKey, options LinkOptions) (TemporaryURL, error) {
	if err := d.Validate(); err != nil {
		return TemporaryURL{}, err
	}
	if err := key.Validate(); err != nil {
		return TemporaryURL{}, err
	}
	if err := options.Validate(); err != nil {
		return TemporaryURL{}, err
	}
	if options.Version != "" && !d.capabilities.Versions {
		return TemporaryURL{}, Failure(Unsupported, SignOperation, NotApplicable, nil)
	}
	provider, ok := d.backend.(SignedURLProvider)
	if !ok {
		return TemporaryURL{}, Failure(Unsupported, SignOperation, NotApplicable, nil)
	}
	op, release, err := d.begin(ctx, SignOperation)
	if err != nil {
		return TemporaryURL{}, err
	}
	defer release()
	var result TemporaryURL
	err = callback.Isolated("storage temporary URL", func() error { var err error; result, err = provider.TemporaryURL(op, key, options); return err })
	if err == nil {
		_, err = NewTemporaryURL(result.address, result.expires)
	}
	if err = finish(SignOperation, op, err, NotApplicable); err != nil {
		return TemporaryURL{}, err
	}
	return result, nil
}

// TemporaryUploadURL signs a direct client upload for an application-selected
// key. Authorize the caller first: the link is a bearer write credential until
// it expires. It is available on private disks; Visibility governs reads only.
func (d *Disk) TemporaryUploadURL(ctx context.Context, key ObjectKey, options UploadLinkOptions) (UploadLink, error) {
	if err := d.Validate(); err != nil {
		return UploadLink{}, err
	}
	if err := key.Validate(); err != nil {
		return UploadLink{}, err
	}
	if err := options.Validate(d.config.MaxObjectBytes); err != nil {
		return UploadLink{}, err
	}
	if err := d.capabilities.ValidatePut(PutOptions{ContentType: options.ContentType, Size: value.Set(options.Size), Checksum: options.Checksum, Condition: options.Condition}); err != nil {
		return UploadLink{}, err
	}
	provider, ok := d.backend.(SignedUploadProvider)
	if !ok {
		return UploadLink{}, Failure(Unsupported, SignOperation, NotApplicable, nil)
	}
	op, release, err := d.begin(ctx, SignOperation)
	if err != nil {
		return UploadLink{}, err
	}
	defer release()
	var result UploadLink
	err = callback.Isolated("storage temporary upload URL", func() error {
		var err error
		result, err = provider.TemporaryUploadURL(op, key, options)
		return err
	})
	if err == nil {
		_, err = NewUploadLink(result.address, result.method, result.headers, result.expires)
	}
	if err = finish(SignOperation, op, err, NotApplicable); err != nil {
		return UploadLink{}, err
	}
	return result, nil
}

// TemporaryUploadForm signs a direct browser form upload for an
// application-selected key. Authorize the caller first: the form is a bearer
// write credential until it expires. Prefer TemporaryUploadURL when the client
// can send a PUT request; use a form when an HTML form posts the file.
func (d *Disk) TemporaryUploadForm(ctx context.Context, key ObjectKey, options UploadFormOptions) (UploadForm, error) {
	if err := d.Validate(); err != nil {
		return UploadForm{}, err
	}
	if err := key.Validate(); err != nil {
		return UploadForm{}, err
	}
	if err := options.Validate(d.config.MaxObjectBytes); err != nil {
		return UploadForm{}, err
	}
	provider, ok := d.backend.(SignedFormProvider)
	if !ok {
		return UploadForm{}, Failure(Unsupported, SignOperation, NotApplicable, nil)
	}
	op, release, err := d.begin(ctx, SignOperation)
	if err != nil {
		return UploadForm{}, err
	}
	defer release()
	var result UploadForm
	err = callback.Isolated("storage temporary upload form", func() error {
		var err error
		result, err = provider.TemporaryUploadForm(op, key, options)
		return err
	})
	if err == nil {
		_, err = NewUploadForm(result.address, result.fields, result.expires)
	}
	if err = finish(SignOperation, op, err, NotApplicable); err != nil {
		return UploadForm{}, err
	}
	return result, nil
}
