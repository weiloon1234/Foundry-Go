// Package i18n owns shared locale identities, immutable UI catalogs, request
// locale resolution and bounded plain-text formatting. Model content uses the
// same locale snapshots with its own separate fallback policy.
package i18n

import (
	"context"
	"reflect"
	"slices"
	"strings"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"golang.org/x/text/language"
)

type LocaleID string

const MaxLocales = 64

// ParseLocale normalizes a BCP 47 tag. It does not grant membership in a
// supported-locale catalog. Operations require both validity and membership.
func ParseLocale(text string) (LocaleID, error) {
	if len(text) == 0 || len(text) > 128 {
		return "", invalidLocale()
	}
	tag, err := language.Parse(text)
	if err != nil {
		return "", invalidLocale()
	}
	return LocaleID(tag.String()), nil
}
func (id LocaleID) Validate() error {
	canonical, err := ParseLocale(string(id))
	if err != nil || canonical != id {
		return invalidLocale()
	}
	return nil
}

// LocaleSet is an immutable snapshot. Its zero value is invalid. It also
// implements LocaleCatalog for applications with a static configured set.
type LocaleSet struct {
	ids           []LocaleID
	defaultLocale LocaleID
}

func NewLocaleSet(defaultLocale LocaleID, locales ...LocaleID) (LocaleSet, error) {
	if len(locales) == 0 || len(locales) > MaxLocales || defaultLocale.Validate() != nil {
		return LocaleSet{}, invalidLocale()
	}
	ids := slices.Clone(locales)
	slices.Sort(ids)
	for i, id := range ids {
		if id.Validate() != nil || i > 0 && ids[i-1] == id {
			return LocaleSet{}, invalidLocale()
		}
	}
	if _, ok := slices.BinarySearch(ids, defaultLocale); !ok {
		return LocaleSet{}, invalidLocale()
	}
	return LocaleSet{ids: ids, defaultLocale: defaultLocale}, nil
}
func (s LocaleSet) Validate() error {
	if len(s.ids) == 0 || len(s.ids) > MaxLocales {
		return invalidLocale()
	}
	if _, ok := slices.BinarySearch(s.ids, s.defaultLocale); !ok {
		return invalidLocale()
	}
	return nil
}
func (s LocaleSet) Default() LocaleID         { return s.defaultLocale }
func (s LocaleSet) Locales() []LocaleID       { return slices.Clone(s.ids) }
func (s LocaleSet) Contains(id LocaleID) bool { _, ok := slices.BinarySearch(s.ids, id); return ok }

// Match returns id when it is supported, otherwise its nearest supported
// parent tag (en-GB → en, zh-Hant-TW → zh-Hant). A language-script tag such as
// zh-Hant or sr-Latn has no parent, so fallback never switches writing system.
// Unrelated siblings are never selected. This is the single parent-locale rule
// shared by request resolution, UI catalog lookup and model-content fallback.
func (s LocaleSet) Match(id LocaleID) (LocaleID, bool) {
	for {
		if s.Contains(id) {
			return id, true
		}
		parent, ok := parentLocale(id)
		if !ok {
			return "", false
		}
		id = parent
	}
}

// parentLocale removes the final BCP 47 subtag. Intermediate truncations that
// are not canonical locale IDs simply never match a validated LocaleSet. As in
// CLDR, whose parent of a language-script locale is the root, a script subtag
// directly after the language ends the chain: zh-Hant does not fall back to zh,
// nor sr-Latn to sr, which may be written in another script.
func parentLocale(id LocaleID) (LocaleID, bool) {
	end := strings.LastIndexByte(string(id), '-')
	if end <= 0 || strings.IndexByte(string(id[:end]), '-') < 0 && scriptSubtag(string(id[end+1:])) {
		return "", false
	}
	return id[:end], true
}

// scriptSubtag reports an ISO 15924 script subtag: exactly four ASCII letters.
func scriptSubtag(subtag string) bool {
	if len(subtag) != 4 {
		return false
	}
	for i := range len(subtag) {
		if c := subtag[i] | 0x20; c < 'a' || c > 'z' {
			return false
		}
	}
	return true
}

// appendChain appends requested, its supported parents, then final, skipping
// duplicates. Chains are short (bounded by subtags), so a linear scan is used.
func (s LocaleSet) appendChain(result []LocaleID, requested, final LocaleID) []LocaleID {
	result = append(result, requested)
	for parent, ok := parentLocale(requested); ok; parent, ok = parentLocale(parent) {
		if s.Contains(parent) && !slices.Contains(result, parent) {
			result = append(result, parent)
		}
	}
	if !slices.Contains(result, final) {
		result = append(result, final)
	}
	return result
}

// Fallbacks returns requested, its supported regional parents (en-GB → en,
// never across a script subtag), the default, then remaining supported locales
// in lexical order, without duplicates. Unsupported requested IDs fail.
func (s LocaleSet) Fallbacks(requested LocaleID) ([]LocaleID, error) {
	if err := s.Validate(); err != nil {
		return nil, err
	}
	if !s.Contains(requested) {
		return nil, invalidLocale()
	}
	result := s.appendChain(make([]LocaleID, 0, len(s.ids)), requested, s.defaultLocale)
	for _, id := range s.ids {
		if !slices.Contains(result, id) {
			result = append(result, id)
		}
	}
	return result, nil
}
func (s LocaleSet) Snapshot(ctx context.Context) (LocaleSet, error) {
	if ctx == nil {
		return LocaleSet{}, invalidLocale()
	}
	if err := ctx.Err(); err != nil {
		return LocaleSet{}, err
	}
	if err := s.Validate(); err != nil {
		return LocaleSet{}, err
	}
	return s, nil
}

// LocaleCatalog returns one consistent supported/default snapshot per operation.
// Implementations are borrowed, concurrent-safe and context-aware. Snapshot must
// not resolve a process-global or goroutine-global request locale.
type LocaleCatalog interface {
	Snapshot(context.Context) (LocaleSet, error)
}

func ValidateLocaleCatalog(c LocaleCatalog) error {
	if c == nil {
		return invalidLocale()
	}
	v := reflect.ValueOf(c)
	switch v.Kind() {
	case reflect.Pointer, reflect.Map, reflect.Func, reflect.Chan, reflect.Interface, reflect.Slice:
		if v.IsNil() {
			return invalidLocale()
		}
	}
	return nil
}

// SnapshotLocales isolates extension panics/Goexit and waits for actual return.
// Managers call it inside their own bounded operation lifetime. The framework's
// immutable LocaleSet and *Catalog run no application code, so they are read
// directly without an isolation goroutine.
func SnapshotLocales(ctx context.Context, c LocaleCatalog) (LocaleSet, error) {
	if ctx == nil {
		return LocaleSet{}, invalidLocale()
	}
	if err := ctx.Err(); err != nil {
		return LocaleSet{}, err
	}
	if err := ValidateLocaleCatalog(c); err != nil {
		return LocaleSet{}, err
	}
	switch framework := c.(type) {
	case LocaleSet:
		return framework.Snapshot(ctx)
	case *Catalog:
		return framework.Snapshot(ctx)
	}
	var result LocaleSet
	err := callback.Isolated("locale catalog", func() error {
		var err error
		result, err = c.Snapshot(ctx)
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		return result.Validate()
	})
	if err != nil {
		return LocaleSet{}, err
	}
	return result, nil
}
func invalidLocale() error { return fault.New(fault.Invalid, "invalid locale or locale catalog") }
