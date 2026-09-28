package validation

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/i18n"
	"strings"
)

// WithLabel returns a field with a static public display name. It preserves
// the wire name, selector and concrete types. Diagnostics keep their JSON
// Pointer path and carry the label separately; built-in messages use this name.
// Nested fields select their own labels, while scalar collection items use
// their containing field's label. Localization produces an owned copy.
func (f Field[T, V]) WithLabel(label string) Field[T, V] {
	if err := f.Validate(); err != nil {
		f.err = err
		return f
	}
	if !validText(label, false) || strings.TrimSpace(label) == "" {
		f.err = invalid("validation field label requires bounded, nonblank public text")
		return f
	}
	f.label = label
	return f
}

// WithLabelKey attaches a parameter-free UI message while preserving the static
// label as the missing-translation fallback. It never changes the wire path.
func (f Field[T, V]) WithLabelKey(key i18n.MessageKey) Field[T, V] {
	if err := f.Validate(); err != nil {
		f.err = err
		return f
	}
	if err := key.Validate(); err != nil {
		f.err = err
		return f
	}
	f.labelKey = key
	return f
}

// LocalizeLabels returns independent diagnostics. Missing registered labels
// retain their existing static text; declaration/configuration failures return
// an error. Rule messages remain the rule's approved public text.
func (e *Errors) LocalizeLabels(ctx context.Context, catalog *i18n.Catalog, locale i18n.LocaleID) (*Errors, error) {
	if e == nil || ctx == nil || catalog.Validate() != nil {
		return nil, invalid("invalid validation label localization")
	}
	set, err := catalog.Snapshot(ctx)
	if err != nil {
		return nil, err
	}
	if !set.Contains(locale) {
		return nil, invalid("unsupported validation locale")
	}
	result := &Errors{issues: e.Issues(), messages: e.messages, truncated: e.truncated}
	for i := range result.issues {
		issue := &result.issues[i]
		if issue.LabelKey == "" {
			continue
		}
		fallback := issue.Label
		if i < len(e.messages) {
			fallback = e.messages[i].label
		}
		label, err := localizedLabel(ctx, catalog, locale, issue.LabelKey, fallback)
		if err != nil {
			return nil, err
		}
		issue.Label = label
	}
	return result, nil
}
