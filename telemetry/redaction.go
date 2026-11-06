package telemetry

import (
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/weiloon1234/Foundry-Go/http"
)

func validIdentity(text string) bool {
	if len(text) == 0 || len(text) > maxIdentityBytes || strings.Contains(text, "..") {
		return false
	}
	for i, c := range []byte(text) {
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' {
			continue
		}
		if i > 0 && (c == '.' || c == '_' || c == '-') {
			continue
		}
		return false
	}
	return true
}

func validDigest(text string) bool {
	if len(text) != sha256.Size*2 {
		return false
	}
	for _, c := range text {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

func digest(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

func fingerprint(issue observedIssue) Fingerprint {
	return Fingerprint(digest(string(issue.Source) + "|" + issue.Code + "|" + string(issue.Route) + "|" + string(issue.Release)))
}

func safeIdentifier(text string, limit int) string {
	// Bound work before inspecting text even for directly supplied reports.
	text = text[:min(len(text), limit)]
	return strings.Clone(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || r == utf8.RuneError {
			return -1
		}
		return r
	}, text))
}

func classifySecurityPath(raw string) (string, bool) {
	// The HTTP owner already bounds this input. Keep direct callers bounded too.
	raw = raw[:min(len(raw), http.MaxSecurityPathBytes)]
	decoded, err := url.PathUnescape(raw)
	if err != nil {
		return "malformed_path", true
	}
	lower := strings.ToLower(decoded)
	switch {
	case strings.Contains(lower, ".env"):
		return "environment_probe", true
	case strings.Contains(lower, "/.git"):
		return "repository_probe", true
	case strings.Contains(lower, "../") || strings.Contains(lower, "..\\"):
		return "traversal_probe", true
	case strings.Contains(lower, "wp-admin") || strings.Contains(lower, "wp-login") || strings.Contains(lower, "phpmyadmin"):
		return "platform_probe", true
	}
	return "", false
}
