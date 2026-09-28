package email_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/email"
	"github.com/weiloon1234/Foundry-Go/storage"
)

func TestDurableMessageSnapshotIsExplicitImmutableAndPinned(t *testing.T) {
	from, err := email.ParseAddress("from@example.test")
	if err != nil {
		t.Fatal(err)
	}
	to, err := email.ParseAddress("private@example.test")
	if err != nil {
		t.Fatal(err)
	}
	message := email.NewMessage(from, "private subject", to).Text("private body").Header("X-Example", "before")
	snapshot, err := email.CaptureMessage(message)
	if err != nil {
		t.Fatal(err)
	}
	message = message.Text("changed").Header("X-Example", "changed")
	data, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	var restored email.Snapshot
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatal(err)
	}
	got, err := restored.Message()
	if err != nil || got.TextBody() != "private body" || got.Headers()["X-Example"] != "before" || got.To()[0] != to {
		t.Fatal("snapshot changed", err)
	}
	if _, err := json.Marshal(message); err == nil {
		t.Fatal("runtime message became implicitly serializable")
	}
	if strings.Contains(fmt.Sprintf("%#v", snapshot), "private") {
		t.Fatal("snapshot formatting exposed content")
	}
	if err := json.Unmarshal([]byte(`{"version":9}`), &restored); err == nil {
		t.Fatal("invalid snapshot accepted")
	}
	if got, err := restored.Message(); err != nil || got.TextBody() != "private body" {
		t.Fatal("failed decode replaced value")
	}
	key, err := storage.ParseKey("file.txt")
	if err != nil {
		t.Fatal(err)
	}
	attachment := email.Attachment{Disk: "private", Key: key, Filename: "file.txt", ContentType: "text/plain"}
	if _, err := email.CaptureMessage(message.Attach(attachment)); err == nil {
		t.Fatal("mutable queued attachment accepted")
	}
	attachment.IfMatch = `"version-one"`
	if _, err := email.CaptureMessage(message.Attach(attachment)); err != nil {
		t.Fatal(err)
	}
}
