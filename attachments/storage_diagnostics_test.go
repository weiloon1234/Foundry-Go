package attachments

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/storage"
)

func TestAttachmentStorageDiagnosticsSurviveCallbacks(t *testing.T) {
	for _, mode := range []string{"read limit", "stream integrity", "applied cleanup"} {
		for _, exit := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/goexit=%t", mode, exit), func(t *testing.T) {
				cause := &privateAttachmentError{exit: exit}
				var original error
				code, operation, outcome := storage.LimitExceeded, storage.OpenOperation, storage.NotApplicable
				switch mode {
				case "read limit":
					_, original = readBytes(t.Context(), nil, Attachment[struct{}, int]{}, 0)
				case "stream integrity":
					reader := &verifiedReader{body: io.NopCloser(strings.NewReader("")), digest: sha256.New(), remaining: 1}
					_, original = reader.Read(make([]byte, 1))
					code = storage.IntegrityFailed
				case "applied cleanup":
					code, operation, outcome = storage.IntegrityFailed, storage.PutOperation, storage.Applied
					original = storageFailure(code, operation, outcome, cause)
				}
				joined := joinFailures(original, cause)
				err := callback.Isolated("attachment operation", func() error { return joined })
				want := "attachment operation: " + string(code) + ": " + string(operation) + "\nattachment operation failed"
				var detail *storage.Error
				var retained *privateAttachmentError
				if err == nil || err.Error() != want || !errors.Is(err, original) || !errors.Is(err, cause) || !errors.Is(err, code) || !errors.As(err, &detail) || detail.Operation() != operation || detail.Outcome() != outcome || !errors.As(err, &retained) || retained != cause {
					t.Fatal("attachment storage diagnostic lost identity, classification or outcome")
				}
				*detail = *storage.Failure(storage.Code("private mutation"), storage.Operation("private operation"), storage.Unknown, cause)
				if fmt.Sprintf("%v %+v %#v", err, err, err) != want+" "+want+" "+want || cause.calls.Load() != 0 {
					t.Fatal("attachment storage diagnostic formatted a private error")
				}
			})
		}
	}
}
