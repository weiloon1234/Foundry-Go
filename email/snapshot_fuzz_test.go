package email_test

import (
	"encoding/json"
	"testing"

	"github.com/weiloon1234/Foundry-Go/email"
)

func FuzzSnapshotDecode(f *testing.F) {
	f.Add([]byte(`{}`))
	f.Add([]byte(`{"version":1,"from":"a@example.test","to":["b@example.test"],"cc":null,"bcc":null,"reply_to":null,"subject":"x","text":"hello","html":"","headers":null,"attachments":null}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 64<<10 {
			return
		}
		var snapshot email.Snapshot
		if err := json.Unmarshal(data, &snapshot); err != nil {
			return
		}
		message, err := snapshot.Message()
		if err != nil || message.Validate() != nil {
			t.Fatal("decoded invalid message")
		}
		encoded, err := json.Marshal(snapshot)
		if err != nil {
			t.Fatal(err)
		}
		var copy email.Snapshot
		if err := json.Unmarshal(encoded, &copy); err != nil {
			t.Fatal("snapshot roundtrip", err)
		}
	})
}
