package tracing

import (
	"strings"

	"github.com/weiloon1234/Foundry-Go/fault"
)

func stateKeyPart(text string, maximum int, digitFirst bool) bool {
	if len(text) == 0 || len(text) > maximum {
		return false
	}
	lower := func(c byte) bool { return c >= 'a' && c <= 'z' }
	digit := func(c byte) bool { return c >= '0' && c <= '9' }
	if !lower(text[0]) && !(digitFirst && digit(text[0])) {
		return false
	}
	for i := 1; i < len(text); i++ {
		c := text[i]
		if !lower(c) && !digit(c) && c != '_' && c != '-' && c != '*' && c != '/' {
			return false
		}
	}
	return true
}

func validStateKey(key string) bool {
	if tenant, system, multi := strings.Cut(key, "@"); multi {
		return stateKeyPart(tenant, 241, true) && stateKeyPart(system, 14, false)
	}
	return stateKeyPart(key, 256, false)
}

func normalizeState(text string) (string, error) {
	invalid := func() (string, error) { return "", fault.New(fault.Invalid, "invalid or oversized tracestate") }
	if len(text) > MaxStateBytes {
		return invalid()
	}
	members := strings.Split(text, ",")
	if len(members) > 32 {
		return invalid()
	}
	seen := make(map[string]bool, len(members))
	result := make([]string, 0, len(members))
	for _, member := range members {
		member = strings.Trim(member, " \t")
		if member == "" {
			continue
		}
		key, value, ok := strings.Cut(member, "=")
		if !ok || !validStateKey(key) || seen[key] || len(value) == 0 || len(value) > 256 {
			return invalid()
		}
		for i := range len(value) {
			if value[i] < 0x20 || value[i] > 0x7e || value[i] == ',' || value[i] == '=' {
				return invalid()
			}
		}
		if value[len(value)-1] == ' ' {
			return invalid()
		}
		seen[key] = true
		result = append(result, key+"="+value)
	}
	return strings.Join(result, ","), nil
}
