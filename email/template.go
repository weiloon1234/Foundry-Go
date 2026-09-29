package email

import (
	"bytes"
	"context"
	"fmt"
	htmltemplate "html/template"
	"io"
	"log/slog"
	"reflect"
	"strings"
	texttemplate "text/template"

	"github.com/weiloon1234/Foundry-Go/i18n"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
)

// TemplateSource is trusted application template code. Data is escaped by
// html/template. Do not put untrusted template source or template.HTML in DTOs.
// Layout optionally wraps the Text and HTML bodies.
type TemplateSource struct {
	Subject, Text, HTML string
	Layout              Layout
	MaxBytes            int
}

// Layout is a shared trusted wrapper for message bodies: its HTML and Text
// sources render the message's own body where they call
// {{template "content" .}}, with the same typed data. A layout part applies
// only when the message defines that part. The subject is never wrapped.
type Layout struct {
	Text, HTML string
}
type renderedTemplates struct {
	subject, text *texttemplate.Template
	html          *htmltemplate.Template
	limit         int
}
type Template[T any] struct{ templates *renderedTemplates }

// DynamicTemplate is the explicit heterogeneous-map escape hatch.
type DynamicTemplate struct{ templates *renderedTemplates }

// Body is rendered template output. Locale is set by LocalizedTemplate and
// carried to the message.
type Body struct {
	Subject, Text, HTML string
	Locale              i18n.LocaleID
}

func (Body) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("rendered email body")) }
func (b Body) Message(from Address, to ...Address) Message {
	return NewMessage(from, b.Subject, to...).Text(b.Text).HTML(b.HTML).Locale(b.Locale)
}

// LocalizedTemplate holds one typed template per locale and selects it along
// the locale set's fallback chain (for example pt-BR, then pt, then the
// default). The rendered Body records the locale actually used.
type LocalizedTemplate[T any] struct {
	locales   i18n.LocaleSet
	templates map[i18n.LocaleID]Template[T]
}

// NewLocalizedTemplate requires a template for the set's default locale; every
// source locale must belong to the set.
func NewLocalizedTemplate[T any](locales i18n.LocaleSet, sources map[i18n.LocaleID]TemplateSource) (LocalizedTemplate[T], error) {
	if locales.Validate() != nil {
		return LocalizedTemplate[T]{}, Construction
	}
	if _, ok := sources[locales.Default()]; !ok {
		return LocalizedTemplate[T]{}, Construction
	}
	result := LocalizedTemplate[T]{locales: locales, templates: make(map[i18n.LocaleID]Template[T], len(sources))}
	for locale, source := range sources {
		if !locales.Contains(locale) {
			return LocalizedTemplate[T]{}, Construction
		}
		template, err := NewTemplate[T](source)
		if err != nil {
			return LocalizedTemplate[T]{}, err
		}
		result.templates[locale] = template
	}
	return result, nil
}

// Render uses the first template on locale's fallback chain; an unsupported
// or empty locale renders the default.
func (t LocalizedTemplate[T]) Render(ctx context.Context, locale i18n.LocaleID, data T) (Body, error) {
	if t.templates == nil {
		return Body{}, Construction
	}
	chain := []i18n.LocaleID{t.locales.Default()}
	if matched, ok := t.locales.Match(locale); ok && locale != "" {
		if fallbacks, err := t.locales.Fallbacks(matched); err == nil {
			chain = fallbacks
		}
	}
	for _, candidate := range chain {
		template, ok := t.templates[candidate]
		if !ok {
			continue
		}
		body, err := template.Render(ctx, data)
		body.Locale = candidate
		return body, err
	}
	return Body{}, Construction
}
func NewTemplate[T any](source TemplateSource) (Template[T], error) {
	typ := reflect.TypeFor[T]()
	if typ.Kind() != reflect.Struct || typ.Name() == "" {
		return Template[T]{}, Construction
	}
	t, err := parseTemplates(source)
	return Template[T]{templates: t}, err
}
func NewDynamicTemplate(source TemplateSource) (DynamicTemplate, error) {
	t, err := parseTemplates(source)
	return DynamicTemplate{templates: t}, err
}
func parseTemplates(s TemplateSource) (*renderedTemplates, error) {
	if s.MaxBytes < 1 || s.MaxBytes > 32<<20 || s.Text == "" && s.HTML == "" || len(s.Subject)+len(s.Text)+len(s.HTML)+len(s.Layout.Text)+len(s.Layout.HTML) > s.MaxBytes {
		return nil, Construction
	}
	for _, layout := range []string{s.Layout.Text, s.Layout.HTML} {
		if layout != "" && !strings.Contains(layout, `"content"`) {
			return nil, Construction
		}
	}
	subject, err := texttemplate.New("subject").Option("missingkey=error").Parse(s.Subject)
	if err != nil {
		return nil, Construction
	}
	text := texttemplate.New("text").Option("missingkey=error")
	if s.Layout.Text != "" && s.Text != "" {
		// The layout is the executed root; the body is its "content".
		if text, err = text.Parse(s.Layout.Text); err == nil {
			_, err = text.New("content").Parse(s.Text)
		}
	} else {
		text, err = text.Parse(s.Text)
	}
	if err != nil {
		return nil, Construction
	}
	html := htmltemplate.New("html").Option("missingkey=error")
	if s.Layout.HTML != "" && s.HTML != "" {
		if html, err = html.Parse(s.Layout.HTML); err == nil {
			_, err = html.New("content").Parse(s.HTML)
		}
	} else {
		html, err = html.Parse(s.HTML)
	}
	if err != nil {
		return nil, Construction
	}
	return &renderedTemplates{subject: subject, text: text, html: html, limit: s.MaxBytes}, nil
}
func (t Template[T]) Render(ctx context.Context, data T) (Body, error) {
	return renderTemplates(ctx, t.templates, data)
}
func (t DynamicTemplate) Render(ctx context.Context, data map[string]any) (Body, error) {
	return renderTemplates(ctx, t.templates, data)
}

type boundedBuffer struct {
	buffer    bytes.Buffer
	remaining int
	ctx       context.Context
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	if b.ctx != nil && b.ctx.Err() != nil {
		return 0, Construction
	}
	if len(p) > b.remaining {
		return 0, Construction
	}
	b.remaining -= len(p)
	return b.buffer.Write(p)
}
func (b *boundedBuffer) WriteString(text string) (int, error) { return b.Write([]byte(text)) }
func (b *boundedBuffer) Reset()                               { b.buffer.Reset() }
func (b *boundedBuffer) String() string                       { return b.buffer.String() }
func (b *boundedBuffer) Bytes() []byte                        { return b.buffer.Bytes() }
func renderTemplates(ctx context.Context, t *renderedTemplates, data any) (Body, error) {
	if ctx == nil || ctx.Err() != nil || t == nil {
		return Body{}, Construction
	}
	var result Body
	err := callback.Isolated("render email template", func() error {
		b := &boundedBuffer{remaining: t.limit, ctx: ctx}
		for _, part := range []struct {
			execute func(io.Writer, any) error
			output  *string
		}{{t.subject.Execute, &result.Subject}, {t.text.Execute, &result.Text}, {t.html.Execute, &result.HTML}} {
			b.Reset()
			if err := part.execute(b, data); err != nil {
				return Construction
			}
			*part.output = b.String()
		}
		return nil
	})
	if err != nil || ctx.Err() != nil || !headerText(result.Subject) || len(result.Subject) > 2000 {
		return Body{}, Construction
	}
	return result, nil
}

func (Body) LogValue() slog.Value { return slog.StringValue("rendered email body") }
