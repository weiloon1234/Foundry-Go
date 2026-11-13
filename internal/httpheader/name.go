// Package httpheader owns shared HTTP field grammar and static header ownership.
package httpheader

import (
	"net/textproto"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/httptoken"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

const MaxNameBytes = 256

// ValidateName checks the bounded ASCII HTTP field-name grammar.
func ValidateName(name string) error {
	if len(name) == 0 || len(name) > MaxNameBytes {
		return safeerror.NewFault(fault.Invalid, "HTTP header name is empty or too long")
	}
	if !httptoken.Valid(name) {
		return safeerror.NewFault(fault.Invalid, "HTTP header name contains an invalid byte")
	}
	return nil
}

// CanonicalName validates before applying Go's conventional header casing.
func CanonicalName(name string) (string, error) {
	if err := ValidateName(name); err != nil {
		return "", err
	}
	return textproto.CanonicalMIMEHeaderKey(name), nil
}
