package mfa

import (
	"crypto/hmac"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/binary"
	"strconv"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/secret"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

// matchTOTP is private: a match is not authentication until its selected step
// has been persisted atomically with the protected operation. Callers supply
// the server clock and last accepted step from the locked factor record.
func matchTOTP(key TOTPSecret, code TOTPCode, now temporal.DateTime, window int, last value.Optional[int64]) (value.Optional[int64], error) {
	var absent value.Optional[int64]
	if err := key.Validate(); err != nil {
		return absent, err
	}
	if err := code.Validate(); err != nil {
		return absent, err
	}
	previous, used := last.Get()
	if window < 0 || window > 1 || now.UTC().Unix() < 0 || used && previous < 0 {
		return absent, fault.New(fault.Invalid, "invalid TOTP verification state")
	}
	raw, _ := totpEncoding.DecodeString(key.Secret().Reveal())
	step := now.UTC().Unix() / totpPeriodSeconds
	var result value.Optional[int64]
	// Examine every permitted step. Picking the highest match prevents a rare
	// truncated-code collision from accepting an earlier step on the next call.
	for candidate := step - int64(window); candidate <= step+int64(window); candidate++ {
		if candidate < 0 {
			continue
		}
		expected := totpAt(raw, uint64(candidate))
		equal := subtle.ConstantTimeCompare([]byte(expected.Secret().Reveal()), []byte(code.Secret().Reveal()))
		if equal == 1 && (!used || candidate > previous) {
			result = value.Set(candidate)
		}
	}
	return result, nil
}
func totpAt(raw []byte, counter uint64) TOTPCode {
	var message [8]byte
	binary.BigEndian.PutUint64(message[:], counter)
	// SHA-1 here is the keyed HOTP construction specified by RFC 6238/4226,
	// never a password hash or an unkeyed integrity check.
	mac := hmac.New(sha1.New, raw)
	_, _ = mac.Write(message[:])
	digest := mac.Sum(nil)
	offset := digest[len(digest)-1] & 15
	number := binary.BigEndian.Uint32(digest[offset:offset+4]) & 0x7fffffff
	text := strconv.FormatUint(uint64(number%1_000_000), 10)
	for len(text) < totpDigits {
		text = "0" + text
	}
	return TOTPCode{digits: secret.New(text)}
}
