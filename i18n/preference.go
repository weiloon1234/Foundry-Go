package i18n

import (
	"context"
	"slices"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/identifier"
)

// PreferenceSource names the step that selected a locale, for Vary/cache policy
// and diagnostics. ContextSource, AcceptLanguageSource and DefaultSource are
// reserved; Preferred steps use application names such as "user" or "tenant".
type PreferenceSource string

const (
	ContextSource        PreferenceSource = "context"
	AcceptLanguageSource PreferenceSource = "accept_language"
	DefaultSource        PreferenceSource = "default"
	MaxPreferences                        = 16
)

// Preference is one ordered locale-selection step for a typed subject, such as
// an HTTP request, an authenticated user or a notification recipient. Its zero
// value is invalid. Construct steps with ContextLocale, Preferred or
// AcceptLanguage; each step either selects a supported locale or defers.
type Preference[S any] struct {
	source PreferenceSource
	match  func(context.Context, LocaleSet, S) (LocaleID, preferenceOutcome, error)
}

// preferenceOutcome is one step's result: select a locale, defer to the next
// step, or defer because the stored value was malformed.
type preferenceOutcome uint8

const (
	deferred preferenceOutcome = iota
	selected
	malformed
)

func matched(locale LocaleID, ok bool) (LocaleID, preferenceOutcome, error) {
	if !ok {
		return "", deferred, nil
	}
	return locale, selected, nil
}

// ContextLocale selects the locale already recorded by WithLocale on the
// operation context, for example by an earlier middleware or explicit caller.
func ContextLocale[S any]() Preference[S] {
	return Preference[S]{source: ContextSource, match: func(ctx context.Context, set LocaleSet, _ S) (LocaleID, preferenceOutcome, error) {
		locale, ok := RequestLocale(ctx)
		if !ok {
			return "", deferred, nil
		}
		return matched(set.Match(locale))
	}}
}

// Preferred adapts an explicit or stored preference, such as a user's profile
// locale. The lookup returns false when the subject has no preference. A stored
// locale that is no longer supported falls back to its nearest supported parent
// or defers to the next step. A malformed stored value, such as en_US, is no
// preference: the step defers and Resolution.Ignored names it, so one bad
// profile value never breaks every request of that user. Lookup errors stop
// resolution and never select the default. The lookup is application code:
// panics are contained without a goroutine, it must honor ctx and be safe for
// concurrent use.
func Preferred[S any](source PreferenceSource, lookup func(context.Context, S) (LocaleID, bool, error)) Preference[S] {
	if lookup == nil || !identifier.Semantic(string(source)) || reservedSource(source) {
		return Preference[S]{}
	}
	operation := "locale preference " + string(source)
	return Preference[S]{source: source, match: func(ctx context.Context, set LocaleSet, subject S) (LocaleID, preferenceOutcome, error) {
		var locale LocaleID
		var found bool
		err := callback.Invoke(operation, func() error {
			var err error
			locale, found, err = lookup(ctx, subject)
			return err
		})
		if err != nil || !found {
			return "", deferred, err
		}
		if locale.Validate() != nil {
			return "", malformed, nil
		}
		return matched(set.Match(locale))
	}}
}

// AcceptLanguage adapts a raw Accept-Language header value supplied by the
// transport, using the same quality ordering, bounds and parent matching as
// LocaleSet.MatchAcceptLanguage. An absent or unmatched header defers.
func AcceptLanguage[S any](header func(S) string) Preference[S] {
	if header == nil {
		return Preference[S]{}
	}
	return Preference[S]{source: AcceptLanguageSource, match: func(_ context.Context, set LocaleSet, subject S) (LocaleID, preferenceOutcome, error) {
		var value string
		if err := callback.Invoke("accept-language preference", func() error { value = header(subject); return nil }); err != nil {
			return "", deferred, err
		}
		return matched(set.MatchAcceptLanguage(value))
	}}
}

func (p Preference[S]) Source() PreferenceSource { return p.source }
func (p Preference[S]) Validate() error {
	if p.match == nil || p.source == "" {
		return fault.New(fault.Invalid, "invalid locale preference")
	}
	return nil
}
func reservedSource(source PreferenceSource) bool {
	return source == ContextSource || source == AcceptLanguageSource || source == DefaultSource
}

// Resolution is the selected supported locale and the step that selected it.
// Ignored names the first Preferred step whose stored value was malformed and
// skipped; it is a diagnostic for repairing that value and grants nothing.
type Resolution struct {
	Locale  LocaleID
	Source  PreferenceSource
	Ignored PreferenceSource
}

// LocaleResolver applies ordered preferences, then the catalog default, against
// one supported-locale snapshot per call. It is immutable, creates no goroutines
// for framework catalogs and is safe to share across requests and workers.
// HTTP typically uses ContextLocale, a stored user preference and AcceptLanguage;
// mail and notifications use a recipient preference without a request header.
type LocaleResolver[S any] struct {
	locales LocaleCatalog
	steps   []Preference[S]
}

func NewLocaleResolver[S any](locales LocaleCatalog, steps ...Preference[S]) (*LocaleResolver[S], error) {
	if err := ValidateLocaleCatalog(locales); err != nil {
		return nil, err
	}
	if len(steps) > MaxPreferences {
		return nil, fault.New(fault.Invalid, "too many locale preferences")
	}
	for i, step := range steps {
		if err := step.Validate(); err != nil {
			return nil, err
		}
		for _, earlier := range steps[:i] {
			if earlier.source == step.source {
				return nil, fault.New(fault.Duplicate, "duplicate locale preference "+string(step.source))
			}
		}
	}
	return &LocaleResolver[S]{locales: locales, steps: slices.Clone(steps)}, nil
}

// Resolve selects the first supported preference for subject, or the default.
// Unsupported preferences defer; preference failures and cancellation return
// an error instead of silently choosing the default.
func (r *LocaleResolver[S]) Resolve(ctx context.Context, subject S) (Resolution, error) {
	if r == nil || r.locales == nil {
		return Resolution{}, invalidLocale()
	}
	set, err := SnapshotLocales(ctx, r.locales)
	if err != nil {
		return Resolution{}, err
	}
	var ignored PreferenceSource
	for _, step := range r.steps {
		locale, outcome, err := step.match(ctx, set, subject)
		if err != nil {
			return Resolution{}, err
		}
		switch outcome {
		case selected:
			return Resolution{Locale: locale, Source: step.source, Ignored: ignored}, nil
		case malformed:
			if ignored == "" {
				ignored = step.source
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return Resolution{}, err
	}
	return Resolution{Locale: set.Default(), Source: DefaultSource, Ignored: ignored}, nil
}

// WithResolvedLocale resolves subject and records the selection on ctx, so
// later formatting, validation and model-content reads use the same locale.
func (r *LocaleResolver[S]) WithResolvedLocale(ctx context.Context, subject S) (context.Context, Resolution, error) {
	resolution, err := r.Resolve(ctx, subject)
	if err != nil {
		return nil, Resolution{}, err
	}
	return context.WithValue(ctx, localeContextKey{}, resolution.Locale), resolution, nil
}
