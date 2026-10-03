package articles_test

import (
	"context"
	"testing"

	"foundry.test/consumer/articles"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/email"
	"github.com/weiloon1234/Foundry-Go/email/memory"
	"github.com/weiloon1234/Foundry-Go/storage"
)

func TestStoredModelAttachmentsCanBeEmailed(t *testing.T) {
	s := seed(t)
	var row articles.Article
	s.read(t, func(ctx context.Context, tx *database.Tx) error {
		var err error
		row, err = articles.QueryArticles().With(s.x.Logo, s.x.Galleries).RequireFind(ctx, tx, s.full.ID)
		return err
	})
	from, err := email.ParseAddress("sender@example.test")
	if err != nil {
		t.Fatal(err)
	}
	to, err := email.ParseAddress("reader@example.test")
	if err != nil {
		t.Fatal(err)
	}
	message, err := email.NewMessage(from, "Article images", to).Text("Attached").AttachStored(row.Logo)
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range row.Galleries.All() {
		message, err = message.AttachStored(file)
		if err != nil {
			t.Fatal(err)
		}
	}
	snapshot, err := email.CaptureMessage(message)
	if err != nil {
		t.Fatal("stored attachments were not durable", err)
	}
	message, err = snapshot.Message()
	if err != nil {
		t.Fatal(err)
	}
	driver, err := memory.New(1)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(driver.Close)
	mailer, err := email.New(driver, s.services.Storage, email.DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := mailer.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	if _, err := mailer.Send(t.Context(), message, email.SendOptions{}); err != nil {
		t.Fatal(err)
	}
	files := driver.Messages()[0].Attachments()
	if len(files) != 3 {
		t.Fatal("stored slot files were omitted")
	}
	for _, file := range files {
		ref := file.Reference()
		if ref.Disk != articles.Files.ID() || ref.ContentType != "image/png" || ref.IfMatch == "" && ref.Version == "" || file.Size() == 0 {
			t.Fatal("stored metadata or content pin was lost")
		}
	}
	// A reference never silently switches to replacement bytes after capture.
	disk, err := s.services.Storage.Disk(articles.Files.ID())
	if err != nil {
		t.Fatal(err)
	}
	if err := disk.Delete(t.Context(), files[0].Reference().Key, storage.DeleteOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := mailer.Send(t.Context(), message, email.SendOptions{}); err == nil {
		t.Fatal("deleted stored content sent successfully")
	}
	if len(driver.Messages()) != 1 {
		t.Fatal("unavailable stored content reached transport")
	}
}
