package embeddedonly

import (
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

// Code tests promoted Format/Parse discovery independently of DefineScalar:
// codeCodec declares Parse and inherits Format from an embedded formatter.
type Code string

type codeFormatter struct{}

func (codeFormatter) Format(v Code) (string, error) { return string(v), nil }

type codeCodec struct{ codeFormatter }

func (codeCodec) Parse(text string) (Code, error) {
	if text == "" {
		return "", safeerror.NewFault(fault.Invalid, "empty value")
	}
	return Code(text), nil
}
