package i18n

import (
	"context"
	"slices"
	"strings"
)

type localeContextKey struct{}

// WithLocale records an explicitly supported locale on this context only.
func WithLocale(ctx context.Context, catalog LocaleCatalog, locale LocaleID) (context.Context, error) {
	set, err := SnapshotLocales(ctx, catalog)
	if err != nil {
		return nil, err
	}
	if !set.Contains(locale) {
		return nil, invalidLocale()
	}
	return context.WithValue(ctx, localeContextKey{}, locale), nil
}
func RequestLocale(ctx context.Context) (LocaleID, bool) {
	if ctx == nil {
		return "", false
	}
	locale, ok := ctx.Value(localeContextKey{}).(LocaleID)
	return locale, ok
}

// Resolve prefers an explicitly selected locale, then quality-sorted header
// candidates, then the configured default. Regional/extension tags may fall
// back to a supported parent; unrelated sibling locales are never selected.
// Malformed/zero-quality candidates are ignored. This is locale selection,
// not HTTP 406 negotiation; absence of an acceptable candidate uses the default.
func (c *Catalog) Resolve(preferred string, acceptLanguage string) (LocaleID, error) {
	if err := c.Validate(); err != nil {
		return "", err
	}
	if locale, ok := c.resolveTag(preferred); ok {
		return locale, nil
	}
	if len(acceptLanguage) > 8192 {
		return c.locales.Default(), nil
	}
	type preference struct {
		tag     string
		quality int
	}
	var choices []preference
	candidates := 0
	for candidate := range strings.SplitSeq(acceptLanguage, ",") {
		if candidates >= 64 {
			break
		}
		candidates++
		parts := strings.Split(candidate, ";")
		if len(parts) > 2 {
			continue
		}
		tag := strings.TrimSpace(parts[0])
		q := 1000
		if len(parts) == 2 {
			name, value, ok := strings.Cut(strings.TrimSpace(parts[1]), "=")
			if !ok || !strings.EqualFold(name, "q") {
				continue
			}
			q = parseQuality(value)
		}
		if q > 0 {
			choices = append(choices, preference{tag, q})
		}
	}
	slices.SortStableFunc(choices, func(a, b preference) int { return b.quality - a.quality })
	for _, choice := range choices {
		if choice.tag == "*" {
			return c.locales.Default(), nil
		}
		if locale, ok := c.resolveTag(choice.tag); ok {
			return locale, nil
		}
	}
	return c.locales.Default(), nil
}
func (c *Catalog) resolveTag(text string) (LocaleID, bool) {
	id, err := ParseLocale(text)
	if err != nil {
		return "", false
	}
	for {
		if c.locales.Contains(id) {
			return id, true
		}
		end := strings.LastIndexByte(string(id), '-')
		if end < 0 {
			return "", false
		}
		id = id[:end]
	}
}
func parseQuality(s string) int {
	if s == "1" || s == "1.0" || s == "1.00" || s == "1.000" {
		return 1000
	}
	if s == "0" {
		return 0
	}
	if !strings.HasPrefix(s, "0.") || len(s) < 3 || len(s) > 5 {
		return -1
	}
	value := 0
	for _, b := range []byte(s[2:]) {
		if b < '0' || b > '9' {
			return -1
		}
		value = value*10 + int(b-'0')
	}
	for i := len(s); i < 5; i++ {
		value *= 10
	}
	return value
}
