package validation

import (
	"net/mail"
	"net/netip"
	"net/url"
	"strings"
	"unicode"

	"github.com/weiloon1234/Foundry-Go/internal/jsonwire"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/temporal"
)

// Rule IDs of built-in formats that metadata owners, such as client
// presentation checks, recognize in rule descriptions.
const (
	EmailRuleID RuleID = "foundry.email"
	URLRuleID   RuleID = "foundry.url"
)

func textRule[S ~string](id RuleID, serverOnly bool, check func(string) bool) Rule[S] {
	return valueRule(Spec{ID: id}, serverOnly, func(s *execution, input S) (bool, error) {
		text := string(input)
		return textValue(s, text) && check(text), nil
	})
}

// Email accepts a single bare mailbox using Go's net/mail parser. Display
// names, comments, angle wrappers and surrounding whitespace are rejected.
// It performs no DNS/delivery checks or normalization. Parser semantics remain
// server-only in metadata rather than claiming browser equivalence.
func Email[S ~string]() Rule[S] {
	return textRule[S](EmailRuleID, true, bareMailbox)
}

func bareMailbox(text string) bool {
	quoted, escaped := false, false
	for _, r := range text {
		if unicode.IsControl(r) {
			return false
		}
		if escaped {
			escaped = false
			continue
		}
		if quoted {
			if r == '\\' {
				escaped = true
			} else if r == '"' {
				quoted = false
			}
			continue
		}
		if r == '"' {
			quoted = true
			continue
		}
		if unicode.IsSpace(r) || strings.ContainsRune("<>()", r) {
			return false
		}
	}
	if quoted || escaped {
		return false
	}
	address, err := mail.ParseAddress(text)
	return err == nil && address.Name == ""
}

// URL accepts an absolute HTTP or HTTPS URL with a hostname. It checks syntax,
// not reachability or whether a destination is authorized for outbound fetching.
func URL[S ~string]() Rule[S] {
	return textRule[S](URLRuleID, true, func(text string) bool {
		parsed, err := url.Parse(text)
		return err == nil && (strings.EqualFold(parsed.Scheme, "http") || strings.EqualFold(parsed.Scheme, "https")) && parsed.Hostname() != ""
	})
}

func validIP(text string, version int) bool {
	address, err := netip.ParseAddr(text)
	return err == nil && address.Zone() == "" && (version == 0 || version == 4 && address.Is4() || version == 6 && address.Is6())
}

// IP accepts a literal IPv4 or IPv6 address without a zone or CIDR suffix.
func IP[S ~string]() Rule[S] {
	return textRule[S]("foundry.ip", false, func(text string) bool { return validIP(text, 0) })
}
func IPv4[S ~string]() Rule[S] {
	return textRule[S]("foundry.ipv4", false, func(text string) bool { return validIP(text, 4) })
}
func IPv6[S ~string]() Rule[S] {
	return textRule[S]("foundry.ipv6", false, func(text string) bool { return validIP(text, 6) })
}

type uuidInput struct{}

// UUID reuses the framework's canonical hyphenated UUID parser. It checks
// representation, including the nil UUID; model identity constraints are separate.
func UUID[S ~string]() Rule[S] {
	return textRule[S]("foundry.uuid", false, func(text string) bool { _, err := model.ParseID[uuidInput](text); return err == nil })
}

// Temporal text rules reuse the framework temporal parsers. Prefer concrete
// temporal DTO fields when the transport should decode a temporal value directly.
func Date[S ~string]() Rule[S] {
	return textRule[S]("foundry.date", false, func(text string) bool { _, err := temporal.ParseDate(text); return err == nil })
}
func Time[S ~string]() Rule[S] {
	return textRule[S]("foundry.time", false, func(text string) bool { _, err := temporal.ParseTime(text); return err == nil })
}
func DateTime[S ~string]() Rule[S] {
	return textRule[S]("foundry.datetime", false, func(text string) bool { _, err := temporal.ParseDateTime(text); return err == nil })
}
func LocalDateTime[S ~string]() Rule[S] {
	return textRule[S]("foundry.local_datetime", false, func(text string) bool { _, err := temporal.ParseLocalDateTime(text); return err == nil })
}

// JSON validates bounded JSON text through the same strict parser as DTOs.
// Duplicate keys are rejected; the original string remains unchanged.
func JSON[S ~string]() Rule[S] {
	return valueRule(Spec{ID: "foundry.json"}, true, func(s *execution, input S) (bool, error) {
		text := string(input)
		if !textValue(s, text) {
			return false, nil
		}
		_, err := jsonwire.Decode([]byte(text), jsonwire.Limits{Bytes: s.limits.ValueBytes, Depth: jsonwire.MaxDepth, Nodes: jsonwire.MaxNodes})
		return err == nil, nil
	})
}
