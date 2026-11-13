package httpheader

import (
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

const MaxValueBytes = 8192

// ValidateValue preserves native header values, rejecting controls except HTAB.
func ValidateValue(value string) error {
	if len(value) > MaxValueBytes {
		return safeerror.NewFault(fault.Invalid, "HTTP header value exceeds its byte bound")
	}
	for i := range len(value) {
		if value[i] != '\t' && (value[i] < 0x20 || value[i] == 0x7f) {
			return safeerror.NewFault(fault.Invalid, "HTTP header value contains an invalid control byte")
		}
	}
	return nil
}

// ValidateRequestValue restricts typed request values to portable ASCII and HTAB.
// Framing owns outer whitespace normalization; scalar codecs own field grammar.
func ValidateRequestValue(value string) error {
	if err := ValidateValue(value); err != nil {
		return err
	}
	for i := range len(value) {
		if value[i] > 0x7f {
			return safeerror.NewFault(fault.Invalid, "HTTP request header value requires ASCII")
		}
	}
	return nil
}
