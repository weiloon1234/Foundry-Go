package auth

import (
	"context"
	"net/netip"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/weiloon1234/Foundry-Go/attribution"
	"github.com/weiloon1234/Foundry-Go/fault"
)

// MaxDeviceUserAgentBytes bounds the user agent stored with a credential.
const MaxDeviceUserAgentBytes = 512

// Device is display metadata about the client that obtained a session or
// token, for "your devices" listings. It is captured from the trusted request
// attribution at issuance and is never used to authenticate or authorize.
type Device struct {
	ClientIP  netip.Addr
	UserAgent string
}

// DeviceFrom captures the current request's trusted client IP and user agent.
// The user agent is truncated on a rune boundary to MaxDeviceUserAgentBytes.
// Contexts without request attribution return a zero Device.
func DeviceFrom(ctx context.Context) Device {
	if ctx == nil {
		return Device{}
	}
	request := attribution.FromContext(ctx).Request()
	agent := request.UserAgent
	if len(agent) > MaxDeviceUserAgentBytes {
		cut := MaxDeviceUserAgentBytes
		for cut > 0 && !utf8.RuneStart(agent[cut]) {
			cut--
		}
		agent = agent[:cut]
	}
	return Device{ClientIP: request.IP.Unmap().WithZone(""), UserAgent: strings.Clone(agent)}
}

func (d Device) Validate() error {
	if d.ClientIP.Zone() != "" || len(d.UserAgent) > MaxDeviceUserAgentBytes || !utf8.ValidString(d.UserAgent) || strings.IndexFunc(d.UserAgent, unicode.IsControl) >= 0 {
		return fault.New(fault.Invalid, "invalid credential device metadata")
	}
	return nil
}
