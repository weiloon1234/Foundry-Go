//go:build windows || plan9

package logging

import (
	"log/slog"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

const syslogSupported = false

type syslogConnection struct{}

func openSyslog(SyslogConfig) (*syslogConnection, error) {
	return nil, safeerror.NewFault(fault.Invalid, "syslog logging is unsupported on this platform")
}
func (*syslogConnection) writeRecord([]byte, slog.Level) (int, error) {
	return 0, safeerror.NewFault(fault.Invalid, "syslog logging is unsupported on this platform")
}
func (*syslogConnection) Close() error { return nil }
