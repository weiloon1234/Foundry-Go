package email

import (
	"bytes"
	"context"
	"fmt"
	htmltemplate "html/template"
	"io"
	"log/slog"
	"reflect"
	texttemplate "text/template"

	"github.com/weiloon1234/Foundry-Go/internal/callback"
)

// TemplateSource is trusted application template code. Data is escaped by
// html/template. Do not put untrusted template source or template.HTML in DTOs.
type TemplateSource struct {
	Subject, Text, HTML string
	MaxBytes            int
}
type renderedTemplates struct {
	subject, text *texttemplate.Template
	html          *htmltemplate.Template
	limit         int
}
type Template[T any] struct{ templates *renderedTemplates }

// DynamicTemplate is the explicit heterogeneous-map escape hatch.
type DynamicTemplate struct{ templates *renderedTemplates }
type Body struct{ Subject, Text, HTML string }

func (Body) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("rendered email body")) }
func (b Body) Message(from Address, to ...Address) Message {
	return NewMessage(from, b.Subject, to...).Text(b.Text).HTML(b.HTML)
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
	if s.MaxBytes < 1 || s.MaxBytes > 32<<20 || s.Text == "" && s.HTML == "" || len(s.Subject)+len(s.Text)+len(s.HTML) > s.MaxBytes {
		return nil, Construction
	}
	subject, err := texttemplate.New("subject").Option("missingkey=error").Parse(s.Subject)
	if err != nil {
		return nil, Construction
	}
	text, err := texttemplate.New("text").Option("missingkey=error").Parse(s.Text)
	if err != nil {
		return nil, Construction
	}
	html, err := htmltemplate.New("html").Option("missingkey=error").Parse(s.HTML)
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
