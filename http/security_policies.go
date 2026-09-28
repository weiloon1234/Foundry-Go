package http

import (
	"strconv"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
)

// FramePolicy selects the supported X-Frame-Options policy. Zero omits the
// header; use the default configuration to select FrameDeny explicitly.
type FramePolicy string

const (
	FrameDeny       FramePolicy = "DENY"
	FrameSameOrigin FramePolicy = "SAMEORIGIN"
)

func (p FramePolicy) Validate() error {
	switch p {
	case "", FrameDeny, FrameSameOrigin:
		return nil
	}
	return fault.New(fault.Invalid, "unsupported frame policy")
}

// ReferrerPolicy selects a single standard Referrer-Policy token. Zero omits
// the header. The type remains distinct from frame and custom-header values.
type ReferrerPolicy string

const (
	ReferrerNoReferrer                  ReferrerPolicy = "no-referrer"
	ReferrerNoReferrerWhenDowngrade     ReferrerPolicy = "no-referrer-when-downgrade"
	ReferrerSameOrigin                  ReferrerPolicy = "same-origin"
	ReferrerOrigin                      ReferrerPolicy = "origin"
	ReferrerStrictOrigin                ReferrerPolicy = "strict-origin"
	ReferrerOriginWhenCrossOrigin       ReferrerPolicy = "origin-when-cross-origin"
	ReferrerStrictOriginWhenCrossOrigin ReferrerPolicy = "strict-origin-when-cross-origin"
	ReferrerUnsafeURL                   ReferrerPolicy = "unsafe-url"
)

func (p ReferrerPolicy) Validate() error {
	switch p {
	case "", ReferrerNoReferrer, ReferrerNoReferrerWhenDowngrade, ReferrerSameOrigin, ReferrerOrigin, ReferrerStrictOrigin, ReferrerOriginWhenCrossOrigin, ReferrerStrictOriginWhenCrossOrigin, ReferrerUnsafeURL:
		return nil
	}
	return fault.New(fault.Invalid, "unsupported referrer policy")
}

// HSTSPolicy is an explicit Strict-Transport-Security declaration. MaxAge must
// be a nonnegative whole number of seconds; zero asks browsers to remove a
// remembered policy. IncludeSubDomains and Preload are explicit choices.
// Preload only emits the directive; it does not enroll a domain in any list.
type HSTSPolicy struct {
	MaxAge            time.Duration
	IncludeSubDomains bool
	Preload           bool
}

func (p HSTSPolicy) Validate() error {
	if p.MaxAge < 0 || p.MaxAge%time.Second != 0 {
		return fault.New(fault.Invalid, "HSTS max age must be a nonnegative whole number of seconds")
	}
	if p.Preload && (!p.IncludeSubDomains || p.MaxAge < 365*24*time.Hour) {
		return fault.New(fault.Invalid, "HSTS preload requires subdomains and at least one year of max age")
	}
	return nil
}

func (p HSTSPolicy) value() HeaderValue {
	text := "max-age=" + strconv.FormatInt(int64(p.MaxAge/time.Second), 10)
	if p.IncludeSubDomains {
		text += "; includeSubDomains"
	}
	if p.Preload {
		text += "; preload"
	}
	return HeaderValue(text)
}
