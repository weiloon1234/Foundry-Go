package attachments

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	stdhttp "net/http"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/extensions"
	"github.com/weiloon1234/Foundry-Go/imaging"
	"github.com/weiloon1234/Foundry-Go/internal/extensiontest"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/storage"
)

type sourceFile struct {
	name, hint string
	data       []byte
}

func (f sourceFile) Open(context.Context) (io.ReadSeekCloser, error) {
	return readSeekCloser{bytes.NewReader(f.data)}, nil
}
func (f sourceFile) Name() string              { return f.name }
func (f sourceFile) ClientContentType() string { return f.hint }

type readSeekCloser struct{ *bytes.Reader }

func (readSeekCloser) Close() error { return nil }

func officeDocument(t *testing.T) []byte {
	t.Helper()
	var document bytes.Buffer
	writer := zip.NewWriter(&document)
	for _, name := range []string{"[Content_Types].xml", "word/document.xml"} {
		part, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = part.Write([]byte("<x/>"))
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return document.Bytes()
}

// Accepts shares write-time detection: every file it accepts publishes and
// every file it rejects fails the write, including an OOXML document that
// HTTP's generic content sniff reports as a ZIP archive.
func TestPostgresSlotAcceptsAgreesWithWrites(t *testing.T) {
	const docx = storage.MediaType("application/vnd.openxmlformats-officedocument.wordprocessingml.document")
	binding := extensions.SlotBinding[extensiontest.Member, int64, Many[extensiontest.Member]]{
		Field: "Documents",
		Reference: func(m extensiontest.Member) model.Reference[extensiontest.Member, int64] {
			return extensions.ORMIdentity(extensiontest.Members).Reference(m.ID)
		},
		Get: func(extensiontest.Member) Many[extensiontest.Member] { return Many[extensiontest.Member]{} },
		Set: func(m extensiontest.Member, _ Many[extensiontest.Member]) extensiontest.Member { return m },
	}
	documents := ManyOf(extensiontest.Members, "slot-documents", Policy{Disk: testDisk, Accepted: []storage.MediaType{docx, "text/plain"}, MaxFiles: 8}, binding)
	f := openAttachments(t, documents.Registration())
	documents = documents.From(f.manager)
	owner := extensiontest.Member{ID: 3}
	office := officeDocument(t)
	if detected := stdhttp.DetectContentType(office); detected != "application/zip" {
		t.Fatal("generic sniff classification changed", detected)
	}
	for name, test := range map[string]struct {
		file   sourceFile
		accept bool
	}{
		"office document": {sourceFile{"report.docx", "application/zip", office}, true},
		"plain text":      {sourceFile{"notes.txt", "text/plain; charset=utf-8", []byte("hello")}, true},
		"svg":             {sourceFile{"logo.svg", "image/svg+xml", []byte(`<svg xmlns="http://www.w3.org/2000/svg"></svg>`)}, false},
		"png":             {sourceFile{"image.png", "image/png", []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR")}, false},
		"path in name":    {sourceFile{"../notes.txt", "text/plain", []byte("hello")}, false},
	} {
		t.Run(name, func(t *testing.T) {
			accepted, err := documents.Accepts(t.Context(), test.file)
			if err != nil || accepted != test.accept {
				t.Fatalf("Accepts=%t err=%v, want %t", accepted, err, test.accept)
			}
			result, err := documents.AddFile(t.Context(), owner, test.file)
			if (err == nil) != test.accept {
				t.Fatalf("write outcome %v disagrees with Accepts", err)
			}
			if test.accept && result.Publication != Published {
				t.Fatal("an accepted file was not published", result.Publication)
			}
		})
	}
	if _, err := documents.From(nil).Accepts(t.Context(), sourceFile{"notes.txt", "", []byte("x")}); err == nil {
		t.Fatal("an unbound slot checked a file")
	}
}

// Links derived from loaded slots equal the collection's links for the same
// files, in collection order, without further database or storage I/O.
func TestPostgresSlotLinksMatchCollectionLinks(t *testing.T) {
	f := openAttachments(t)
	type ownerModel struct {
		ID      int64
		Gallery Many[ownerModel]
	}
	type ownerTable struct {
		orm.Table[ownerModel, int64]
		Gallery orm.Slot[ownerModel, Many[ownerModel]]
	}
	fields := orm.Define("extension_members", func(b orm.Builder[ownerModel], m *ownerModel) ownerTable {
		return ownerTable{orm.TableOf(b, &m.ID), orm.SlotOf(b, &m.Gallery)}
	})
	owners := extensions.DefineORMOwner("members", orm.IdentityOf(fields.Table, fields.Key()), extensions.OwnerOptions{})
	// Register this concrete slot owner; a same-named declaration is not a capability.
	registry, err := extensions.NewRegistry(owners.Registration())
	if err != nil {
		t.Fatal(err)
	}
	settings := extensions.DefaultConfig()
	settings.Schema = f.Schema
	f.Store, err = extensions.New(f.DB, registry, settings)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := f.Store.Close(ctx); err != nil {
			t.Error(err)
		}
	})
	gallery := ManyOf(owners, "slot-gallery", Policy{Disk: testDisk, Accepted: []storage.MediaType{"text/plain"}, MaxFiles: 4}, extensions.BindSlot(fields.Key(), fields.Gallery))
	m, _, _ := linkedManager(t, f, gallery.Registration())
	gallery = gallery.From(m)
	owner := ownerModel{ID: 4}
	if _, err := gallery.PublicURLs(t.Context(), owner); err == nil {
		t.Fatal("an unloaded slot produced links")
	}
	for _, name := range []string{"first.txt", "second.txt"} {
		if _, err := gallery.AddFile(t.Context(), owner, sourceFile{name, "text/plain", []byte(name)}); err != nil {
			t.Fatal(err)
		}
	}
	loaded, err := orm.LoadFrom(t.Context(), f.DB, fields.Query().With(gallery.Relation(fields.Gallery)), []ownerModel{owner})
	if err != nil {
		t.Fatal(err)
	}
	owner = loaded[0]
	urls, err := gallery.PublicURLs(t.Context(), owner)
	if err != nil {
		t.Fatal(err)
	}
	files, err := ManyCollection(gallery).List(t.Context(), m, extensions.ORMIdentity(owners).Reference(owner.ID))
	if err != nil {
		t.Fatal(err)
	}
	want, err := ManyCollection(gallery).PublicURLsOf(t.Context(), m, files)
	if err != nil || len(urls) != 2 || urls[0] != want[0] || urls[1] != want[1] {
		t.Fatal("slot links differ from collection links", urls, want, err)
	}
	first, _ := owner.Gallery.Get()
	if url, err := gallery.URL(t.Context(), owner, first[0]); err != nil || url != want[0] {
		t.Fatal("single slot link", url, err)
	}
	if signed, err := gallery.TemporaryURL(t.Context(), owner, first[0], storage.LinkOptions{ExpiresIn: time.Minute}); err != nil || signed.URL() == "" {
		t.Fatal("temporary slot link", err)
	}
	if _, err := gallery.VariantURL(t.Context(), owner, first[0], DefineVariant("preview", imaging.NewPlan().Fit(8, 8, false))); err == nil {
		t.Fatal("an undeclared variant produced a link")
	}
}
