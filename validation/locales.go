package validation

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/i18n"
)

// Locales checks a locale-keyed map, such as translated text, against one
// locale catalog snapshot per check. Every key must be a supported locale
// (foundry.supported_locale at the entry path, such as /title/fr), and every
// locale require selects must be present with nonempty content
// (foundry.required at its entry path, such as /title/ms). The catalog is
// runtime configuration, so the rule is server-only metadata.
func Locales[M ~map[i18n.LocaleID]V, V ~string](catalog i18n.LocaleCatalog, require i18n.LocaleRequirement) Rule[M] {
	if i18n.ValidateLocaleCatalog(catalog) != nil {
		return failed[M](invalid("locale validation requires a locale catalog"))
	}
	if require.Validate() != nil {
		return failed[M](invalid("locale validation requires a valid locale requirement"))
	}
	empty := nativeEmptyCheck[V]()
	if empty.err != nil {
		return failed[M](empty.err)
	}
	snapshot := NewSlot[i18n.LocaleSet]()
	supported := reportRule(Spec{ID: "foundry.supported_locale"}, func(s *execution, input M, report *Report) error {
		locales, err := snapshot.Value(s.ctx)
		if err != nil {
			return err
		}
		for _, key := range sortedKeys(s, input) {
			if locales.Contains(key) {
				continue
			}
			// A key that cannot be a path segment, such as one containing
			// NUL, is still a rejected locale, reported at the map itself.
			if validText(string(key), false) {
				report.Add(string(key))
			} else {
				report.Add()
			}
		}
		return nil
	})
	required := reportRule(requiredSpec(empty.parameters()), func(s *execution, input M, report *Report) error {
		locales, err := snapshot.Value(s.ctx)
		if err != nil {
			return err
		}
		for _, locale := range require.Required(locales) {
			text, present := input[locale]
			if !present {
				report.Add(string(locale))
				continue
			}
			if isEmpty, valid := empty.inspect(s, text); isEmpty || !valid {
				report.Add(string(locale))
			}
		}
		return nil
	})
	return Provide(snapshot, func(ctx context.Context, _ M) (i18n.LocaleSet, error) {
		return i18n.SnapshotLocales(ctx, catalog)
	}, supported, required)
}

// MaxBytes limits the UTF-8 encoded size of text, for storage bounds measured
// in bytes. MaxLength counts characters instead. It is server-only metadata.
func MaxBytes[S ~string](maximum int) Rule[S] {
	if maximum < 0 {
		return failed[S](invalid("maximum text bytes must not be negative"))
	}
	return valueRule(Spec{ID: "foundry.max_bytes", Parameters: []Parameter{parameter("max", maximum)}}, true, func(s *execution, input S) (bool, error) {
		text := string(input)
		return textValue(s, text) && len(text) <= maximum, nil
	})
}

// reportRule is a built-in, server-only leaf that records its own issues at
// paths below its input, sharing Hook's Report without an application
// definition, so one built-in ID can appear in several rules of a tree.
func reportRule[T any](spec Spec, check func(*execution, T, *Report) error) Rule[T] {
	rule := valueRule(spec, true, func(*execution, T) (bool, error) { return true, nil })
	if rule.err != nil {
		return rule
	}
	rule.leaf = nil
	rule.report = func(s *execution, input T, spec Spec, declared *i18n.PreparedMessage) error {
		report := &Report{state: s, spec: spec, declared: declared}
		err := check(s, input, report)
		report.closed = true
		if err != nil {
			return err
		}
		return report.err
	}
	return rule
}
