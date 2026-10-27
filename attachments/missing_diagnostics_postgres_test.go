package attachments

import (
	"errors"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/extensiontest"
	"github.com/weiloon1234/Foundry-Go/model"
)

func TestMissingAttachmentDiagnosticsAcrossReadPipeline(t *testing.T) {
	f := openAttachments(t)
	missing, err := model.NewIDAt[FileOf[extensiontest.Member]](time.Now())
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, message string
		run           func() error
	}{
		{"batch owner", "consumer: not_found", func() error {
			batch, err := testSingle.Load(t.Context(), f.manager, []model.Reference[extensiontest.Member, int64]{member(t, 1)})
			if err != nil {
				return err
			}
			_, err = batch.Get(member(t, 99))
			return err
		}},
		{"missing file", "consumer: attachment lookup: not_found", func() error {
			_, err := testSingle.Find(t.Context(), f.manager, member(t, 1), missing)
			return err
		}},
		{"missing owner", "consumer: attachment lookup: not_found", func() error {
			_, err := testSingle.Find(t.Context(), f.manager, member(t, 99), missing)
			return err
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			source := test.run()
			got := callback.Isolated("consumer", func() error { return source })
			if got == nil || got.Error() != test.message || !errors.Is(got, source) || !errors.Is(got, database.NotFound) {
				t.Fatal("missing attachment diagnostic, identity or classification was lost")
			}
		})
	}
}
