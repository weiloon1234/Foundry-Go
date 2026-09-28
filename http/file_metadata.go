package http

import (
	"mime"
	"strings"

	"github.com/weiloon1234/Foundry-Go/fault"
)

// MediaType is a concrete response media type, optionally with parameters.
// Wildcards belong to negotiation and are not a representation's media type.
type MediaType string

func (m MediaType) Validate() error {
	if err := HeaderValue(m).Validate(); err != nil {
		return err
	}
	media, _, err := mime.ParseMediaType(string(m))
	major, minor, slash := strings.Cut(media, "/")
	if err != nil || !slash || major == "" || minor == "" || strings.ContainsAny(media, "*") || strings.Contains(minor, "/") {
		return fault.New(fault.Invalid, "invalid response media type")
	}
	return nil
}

// EntityTag is an HTTP representation validator. Zero omits the validator;
// nonzero values use the quoted strong or W/quoted weak wire representation.
// A tag is supplied by the representation owner, never guessed from its name.
type EntityTag string

func (tag EntityTag) Validate() error {
	if tag == "" {
		return nil
	}
	if err := HeaderValue(tag).Validate(); err != nil {
		return err
	}
	text := strings.TrimPrefix(string(tag), "W/")
	if len(text) < 2 || text[0] != '"' || text[len(text)-1] != '"' {
		return fault.New(fault.Invalid, "invalid entity tag")
	}
	for _, b := range []byte(text[1 : len(text)-1]) {
		if b != 0x21 && !(b >= 0x23 && b <= 0x7e) && b < 0x80 {
			return fault.New(fault.Invalid, "invalid entity tag")
		}
	}
	return nil
}
