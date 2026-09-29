package logging

import (
	"path/filepath"
	"strings"

	"github.com/weiloon1234/Foundry-Go/fault"
)

// SyslogFacility selects the syslog facility for a syslog sink.
type SyslogFacility string

const (
	FacilityUser   SyslogFacility = "user"
	FacilityDaemon SyslogFacility = "daemon"
	FacilityLocal0 SyslogFacility = "local0"
	FacilityLocal1 SyslogFacility = "local1"
	FacilityLocal2 SyslogFacility = "local2"
	FacilityLocal3 SyslogFacility = "local3"
	FacilityLocal4 SyslogFacility = "local4"
	FacilityLocal5 SyslogFacility = "local5"
	FacilityLocal6 SyslogFacility = "local6"
	FacilityLocal7 SyslogFacility = "local7"
)

// SyslogConfig selects the daemon for the syslog driver. Network and Address
// are both empty for the platform's local daemon, or select "udp"/"tcp" with a
// host:port or "unix"/"unixgram" with an absolute socket path. Tag defaults to
// the process name; Facility defaults to user. Record severity follows the
// record level: debug, info, warning or err.
type SyslogConfig struct {
	Network  string
	Address  string
	Tag      string
	Facility SyslogFacility
}

func (c SyslogConfig) Validate() error {
	switch c.Network {
	case "":
		if c.Address != "" {
			return fault.New(fault.Invalid, "syslog address requires a network")
		}
	case "udp", "tcp":
		if c.Address == "" || len(c.Address) > 256 || strings.IndexFunc(c.Address, invalidSyslogRune) >= 0 {
			return fault.New(fault.Invalid, "syslog network address must be a bounded host:port")
		}
	case "unix", "unixgram":
		if !filepath.IsAbs(c.Address) || len(c.Address) > 1024 || strings.ContainsRune(c.Address, 0) {
			return fault.New(fault.Invalid, "syslog socket requires an absolute path")
		}
	default:
		return fault.New(fault.Invalid, "unsupported syslog network")
	}
	if len(c.Tag) > 48 || strings.IndexFunc(c.Tag, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-' || r == '.')
	}) >= 0 {
		return fault.New(fault.Invalid, "syslog tag must be at most 48 letters, digits, '.', '_' or '-'")
	}
	switch c.Facility {
	case "", FacilityUser, FacilityDaemon, FacilityLocal0, FacilityLocal1, FacilityLocal2, FacilityLocal3, FacilityLocal4, FacilityLocal5, FacilityLocal6, FacilityLocal7:
		return nil
	default:
		return fault.New(fault.Invalid, "unsupported syslog facility")
	}
}

func invalidSyslogRune(r rune) bool { return r <= ' ' || r == 0x7f }
