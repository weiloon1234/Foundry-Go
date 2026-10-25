package local_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/storage"
)

func TestLocalDiagnosticsSurviveDiskAndConsumerBoundaries(t *testing.T) {
	for _, operation := range []string{"open", "stat", "conditional put"} {
		t.Run(operation, func(t *testing.T) {
			disk, _ := openLocal(t, t.TempDir())
			object := key(t, "diagnostic/object")
			if operation == "conditional put" {
				if _, err := disk.PutBytes(t.Context(), object, []byte("existing"), storage.PutOptions{}); err != nil {
					t.Fatal(err)
				}
			}
			var original error
			err := callback.Isolated("consumer", func() error {
				switch operation {
				case "open":
					_, _, original = disk.Open(t.Context(), object, storage.ReadOptions{})
				case "stat":
					_, original = disk.Stat(t.Context(), object, storage.ReadOptions{})
				default:
					_, original = disk.PutBytes(t.Context(), object, []byte("replacement"), storage.PutOptions{Condition: storage.IfAbsent()})
				}
				return original
			})
			code, op, outcome := storage.NotFound, storage.OpenOperation, storage.NotApplicable
			want := "consumer: storage open: storage_not_found: open"
			if operation == "stat" {
				op = storage.StatOperation
				want = "consumer: storage stat: storage_not_found: open"
			}
			if operation == "conditional put" {
				code, op, outcome = storage.PreconditionFailed, storage.PutOperation, storage.Unchanged
				want = "consumer: storage put: storage_precondition_failed: put"
			}
			var detail *storage.Error
			if err == nil || err.Error() != want || !errors.Is(err, original) || !errors.Is(err, code) || !errors.As(err, &detail) || detail.Operation() != op || detail.Outcome() != outcome {
				t.Fatal("local diagnostic lost classification, identity or outcome")
			}
			*detail = *storage.Failure(storage.Code("private mutation"), storage.Operation("private operation"), storage.Unknown, nil)
			if fmt.Sprintf("%v %+v %#v", err, err, err) != want+" "+want+" "+want || disk.Stats().Active != 0 {
				t.Fatal("local diagnostic was mutable or retained capacity")
			}
			if operation == "conditional put" {
				data, _, err := disk.ReadBytes(t.Context(), object, 64, storage.ReadOptions{})
				if err != nil || string(data) != "existing" {
					t.Fatal("failed conditional publication changed the existing object")
				}
			}
		})
	}
}
