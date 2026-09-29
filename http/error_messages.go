package http

import (
	"bufio"
	"context"
	"io"
	"net"
	stdhttp "net/http"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/i18n"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
)

// Decoder messages deliberately describe the expected shape, never the rejected
// value. Unknown/custom diagnostics retain their original approved message.
var decoderMessages = [...]struct {
	code contract.IssueCode
	text string
}{
	{contract.TypeIssue, "This field has an invalid type."},
	{contract.KeyIssue, "This field has an invalid key."},
	{contract.NullIssue, "This field must not be null."},
	{contract.RequiredIssue, "This field must be supplied."},
	{contract.UnknownIssue, "This field is not allowed."},
	{contract.ValueIssue, "This field has an invalid value."},
	{contract.LengthIssue, "This field has an invalid length."},
}

// MessageDefinitions returns the parameter-free signatures for shared HTTP
// error envelopes (http.error.<code>) and decoding issues (http.input.<code>).
// Configured applications register these automatically when locales are enabled.
func MessageDefinitions() []i18n.MessageDefinition {
	result := make([]i18n.MessageDefinition, 0, len(errorDefinitions)+len(decoderMessages))
	for _, d := range errorDefinitions {
		result = append(result, i18n.MessageDefinition{Key: errorMessageKey(d.Code)})
	}
	for _, d := range decoderMessages {
		result = append(result, i18n.MessageDefinition{Key: i18n.MessageKey("http.input." + string(d.code))})
	}
	return result
}

// errorMessageKey is the catalog key shared by built-in envelopes and declared
// application errors.
func errorMessageKey(code ErrorCode) i18n.MessageKey {
	return i18n.MessageKey("http.error." + string(code))
}

type errorPresenter struct {
	catalog *i18n.Catalog
	locale  i18n.LocaleID
}

func (p *errorPresenter) text(ctx context.Context, key i18n.MessageKey, fallback string) string {
	definition, exists := p.catalog.Definition(key)
	if !exists || len(definition.Parameters) != 0 {
		return fallback
	}
	result, err := p.catalog.FormatDynamic(ctx, p.locale, key, nil)
	if err != nil || result.Missing {
		return fallback
	}
	return result.Text
}
func (p *errorPresenter) present(ctx context.Context, payload *ErrorResponse) {
	// A declared application error is localized only when the catalog defines
	// its parameter-free key (see ErrorDeclaration.MessageDefinition); its
	// declared message remains the fallback. The code and status never change.
	payload.Message = p.text(ctx, errorMessageKey(payload.Code), payload.Message)
	if payload.Code != BadRequest {
		return
	}
	for i := range payload.Issues {
		issue := &payload.Issues[i]
		if issue.Message != "" {
			continue
		}
		for _, d := range decoderMessages {
			if d.code == issue.Code {
				issue.Message = p.text(ctx, i18n.MessageKey("http.input."+string(d.code)), d.text)
				break
			}
		}
	}
}

// This transparent owner retains the catalog outside request context and keeps
// native transport capabilities. Unwrap visits it even through buffering owners.
type localizedResponseWriter struct {
	stdhttp.ResponseWriter
	presenter errorPresenter
}

func (w *localizedResponseWriter) errorMessages() *errorPresenter { return &w.presenter }
func (w *localizedResponseWriter) ReadFrom(r io.Reader) (int64, error) {
	return io.Copy(w.ResponseWriter, r)
}
func (w *localizedResponseWriter) FlushError() error {
	return stdhttp.NewResponseController(w.ResponseWriter).Flush()
}
func (w *localizedResponseWriter) Unwrap() stdhttp.ResponseWriter           { return responseController{w} }
func (w *localizedResponseWriter) underlyingWriter() stdhttp.ResponseWriter { return w.ResponseWriter }
func (w *localizedResponseWriter) hijackResponse() (net.Conn, *bufio.ReadWriter, error) {
	return stdhttp.NewResponseController(w.ResponseWriter).Hijack()
}
func (w *localizedResponseWriter) enableFullDuplexResponse() error {
	return stdhttp.NewResponseController(w.ResponseWriter).EnableFullDuplex()
}
func findErrorPresenter(w stdhttp.ResponseWriter) (*errorPresenter, error) {
	var result *errorPresenter
	err := callback.Invoke("HTTP message presenter lookup", func() error {
		for range 64 {
			candidate := w
			if controller, ok := w.(responseController); ok {
				candidate = controller.controlledResponseWriter
			}
			if owner, ok := candidate.(interface{ errorMessages() *errorPresenter }); ok {
				result = owner.errorMessages()
				return nil
			}
			wrapper, ok := w.(interface{ Unwrap() stdhttp.ResponseWriter })
			if !ok {
				return nil
			}
			w = wrapper.Unwrap()
			if w == nil {
				return nil
			}
		}
		return fault.New(fault.Invalid, "HTTP response wrapper chain exceeds its bound")
	})
	return result, err
}
