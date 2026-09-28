package email_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"mime/multipart"
	"net/mail"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/email"
	emaillog "github.com/weiloon1234/Foundry-Go/email/log"
	"github.com/weiloon1234/Foundry-Go/email/memory"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/storage"
	"github.com/weiloon1234/Foundry-Go/storage/local"
	emailtest "github.com/weiloon1234/Foundry-Go/testkit/email"
)

func address(t *testing.T, text string) email.Address {
	t.Helper()
	a, err := email.ParseAddress(text)
	if err != nil {
		t.Fatal(err)
	}
	return a
}
func message(t *testing.T) email.Message {
	return email.NewMessage(address(t, "Sender <sender@example.test>"), "Private subject", address(t, "Recipient <recipient@example.test>")).Text("Private body")
}
func mailer(t *testing.T, driver email.Driver, disks *storage.Registry, observer email.Observer) *email.Mailer {
	t.Helper()
	m, err := email.New(driver, disks, email.DefaultConfig(), observer)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := m.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	return m
}
func memoryMailer(t *testing.T) (*email.Mailer, *memory.Driver) {
	t.Helper()
	driver, err := memory.New(16)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(driver.Close)
	return mailer(t, driver, nil, nil), driver
}

func TestAddressesAndHeaderInjection(t *testing.T) {
	for _, invalid := range []string{"", "no-address", "x@example.test\r\nBcc: leak@example.test", "a@example.test,b@example.test", "ü@example.test", "x@example.test\x00"} {
		if _, err := email.ParseAddress(invalid); err == nil {
			t.Fatal("invalid address accepted")
		}
	}
	a, err := email.NewAddress("valid@example.test", "A, \"quoted\" 名")
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(a)
	if err != nil {
		t.Fatal(err)
	}
	var decoded email.Address
	if json.Unmarshal(encoded, &decoded) != nil || decoded != a {
		t.Fatal("address JSON did not round trip")
	}
	original := decoded
	if json.Unmarshal([]byte(`"bad"`), &decoded) == nil || decoded != original {
		t.Fatal("failed decode mutated address")
	}
	m, d := memoryMailer(t)
	for _, bad := range []email.Message{message(t).Header("Bcc", "leak@example.test"), message(t).Header("X-Test", "value\r\nInjected: yes"), message(t).Header("Content-Type", "text/html"), message(t).Header("Bad:Name", "value"), email.NewMessage(a, "subject\r\nInjected: yes", a).Text("text"), email.Message{}} {
		if _, err := m.Send(t.Context(), bad, email.SendOptions{}); !errors.Is(err, email.Construction) {
			t.Fatal("construction rejection missing", err)
		}
	}
	emailtest.AssertCount(t, d, 0)
	if text := fmt.Sprintf("%+v %#v", a, message(t)); strings.Contains(text, "example.test") || strings.Contains(text, "Private") {
		t.Fatal("private value formatted")
	}
}

type welcomeData struct {
	Name string
	URL  string
}

func TestTypedTemplateEscapingBoundsAndFailure(t *testing.T) {
	template, err := email.NewTemplate[welcomeData](email.TemplateSource{Subject: "Hello {{.Name}}", Text: "{{.Name}}", HTML: `<p>{{.Name}}</p><a href="{{.URL}}">Open</a>`, MaxBytes: 4096})
	if err != nil {
		t.Fatal(err)
	}
	body, err := template.Render(t.Context(), welcomeData{Name: `<script>alert(1)</script>`, URL: "javascript:alert(1)"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body.HTML, "&lt;script&gt;") || !strings.Contains(body.HTML, "#ZgotmplZ") || strings.Contains(body.HTML, "<script>") {
		t.Fatal("HTML escaping was bypassed")
	}
	if _, err := template.Render(t.Context(), welcomeData{Name: "injected\r\nBcc: bad"}); err == nil {
		t.Fatal("rendered subject injection accepted")
	}
	bounded, err := email.NewTemplate[welcomeData](email.TemplateSource{Text: "{{.Name}}", MaxBytes: 16})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bounded.Render(t.Context(), welcomeData{Name: strings.Repeat("x", 17)}); err == nil {
		t.Fatal("render bound ignored")
	}
	if _, err := email.NewTemplate[map[string]string](email.TemplateSource{Text: "x", MaxBytes: 16}); err == nil {
		t.Fatal("map accepted outside dynamic escape hatch")
	}
	dynamic, err := email.NewDynamicTemplate(email.TemplateSource{Text: "{{.missing}}", MaxBytes: 32})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := dynamic.Render(t.Context(), map[string]any{}); err == nil {
		t.Fatal("missing dynamic key ignored")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := template.Render(ctx, welcomeData{}); err == nil {
		t.Fatal("cancelled render accepted")
	}
}
func attachmentStore(t *testing.T, wrap ...func(storage.Backend) storage.Backend) (*storage.Registry, email.Attachment) {
	t.Helper()
	backend, err := local.Open(t.Context(), local.DefaultConfig(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := backend.Close(); err != nil {
			t.Error(err)
		}
	})
	var peer storage.Backend = backend
	for _, decorator := range wrap {
		peer = decorator(peer)
	}
	disk, err := storage.NewDisk("mail", peer, storage.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := disk.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	key, err := storage.ParseKey("document.txt")
	if err != nil {
		t.Fatal(err)
	}
	stored, err := disk.Put(t.Context(), key, strings.NewReader("private attachment"), storage.PutOptions{ContentType: "text/plain"})
	if err != nil {
		t.Fatal(err)
	}
	registry, err := storage.NewRegistry(disk)
	if err != nil {
		t.Fatal(err)
	}
	return registry, email.Attachment{Disk: "mail", Key: key, Filename: "report.txt", ContentType: "text/plain", IfMatch: stored.Object.ETag}
}
func TestMIMEAttachmentsBCCAndSnapshots(t *testing.T) {
	registry, ordinary := attachmentStore(t)
	inline := ordinary
	inline.Filename = "logo.txt"
	inline.ContentID = "logo"
	driver, err := memory.New(4)
	if err != nil {
		t.Fatal(err)
	}
	defer driver.Close()
	m := mailer(t, driver, registry, nil)
	bcc := address(t, "Hidden <hidden@example.test>")
	recipient := address(t, "Recipient <recipient@example.test>")
	base := message(t)
	input := base.HTML(`<img src="cid:logo">`).Bcc(bcc).Cc(address(t, "cc@example.test")).ReplyTo(address(t, "reply@example.test")).Header("X-Custom", "original").Attach(ordinary, inline)
	changed := input.Header("X-Custom", "changed")
	if input.Headers()["X-Custom"] != "original" || base.HTMLBody() != "" || changed.Headers()["X-Custom"] != "changed" {
		t.Fatal("builders mutated their source")
	}
	result, err := m.Send(t.Context(), input, email.SendOptions{IdempotencyKey: "stable-key"})
	if err != nil || !result.Accepted {
		t.Fatal(result, err)
	}
	emailtest.AssertCount(t, driver, 1)
	emailtest.AssertRecipientCount(t, driver, bcc, 1)
	emailtest.AssertRecipientCount(t, driver, recipient, 1)
	out := driver.Messages()[0]
	wire := out.MIME()
	if bytes.Contains(wire, []byte("hidden@example.test")) || bytes.Contains(wire, []byte("Bcc:")) {
		t.Fatal("BCC leaked into MIME")
	}
	parsed, err := mail.ReadMessage(bytes.NewReader(wire))
	if err != nil {
		t.Fatal(err)
	}
	decodedSubject, err := new(mime.WordDecoder).DecodeHeader(parsed.Header.Get("Subject"))
	if err != nil || decodedSubject != "Private subject" {
		t.Fatal("subject encoding")
	}
	var kinds []string
	var attachmentBodies [][]byte
	var walk func(map[string][]string, io.Reader)
	walk = func(h map[string][]string, body io.Reader) {
		kind, params, err := mime.ParseMediaType(first(h, "Content-Type"))
		if err != nil {
			t.Fatal(err)
		}
		kinds = append(kinds, kind)
		if strings.HasPrefix(kind, "multipart/") {
			reader := multipart.NewReader(body, params["boundary"])
			for {
				part, err := reader.NextPart()
				if err == io.EOF {
					break
				}
				if err != nil {
					t.Fatal(err)
				}
				walk(part.Header, part)
				part.Close()
			}
			return
		}
		if first(h, "Content-Transfer-Encoding") == "base64" {
			data, err := io.ReadAll(base64.NewDecoder(base64.StdEncoding, body))
			if err != nil {
				t.Fatal(err)
			}
			attachmentBodies = append(attachmentBodies, data)
		}
	}
	walk(parsed.Header, parsed.Body)
	if strings.Join(kinds, ",") != "multipart/mixed,multipart/related,multipart/alternative,text/plain,text/html,text/plain,text/plain" || len(attachmentBodies) != 2 {
		t.Fatal("MIME nesting or attachments differ", kinds)
	}
	for _, data := range attachmentBodies {
		if string(data) != "private attachment" {
			t.Fatal("attachment changed")
		}
	}
	wire[0] = '!'
	copy := out.Attachments()[0].Bytes()
	copy[0] = '!'
	if driver.Messages()[0].MIME()[0] == '!' || driver.Messages()[0].Attachments()[0].Bytes()[0] == '!' {
		t.Fatal("snapshot exposed mutable storage")
	}
	for _, line := range bytes.Split(out.MIME(), []byte("\r\n")) {
		if len(line) > 998 {
			t.Fatal("MIME physical line too long")
		}
	}
}
func first(h map[string][]string, key string) string {
	if values := h[key]; len(values) > 0 {
		return values[0]
	}
	return ""
}
func TestAttachmentPinFailureAndMessageBounds(t *testing.T) {
	registry, attachment := attachmentStore(t)
	var sent atomic.Int32
	driver := email.DriverFunc(func(context.Context, email.Outbound) (email.Receipt, error) { sent.Add(1); return email.Receipt{}, nil })
	m := mailer(t, driver, registry, nil)
	attachment.IfMatch = `"wrong"`
	if _, err := m.Send(t.Context(), message(t).Attach(attachment), email.SendOptions{}); !errors.Is(err, email.Construction) {
		t.Fatal("stale attachment was sent", err)
	}
	config := email.DefaultConfig()
	config.MaxAttachmentBytes = 2
	bounded, err := email.New(driver, registry, config, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer bounded.Close(context.Background())
	attachment.IfMatch = ""
	if _, err := bounded.Send(t.Context(), message(t).Attach(attachment), email.SendOptions{}); !errors.Is(err, email.Construction) {
		t.Fatal("attachment byte bound ignored")
	}
	config.MaxMessageBytes = 1024
	config.MaxAttachmentBytes = 512
	small, err := email.New(driver, registry, config, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer small.Close(context.Background())
	if _, err := small.Send(t.Context(), message(t).Text(strings.Repeat("é", 450)), email.SendOptions{}); err == nil {
		t.Fatal("MIME encoding overhead escaped total bound")
	}
	if sent.Load() != 0 {
		t.Fatal("construction failure reached driver")
	}
}

type badError struct{ goexit bool }

func (b badError) Error() string { return "private driver failure" }
func (b badError) Is(error) bool {
	if b.goexit {
		runtime.Goexit()
	}
	panic("private panic")
}
func TestCallbacksAndUnknownFailuresDoNotLeakOrRetryAcceptance(t *testing.T) {
	for _, kind := range []string{"error", "panic", "goexit", "bad-is", "bad-is-goexit"} {
		t.Run(kind, func(t *testing.T) {
			m := mailer(t, email.DriverFunc(func(context.Context, email.Outbound) (email.Receipt, error) {
				switch kind {
				case "panic":
					panic("private")
				case "goexit":
					runtime.Goexit()
				case "bad-is":
					return email.Receipt{}, badError{}
				case "bad-is-goexit":
					return email.Receipt{}, badError{true}
				}
				return email.Receipt{}, errors.New("private")
			}), nil, nil)
			result, err := m.Send(t.Context(), message(t), email.SendOptions{})
			if !errors.Is(err, email.Ambiguous) || result.Accepted || strings.Contains(err.Error(), "private") {
				t.Fatal("unsafe failure classification", err)
			}
		})
	}
	_, d := memoryMailer(t)
	observed := mailer(t, d, nil, func(_ context.Context, n email.Notice) error {
		if n.Stage == email.Finished {
			runtime.Goexit()
		}
		return nil
	})
	result, err := observed.Send(t.Context(), message(t), email.SendOptions{})
	if err != nil || !result.Accepted || !result.ObserverFailed || observed.Snapshot().ObserverFailures != 1 {
		t.Fatal("observer converted acceptance to failure", result, err)
	}
}
func TestMailerCapacityCancellationAndSelfWait(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	var m *email.Mailer
	driver := email.DriverFunc(func(ctx context.Context, _ email.Outbound) (email.Receipt, error) {
		if !errors.Is(m.Close(ctx), fault.Cycle) {
			return email.Receipt{}, errors.New("self wait allowed")
		}
		close(entered)
		<-release
		return email.Receipt{}, nil
	})
	config := email.DefaultConfig()
	config.MaxActive = 1
	var err error
	m, err = email.New(driver, nil, config, nil)
	if err != nil {
		t.Fatal(err)
	}
	finished := make(chan error, 1)
	go func() { _, err := m.Send(context.Background(), message(t), email.SendOptions{}); finished <- err }()
	<-entered
	if _, err := m.Send(t.Context(), message(t), email.SendOptions{}); !errors.Is(err, email.Transient) {
		t.Fatal("active capacity not retained")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Millisecond)
	defer cancel()
	if !errors.Is(m.Close(ctx), context.DeadlineExceeded) {
		t.Fatal("close claimed unfinished callback")
	}
	select {
	case <-m.Done():
		t.Fatal("ownership abandoned")
	default:
	}
	once.Do(func() { close(release) })
	if err := <-finished; err != nil {
		t.Fatal("known acceptance changed after cancellation", err)
	}
	if err := m.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
}
func TestLogDriverRedactsMessageContent(t *testing.T) {
	var output bytes.Buffer
	driver, err := emaillog.New(slog.New(slog.NewJSONHandler(&output, nil)))
	if err != nil {
		t.Fatal(err)
	}
	m := mailer(t, driver, nil, nil)
	if _, err := m.Send(t.Context(), message(t), email.SendOptions{IdempotencyKey: "private-key"}); err != nil {
		t.Fatal(err)
	}
	for _, private := range []string{"Private", "example.test", "private-key"} {
		if strings.Contains(output.String(), private) {
			t.Fatal("private data logged")
		}
	}
}
func FuzzAddressRoundTrip(f *testing.F) {
	for _, seed := range []string{"a@example.test", `A <a@example.test>`, "\r\n", "名 <a@example.test>"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, text string) {
		a, err := email.ParseAddress(text)
		if err != nil {
			return
		}
		b, err := email.ParseAddress(a.Header())
		if err != nil || a != b {
			t.Fatal("address round trip")
		}
	})
}

func TestAddressJSONBoundsAndPrivateStructuredLogging(t *testing.T) {
	var missing *email.Address
	if missing.UnmarshalJSON([]byte(`"a@example.test"`)) == nil {
		t.Fatal("nil destination accepted")
	}
	var decoded email.Address
	if decoded.UnmarshalJSON([]byte(`"`+strings.Repeat("x", email.MaxAddressBytes*6+1)+`"`)) == nil {
		t.Fatal("oversized raw address JSON accepted")
	}
	if (email.Address{}).JSONContract().Validate() != nil {
		t.Fatal("invalid shared address contract")
	}
	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, nil))
	m, _ := memoryMailer(t)
	logger.Info("safe", "address", address(t, "private@example.test"), "body", email.Body{Text: "private-body"}, "key", email.IdempotencyKey("private-key"), "receipt", email.Receipt{MessageID: "private-id"}, "mailer", m)
	if strings.Contains(output.String(), "private-") || strings.Contains(output.String(), "private@example") {
		t.Fatal("private value logged")
	}
	if strings.Contains(fmt.Sprintf("%+v %#v", m, m), "Private") {
		t.Fatal("mailer formatting exposed content")
	}
	if _, err := json.Marshal(message(t)); err == nil {
		t.Fatal("runtime message silently serialized")
	}
}

func TestAddressRejectsUnicodeControls(t *testing.T) {
	if _, err := email.NewAddress("user@example.test", "before\u0085after"); err == nil {
		t.Fatal("Unicode control accepted in display name")
	}
}
