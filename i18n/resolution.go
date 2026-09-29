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
// LocaleResolver composes the same steps with typed stored preferences.
func (c *Catalog) Resolve(preferred string, acceptLanguage string) (LocaleID, error) {
	if err := c.Validate(); err != nil {
		return "", err
	}
	if locale, ok := c.locales.MatchTag(preferred); ok {
		return locale, nil
	}
	if locale, ok := c.locales.MatchAcceptLanguage(acceptLanguage); ok {
		return locale, nil
	}
	return c.locales.Default(), nil
}

// MatchTag parses external BCP 47 text and applies Match. Malformed or
// unsupported text reports false rather than an error.
func (s LocaleSet) MatchTag(text string) (LocaleID, bool) {
	if text == "" {
		return "", false
	}
	id, err := ParseLocale(text)
	if err != nil {
		return "", false
	}
	return s.Match(id)
}

// MaxAcceptLanguageBytes and MaxAcceptLanguageCandidates bound header parsing.
const (
	MaxAcceptLanguageBytes      = 8192
	MaxAcceptLanguageCandidates = 64
)

// MatchAcceptLanguage selects the first supported locale (or supported parent)
// from quality-sorted Accept-Language candidates. "*" selects the default.
// Oversized headers, malformed and zero-quality candidates are ignored; ties
// keep header order. It reports false when nothing matches.
func (s LocaleSet) MatchAcceptLanguage(header string) (LocaleID, bool) {
	if header == "" || len(header) > MaxAcceptLanguageBytes || s.Validate() != nil {
		return "", false
	}
	type preference struct {
		tag     string
		quality int
	}
	var buffer [8]preference
	choices := buffer[:0]
	candidates := 0
	for candidate := range strings.SplitSeq(header, ",") {
		if candidates >= MaxAcceptLanguageCandidates {
			break
		}
		candidates++
		tag, parameters, hasParameters := strings.Cut(candidate, ";")
		tag = strings.TrimSpace(tag)
		q := 1000
		if hasParameters {
			if strings.Contains(parameters, ";") {
				continue
			}
			name, value, ok := strings.Cut(strings.TrimSpace(parameters), "=")
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
			return s.defaultLocale, true
		}
		if locale, ok := s.MatchTag(choice.tag); ok {
			return locale, true
		}
	}
	return "", false
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
