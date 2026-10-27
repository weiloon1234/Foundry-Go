package s3_test

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/storage"
)

func TestS3DiagnosticsSurviveSDKDiskAndConsumerBoundaries(t *testing.T) {
	for _, test := range []struct {
		name     string
		status   int
		provider string
		code     storage.Code
		outcome  storage.Outcome
	}{
		{"missing", 404, "NoSuchKey", storage.NotFound, storage.NotApplicable},
		{"forbidden", 403, "AccessDenied", storage.Forbidden, storage.NotApplicable},
		{"conditional publication", 412, "PreconditionFailed", storage.PreconditionFailed, storage.Unchanged},
	} {
		t.Run(test.name, func(t *testing.T) {
			disk, _ := wireDisk(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { wireError(w, test.status, test.provider) }), nil)
			var original error
			err := callback.Isolated("consumer", func() error {
				if test.name == "conditional publication" {
					_, original = disk.PutBytes(t.Context(), objectKey(t), []byte("payload"), storage.PutOptions{Condition: storage.IfAbsent()})
				} else {
					_, _, original = disk.Open(t.Context(), objectKey(t), storage.ReadOptions{})
				}
				return original
			})
			op := storage.OpenOperation
			if test.name == "conditional publication" {
				op = storage.PutOperation
			}
			want := "consumer: storage " + string(op) + ": " + string(test.code) + ": " + string(op)
			var detail *storage.Error
			if err == nil || err.Error() != want || !errors.Is(err, original) || !errors.Is(err, test.code) || !errors.As(err, &detail) || detail.Operation() != op || detail.Outcome() != test.outcome {
				t.Fatal("S3 diagnostic lost identity, classification or outcome")
			}
			*detail = *storage.Failure(storage.Code("private mutation"), storage.Operation("private operation"), storage.Unknown, nil)
			if fmt.Sprintf("%v %+v %#v", err, err, err) != want+" "+want+" "+want || disk.Stats().Active != 0 {
				t.Fatal("S3 diagnostic exposed mutable/provider text or retained admission")
			}
		})
	}
}
