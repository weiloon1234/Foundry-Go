package http

import (
	"strings"
)

const maxAcceptEncodingBytes = 8 << 10
const maxAcceptEncodingLines = 16
const maxAcceptEncodingSlots = 64

type compressionPreferences struct {
	weights map[string]int
	present bool
}

// Parse bounded HTTP list syntax and integer thousandths; no float rounding or
// application-supplied error text participates in negotiation.
func parseAcceptEncoding(lines []string) (compressionPreferences, error) {
	p := compressionPreferences{weights: make(map[string]int), present: len(lines) != 0}
	if len(lines) > maxAcceptEncodingLines {
		return p, BadRequest
	}
	bytes, slots := 0, 0
	for _, line := range lines {
		bytes += len(line)
		if bytes > maxAcceptEncodingBytes {
			return p, BadRequest
		}
		for i := range len(line) {
			if line[i] < 32 && line[i] != '\t' || line[i] == 127 {
				return p, BadRequest
			}
		}
		for _, part := range strings.Split(line, ",") {
			slots++
			if slots > maxAcceptEncodingSlots {
				return p, BadRequest
			}
			part = strings.Trim(part, " \t")
			if part == "" {
				continue
			}
			name, weight, hasWeight := strings.Cut(part, ";")
			name = strings.ToLower(strings.Trim(name, " \t"))
			if err := HeaderName(name).Validate(); err != nil {
				return p, BadRequest
			}
			if _, exists := p.weights[name]; exists {
				return p, BadRequest
			}
			q := 1000
			if hasWeight {
				key, text, ok := strings.Cut(strings.Trim(weight, " \t"), "=")
				if !ok || !strings.EqualFold(strings.Trim(key, " \t"), "q") {
					return p, BadRequest
				}
				var valid bool
				q, valid = parseCompressionQuality(strings.Trim(text, " \t"))
				if !valid {
					return p, BadRequest
				}
			}
			p.weights[name] = q
		}
	}
	return p, nil
}
func parseCompressionQuality(text string) (int, bool) {
	if text == "0" {
		return 0, true
	}
	if text == "1" {
		return 1000, true
	}
	if len(text) < 2 || len(text) > 5 || (text[0] != '0' && text[0] != '1') || text[1] != '.' {
		return 0, false
	}
	n, multiplier := 0, 100
	for i := 2; i < len(text); i++ {
		if text[i] < '0' || text[i] > '9' || text[0] == '1' && text[i] != '0' {
			return 0, false
		}
		n += int(text[i]-'0') * multiplier
		multiplier /= 10
	}
	if text[0] == '1' {
		return 1000, true
	}
	return n, true
}
func (p compressionPreferences) quality(encoding string) int {
	if q, ok := p.weights[encoding]; ok {
		return q
	}
	if encoding == "identity" {
		if q, ok := p.weights["*"]; ok && q == 0 {
			return 0
		}
		return 1000
	}
	if q, ok := p.weights["*"]; ok {
		return q
	}
	if !p.present {
		return 1000
	}
	return 0
}
func (p compressionPreferences) choose(encoders []CompressionEncoder) (CompressionEncoder, bool) {
	// An absent/empty field deliberately selects identity; RFC 9110 permits
	// identity without guessing a client's coding preference.
	if !p.present || len(p.weights) == 0 {
		return CompressionEncoder{}, false
	}
	best := 0
	var selected CompressionEncoder
	for _, e := range encoders {
		q := p.quality(string(e.encoding))
		if q > best {
			best = q
			selected = e
		}
	}
	// Explicit identity preference is respected; otherwise identity remains an
	// acceptable fallback and does not suppress a listed compression preference.
	if identity, explicit := p.weights["identity"]; explicit && identity > best {
		return CompressionEncoder{}, false
	}
	return selected, best > 0
}
