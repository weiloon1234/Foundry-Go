package httpcapture

import (
	"net/http"
	"slices"
	"strings"
)

// Field is one header, query or form name with its retained values. Capture
// tables store lists of it as JSON.
type Field struct {
	Name   string   `json:"name"`
	Values []string `json:"values"`
}

const (
	headerBudget = 16 << 10
	maxFields    = 128
)

// Clip copies at most limit bytes of text, so a capture never pins a larger
// backing string, and reports whether text was cut.
func Clip(text string, limit int) (string, bool) {
	return strings.Clone(text[:min(len(text), limit)]), len(text) > limit
}

// Headers copies at most 128 canonical names, 128 values and 16 KiB in sorted
// order, so the snapshot and its truncation never depend on map order. Both
// capture directions bound headers with it.
func Headers(header http.Header) ([]Field, bool) {
	// Keep only the first 128 canonical names while sorting. An oversized
	// native header map must not allocate an unbudgeted full key inventory.
	type namePair struct{ canonical, original string }
	names := make([]namePair, 0, min(len(header), maxFields))
	truncated := false
	for original := range header {
		if len(original) > 256 {
			truncated = true
			continue
		}
		name := namePair{http.CanonicalHeaderKey(original), original}
		index, _ := slices.BinarySearchFunc(names, name, func(a, b namePair) int {
			if order := strings.Compare(a.canonical, b.canonical); order != 0 {
				return order
			}
			return strings.Compare(a.original, b.original)
		})
		if len(names) == maxFields {
			truncated = true
			if index == maxFields {
				continue
			}
			names = names[:maxFields-1]
		}
		names = slices.Insert(names, index, name)
	}
	remaining := headerBudget
	fields := make([]Field, 0, len(names))
	entries := 0
	for _, pair := range names {
		name, values := pair.canonical, header[pair.original]
		if len(fields) == maxFields || remaining == 0 {
			truncated = true
			break
		}
		key, cut := Clip(name, min(256, remaining))
		remaining -= len(key)
		truncated = truncated || cut
		field := Field{Name: key}
		for _, raw := range values {
			if entries == maxFields || remaining == 0 {
				truncated = true
				break
			}
			text, cut := Clip(raw, min(4<<10, remaining))
			field.Values = append(field.Values, text)
			entries++
			remaining -= len(text)
			truncated = truncated || cut
		}
		fields = append(fields, field)
	}
	return fields, truncated
}
