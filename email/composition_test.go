package email_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/email"
	emaillog "github.com/weiloon1234/Foundry-Go/email/log"
	"github.com/weiloon1234/Foundry-Go/email/memory"
	"github.com/weiloon1234/Foundry-Go/i18n"
)

type Receipt struct {
	Name  string
	Total string
}

func TestLayoutWrapsTextAndHTMLBodiesWithTypedData(t *testing.T) {
	template, err := email.NewTemplate[Receipt](email.TemplateSource{
		Subject: "Receipt for {{.Name}}",
		Text:    "Total {{.Total}}",
		HTML:    "<p>Total {{.Total}}</p>",
		Layout: email.Layout{
			Text: "Hello {{.Name}}\n{{template \"content\" .}}\n-- Shop",
			HTML: "<html><body><h1>{{.Name}}</h1>{{template \"content\" .}}</body></html>",
		},
		MaxBytes: 4096,
	})
	if err != nil {
		t.Fatal(err)
	}
	body, err := template.Render(t.Context(), Receipt{Name: "<Ada>", Total: "10"})
	if err != nil {
		t.Fatal(err)
	}
	if body.Subject != "Receipt for <Ada>" || body.Text != "Hello <Ada>\nTotal 10\n-- Shop" {
		t.Fatalf("text layout: %q %q", body.Subject, body.Text)
	}
	if body.HTML != "<html><body><h1>&lt;Ada&gt;</h1><p>Total 10</p></body></html>" {
		t.Fatalf("html layout: %q", body.HTML)
	}
	if _, err := email.NewTemplate[Receipt](email.TemplateSource{Text: "x", Layout: email.Layout{Text: "no content call"}, MaxBytes: 100}); err == nil {
		t.Fatal("layout without content accepted")
	}
}

func TestLocalizedTemplateFallsBackAndSetsMessageLocale(t *testing.T) {
	locales, err := i18n.NewLocaleSet("en", "en", "pt", "pt-BR")
	if err != nil {
		t.Fatal(err)
	}
	template, err := email.NewLocalizedTemplate[Receipt](locales, map[i18n.LocaleID]email.TemplateSource{
		"en": {Subject: "Receipt", Text: "Total {{.Total}}", MaxBytes: 100},
		"pt": {Subject: "Recibo", Text: "Total {{.Total}}", MaxBytes: 100},
	})
	if err != nil {
		t.Fatal(err)
	}
	for locale, want := range map[i18n.LocaleID]string{"pt-BR": "Recibo", "pt": "Recibo", "fr": "Receipt", "": "Receipt"} {
		body, err := template.Render(t.Context(), locale, Receipt{Total: "1"})
		if err != nil || body.Subject != want {
			t.Fatalf("locale %q: %q %v", locale, body.Subject, err)
		}
	}
	body, err := template.Render(t.Context(), "pt-BR", Receipt{Total: "1"})
	if err != nil || body.Locale != "pt" {
		t.Fatal("rendered locale not recorded", body.Locale, err)
	}
	m, driver := memoryMailer(t)
	if _, err := m.Send(t.Context(), body.Message(address(t, "shop@example.test"), address(t, "ada@example.test")), email.SendOptions{}); err != nil {
		t.Fatal(err)
	}
	sent := driver.Messages()
	if sent[0].Message().LocaleID() != "pt" || !bytes.Contains(sent[0].MIME(), []byte("Content-Language: pt\r\n")) {
		t.Fatal("message locale was not sent")
	}
	if _, err := email.NewLocalizedTemplate[Receipt](locales, map[i18n.LocaleID]email.TemplateSource{"pt": {Text: "x", MaxBytes: 10}}); err == nil {
		t.Fatal("localized template without the default locale accepted")
	}
}

func TestDataAttachmentsAreBoundedAndNotSnapshotted(t *testing.T) {
	invoice, err := email.NewDataAttachment("invoice.txt", "text/plain", []byte("invoice 42"))
	if err != nil {
		t.Fatal(err)
	}
	logo, err := email.NewDataAttachment("logo.png", "image/png", []byte{0x89, 'P', 'N', 'G'})
	if err == nil {
		logo, err = logo.WithContentID("logo")
	}
	if err != nil {
		t.Fatal(err)
	}
	message := message(t).HTML(`<img src="cid:logo">`).AttachData(invoice, logo)
	m, driver := memoryMailer(t)
	if _, err := m.Send(t.Context(), message, email.SendOptions{}); err != nil {
		t.Fatal(err)
	}
	sent := driver.Messages()[0]
	if len(sent.Attachments()) != 2 || string(sent.Attachments()[0].Bytes()) != "invoice 42" || !bytes.Contains(sent.MIME(), []byte("Content-Id: <logo>")) {
		t.Fatal("data attachments were not submitted")
	}
	if _, err := email.CaptureMessage(message); err == nil {
		t.Fatal("in-memory attachment entered a durable snapshot")
	}
	config := email.DefaultConfig()
	config.MaxAttachmentBytes = 4
	bounded, err := email.New(driverFor(t), nil, config, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = bounded.Close(context.Background()) })
	if _, err := bounded.Send(t.Context(), message, email.SendOptions{}); !errorsIs(err, email.Construction) {
		t.Fatal("oversized data attachment accepted", err)
	}
	if _, err := email.NewDataAttachment("../escape", "text/plain", []byte("x")); err == nil {
		t.Fatal("unsafe filename accepted")
	}
}

func TestPreviewDriverLogsRenderedMessageWithRedactedHeaders(t *testing.T) {
	var logs bytes.Buffer
	driver, err := emaillog.NewPreview(slog.New(slog.NewJSONHandler(&logs, nil)), 16)
	if err != nil {
		t.Fatal(err)
	}
	m := mailer(t, driver, nil, nil)
	sent := message(t).Header("X-Api-Key", "live-secret").Header("X-Campaign", "spring").Bcc(address(t, "hidden@example.test"))
	if _, err := m.Send(t.Context(), sent, email.SendOptions{}); err != nil {
		t.Fatal(err)
	}
	var entry map[string]any
	if err := json.Unmarshal(logs.Bytes(), &entry); err != nil {
		t.Fatal(err)
	}
	text := logs.String()
	headers, _ := entry["headers"].(map[string]any)
	if entry["msg"] != "email preview" || entry["subject"] != "Private subject" || headers["X-Campaign"] != "spring" || headers["X-Api-Key"] != "[redacted]" {
		t.Fatal("preview missing rendered content", text)
	}
	if strings.Contains(text, "live-secret") || strings.Contains(text, "hidden@example.test") {
		t.Fatal("preview leaked a credential header or BCC address")
	}
}

func driverFor(t *testing.T) email.Driver {
	t.Helper()
	driver, err := memory.New(4)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(driver.Close)
	return driver
}

func errorsIs(err error, kind email.Kind) bool { return email.Classification(err) == kind }
