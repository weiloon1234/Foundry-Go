// Package i18n owns shared locale identities, immutable UI catalogs, request
// locale resolution and bounded plain-text formatting. Model content uses the
// same locale snapshots with its own separate fallback policy.
package i18n

import (
	"context"
	"reflect"
	"slices"

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

// Fallbacks returns requested, default, then remaining supported locales in
// lexical order, without duplicates. Unsupported requested IDs fail.
func (s LocaleSet) Fallbacks(requested LocaleID) ([]LocaleID, error) {
	if err := s.Validate(); err != nil {
		return nil, err
	}
	if !s.Contains(requested) {
		return nil, invalidLocale()
	}
	result := make([]LocaleID, 0, len(s.ids))
	result = append(result, requested)
	if s.defaultLocale != requested {
		result = append(result, s.defaultLocale)
	}
	for _, id := range s.ids {
		if id != requested && id != s.defaultLocale {
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
// Managers call it inside their own bounded operation lifetime.
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
