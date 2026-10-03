package email_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"runtime"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/email"
	"github.com/weiloon1234/Foundry-Go/email/memory"
	"github.com/weiloon1234/Foundry-Go/internal/upload"
)

type mailUpload struct {
	name, hint string
	open       func(context.Context) (io.ReadSeekCloser, error)
}

func (f mailUpload) Name() string                                        { return f.name }
func (f mailUpload) ClientContentType() string                           { return f.hint }
func (f mailUpload) Open(ctx context.Context) (io.ReadSeekCloser, error) { return f.open(ctx) }

type mailUploadReader struct {
	*strings.Reader
	closed   bool
	read     func([]byte) (int, error)
	closeErr error
}

func (r *mailUploadReader) Read(p []byte) (int, error) {
	if r.read != nil {
		return r.read(p)
	}
	return r.Reader.Read(p)
}
func (r *mailUploadReader) Close() error { r.closed = true; return r.closeErr }

type storedMailFile struct{ reference email.Attachment }

func (f storedMailFile) EmailAttachment() (email.Attachment, error) { return f.reference, nil }

func TestEmailUploadSurvivesRequestCleanupWithoutStorage(t *testing.T) {
	directory := t.TempDir()
	batch, err := upload.New(t.Context(), upload.Config{TempDirectory: directory, MaxBytes: 1024, MaxFileBytes: 1024, MaxFiles: 1, MaxReaders: 1})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := batch.Close(); err != nil {
			t.Error(err)
		}
	})
	file, err := batch.Capture(t.Context(), strings.NewReader("uploaded document"), "document.txt", "image/png")
	if err != nil {
		t.Fatal(err)
	}
	original := message(t)
	message, err := original.AttachUpload(t.Context(), file)
	if err != nil {
		t.Fatal(err)
	}
	if len(original.DataAttachments()) != 0 || len(message.DataAttachments()) != 1 || message.DataAttachments()[0].ContentType() != "text/plain" {
		t.Fatal("upload mutated original message or trusted client MIME hint")
	}
	// MaxReaders is one: reopening proves AttachUpload released its reader.
	reader, err := file.Open(t.Context())
	if err != nil {
		t.Fatal("email retained the request reader", err)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	if err := batch.Close(); err != nil {
		t.Fatal(err)
	}
	m, memory := memoryMailer(t)
	if _, err := m.Send(t.Context(), message, email.SendOptions{}); err != nil {
		t.Fatal(err)
	}
	if got := memory.Messages()[0].Attachments(); len(got) != 1 || !bytes.Equal(got[0].Bytes(), []byte("uploaded document")) {
		t.Fatal("request cleanup lost upload bytes")
	}
	if _, err := email.CaptureMessage(message); !errors.Is(err, email.Construction) {
		t.Fatal("runtime upload silently became durable", err)
	}
	if _, err := original.AttachUpload(t.Context(), file); !errors.Is(err, email.Construction) {
		t.Fatal("expired upload accepted", err)
	}
}

func TestEmailUploadBoundsReaderOwnershipAndFailures(t *testing.T) {
	for _, name := range []string{"empty", "oversize", "read-error", "open-error", "nil-reader", "close-error", "panic", "goexit", "cancel"} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			reader := &mailUploadReader{Reader: strings.NewReader("content")}
			file := mailUpload{name: "document.txt", hint: "text/plain", open: func(context.Context) (io.ReadSeekCloser, error) { return reader, nil }}
			switch name {
			case "empty":
				reader.Reader = strings.NewReader("")
			case "oversize":
				reader.Reader = strings.NewReader(strings.Repeat("a", email.MaxDataAttachmentBytes+1))
			case "read-error":
				reader.read = func([]byte) (int, error) { return 0, errors.New("private reader failure") }
			case "open-error":
				file.open = func(context.Context) (io.ReadSeekCloser, error) { return reader, errors.New("private open failure") }
			case "nil-reader":
				file.open = func(context.Context) (io.ReadSeekCloser, error) { return nil, nil }
			case "close-error":
				reader.closeErr = errors.New("private close failure")
			case "panic":
				reader.read = func([]byte) (int, error) { panic("private panic") }
			case "goexit":
				reader.read = func([]byte) (int, error) { runtime.Goexit(); return 0, nil }
			case "cancel":
				reader.read = func([]byte) (int, error) { cancel(); return 0, ctx.Err() }
			}
			result, err := message(t).AttachUpload(ctx, file)
			want := email.Construction
			if name == "cancel" {
				want = email.Transient
			}
			if !errors.Is(err, want) || len(result.DataAttachments()) != 0 || name != "nil-reader" && !reader.closed {
				t.Fatal("upload failure lost cleanup or exposed partial data", err)
			}
			if strings.Contains(err.Error(), "private") {
				t.Fatal("upload error leaked")
			}
		})
	}
}

func TestEmailStoredAttachmentSnapshotsPinnedReference(t *testing.T) {
	registry, reference := attachmentStore(t)
	file := storedMailFile{reference}
	before := message(t)
	attached, err := before.AttachStored(file)
	if err != nil {
		t.Fatal(err)
	}
	file.reference.Filename = "changed.txt"
	if len(before.Attachments()) != 0 || attached.Attachments()[0] != reference {
		t.Fatal("stored reference was not snapshotted")
	}
	snapshot, err := email.CaptureMessage(attached)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := snapshot.Message()
	if err != nil {
		t.Fatal(err)
	}
	driver, err := memory.New(1)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(driver.Close)
	m := mailer(t, driver, registry, nil)
	if _, err := m.Send(t.Context(), restored, email.SendOptions{}); err != nil {
		t.Fatal(err)
	}
	if string(driver.Messages()[0].Attachments()[0].Bytes()) != "private attachment" {
		t.Fatal("stored file was not resolved by the shared mailer")
	}
	reference.Version, reference.IfMatch = "", ""
	if _, err := before.AttachStored(storedMailFile{reference}); !errors.Is(err, email.Construction) {
		t.Fatal("unpinned stored file accepted", err)
	}
	if _, err := before.AttachStored(nil); !errors.Is(err, email.Construction) {
		t.Fatal("nil stored file accepted", err)
	}
}
