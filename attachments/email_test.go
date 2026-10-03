package attachments

import (
	"testing"

	"github.com/weiloon1234/Foundry-Go/email"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/storage"
)

type mailOwner struct{}

func TestReadyFilesAndSlotsExposePinnedEmailReferences(t *testing.T) {
	id, err := model.NewID[FileOf[mailOwner]]()
	if err != nil {
		t.Fatal(err)
	}
	key, err := storage.ParseKey("mail/document.txt")
	if err != nil {
		t.Fatal(err)
	}
	file := File[mailOwner]{id: id, disk: "uploads", key: key, etag: `"etag-1"`, info: UploadInfo{OriginalName: "document.txt", MediaType: "text/plain", Size: 10}}
	for _, version := range []storage.VersionID{"", "version-1"} {
		file.version = version
		for _, source := range []email.StoredAttachment{file, Attachment[mailOwner, int64]{File: file}, One[mailOwner]{loaded: true, file: &file}} {
			reference, err := source.EmailAttachment()
			if err != nil || reference.Disk != file.disk || reference.Key != key || reference.Filename != "document.txt" || reference.ContentType != "text/plain" || reference.Version != version {
				t.Fatal("incorrect stored email reference", err)
			}
			if version == "" && reference.IfMatch != file.etag || version != "" && reference.IfMatch != "" {
				t.Fatal("stored version pin lost")
			}
		}
	}
	file.version, file.etag = "", ""
	for _, source := range []email.StoredAttachment{file, File[mailOwner]{}, One[mailOwner]{}, One[mailOwner]{loaded: true}} {
		if _, err := source.EmailAttachment(); err == nil {
			t.Fatal("missing, unloaded or unpinned attachment accepted")
		}
	}
}
