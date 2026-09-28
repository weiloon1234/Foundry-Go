package storage

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/weiloon1234/Foundry-Go/internal/callback"
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
	parsed.Path = strings.TrimRight(parsed.Path, "/") + "/" + key.String()
	parsed.RawPath = ""
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

type LinkOptions struct {
	ExpiresIn time.Duration
	Version   VersionID
}

func (o LinkOptions) Validate() error {
	if o.ExpiresIn < time.Second || o.ExpiresIn > 7*24*time.Hour {
		return Failure(Invalid, SignOperation, NotApplicable, nil)
	}
	return o.Version.Validate()
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
