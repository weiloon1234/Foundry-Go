package validation

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/i18n"
	"github.com/weiloon1234/Foundry-Go/i18n/message"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
)

// WithTranslation attaches a generated, typed message to a leaf rule. Arguments
// are public declaration data, evaluated and snapshotted once; never pass received
// secrets. Optional text arguments attribute/other receive the bound field labels.
// The rule's current message is the English fallback. WithMessage restores a
// literal override; it is not parsed as a template.
func WithTranslation[T, A any](rule Rule[T], translated message.Message[A], args A) Rule[T] {
	if rule.Validate() != nil {
		return rule
	}
	if rule.info.Kind != LeafKind {
		return failed[T](invalid("translation override requires a leaf rule"))
	}
	var prepared i18n.PreparedMessage
	err := callback.Isolated("validation message declaration", func() error {
		var err error
		prepared, err = translated.BindLiteral(context.Background(), args, rule.info.Spec.Message)
		return err
	})
	if err != nil {
		return failed[T](fault.Wrap(fault.Invalid, "invalid validation message", err))
	}
	if err := validateLabelParameters(prepared.Description().Definition); err != nil {
		return failed[T](err)
	}
	before := infoBytes(rule.info)
	rule.info = cloneDescription(rule.info)
	recipe := prepared.Description()
	rule.info.Spec.Translation = &recipe
	rule.message = &prepared
	rule.bytes += infoBytes(rule.info) - before
	if rule.bytes > maxDescriptionBytes {
		return failed[T](invalid("validation metadata exceeds its byte bound"))
	}
	return rule
}

type issueMessage struct {
	message                *i18n.PreparedMessage
	field, label           string
	labelKey               i18n.MessageKey
	otherField, otherLabel string
	otherLabelKey          i18n.MessageKey
}

func localizedLabel(ctx context.Context, catalog *i18n.Catalog, locale i18n.LocaleID, key i18n.MessageKey, fallback string) (string, error) {
	if catalog == nil || key == "" {
		return fallback, nil
	}
	if _, exists := catalog.Definition(key); !exists {
		return fallback, nil
	}
	result, err := catalog.FormatDynamic(ctx, locale, key, nil)
	if err != nil {
		return "", err
	}
	if result.Missing {
		return fallback, nil
	}
	return result.Text, nil
}
func (i issueMessage) render(ctx context.Context, catalog *i18n.Catalog, locale i18n.LocaleID) (string, error) {
	if i.message == nil {
		return "", nil
	}
	prepared := *i.message
	var err error
	if i.label != "" || i.field != "" || i.labelKey != "" {
		attribute := i.label
		if attribute == "" {
			attribute = i.field
		}
		attribute, err = localizedLabel(ctx, catalog, locale, i.labelKey, attribute)
		if err != nil {
			return "", err
		}
		prepared, err = prepared.WithText("attribute", attribute)
		if err != nil {
			return "", err
		}
	}
	other := i.otherLabel
	if other == "" {
		other = i.otherField
	}
	if other != "" {
		other, err = localizedLabel(ctx, catalog, locale, i.otherLabelKey, other)
		if err != nil {
			return "", err
		}
		prepared, err = prepared.WithText("other", other)
		if err != nil {
			return "", err
		}
	}
	result, err := prepared.Format(ctx, catalog, locale)
	return result.Text, err
}

// Localize returns owned labels/messages using a request-independent explicit
// catalog and locale. Codes, paths, order and truncation are unchanged. Missing
// translations retain English; conflicting declarations return an error.
func (e *Errors) Localize(ctx context.Context, catalog *i18n.Catalog, locale i18n.LocaleID) (*Errors, error) {
	result, err := e.LocalizeLabels(ctx, catalog, locale)
	if err != nil {
		return nil, err
	}
	for index, item := range e.messages {
		if item.message == nil {
			continue
		}
		text, err := item.render(ctx, catalog, locale)
		if err != nil {
			return nil, err
		}
		result.issues[index].Message = text
	}
	return result, nil
}

func validateLabelParameters(definition i18n.MessageDefinition) error {
	for _, p := range definition.Parameters {
		if (p.Name == "attribute" || p.Name == "other") && p.Kind != i18n.TextParameter {
			return invalid("validation label parameters must be text")
		}
	}
	return nil
}
