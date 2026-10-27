package storage_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/storage"
)

func TestStorageSourceDiagnosticsCrossCallbacks(t *testing.T) {
	for _, test := range []struct {
		name, message string
		code          storage.Code
		operation     storage.Operation
		outcome       storage.Outcome
		run           func() error
	}{
		{"configuration", "storage_invalid: open", storage.Invalid, storage.OpenOperation, storage.NotApplicable, func() error { return (storage.Config{}).Validate() }},
		{"range", "storage_invalid: open", storage.Invalid, storage.OpenOperation, storage.NotApplicable, func() error { return (storage.ByteRange{Offset: -1}).Validate() }},
		{"capability", "storage_unsupported: put", storage.Unsupported, storage.PutOperation, storage.Unchanged, func() error {
			return (storage.Capabilities{}).ValidatePut(storage.PutOptions{Condition: storage.IfAbsent()})
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			original := test.run()
			var raw *storage.Error
			err := callback.Isolated("consumer", func() error { return original })
			want := "consumer: " + test.message
			if err == nil || err.Error() != want || !errors.Is(err, original) || !errors.Is(err, test.code) || !errors.As(err, &raw) || raw.Operation() != test.operation || raw.Outcome() != test.outcome {
				t.Fatal("storage source lost its diagnostic, identity or outcome")
			}
			*raw = *storage.Failure(storage.Code("private replacement"), storage.Operation("private operation"), storage.Unknown, nil)
			if fmt.Sprintf("%v %+v %#v", err, err, err) != want+" "+want+" "+want {
				t.Fatal("storage diagnostic followed a mutated public error")
			}
		})
	}
}

type privateStorageDiagnostic struct {
	exit  bool
	calls atomic.Int32
}

func (e *privateStorageDiagnostic) Error() string {
	e.calls.Add(1)
	if e.exit {
		runtime.Goexit()
	}
	panic("private storage diagnostic")
}
func (e *privateStorageDiagnostic) Format(fmt.State, rune) { _ = e.Error() }

func TestStorageOutcomeDiagnosticsDoNotTrustPublicFailures(t *testing.T) {
	for _, typed := range []bool{false, true} {
		for _, exit := range []bool{false, true} {
			t.Run(fmt.Sprintf("typed=%t/goexit=%t", typed, exit), func(t *testing.T) {
				cause := &privateStorageDiagnostic{exit: exit}
				var original error = cause
				cleanup := storage.NewCleanupID("private upload reference")
				code, outcome := storage.Unavailable, storage.Unknown
				if typed {
					code, outcome = storage.Code("private classification"), storage.Applied
					original = storage.Failure(code, storage.Operation("private operation"), outcome, cause).WithCleanup(cleanup)
				}
				disk := diskFor(t, &backend{put: func(context.Context, storage.ObjectKey, io.Reader, storage.PutOptions) (storage.ObjectInfo, error) {
					return storage.ObjectInfo{}, original
				}}, 1)
				err := callback.Isolated("consumer", func() error {
					_, err := disk.Put(t.Context(), testKey(t), strings.NewReader("payload"), storage.PutOptions{})
					return err
				})
				want := "consumer: storage_unavailable: put"
				if typed {
					want = "consumer failed"
				}
				var detail *storage.Error
				var retained *privateStorageDiagnostic
				if err == nil || err.Error() != want || !errors.Is(err, original) || !errors.Is(err, cause) || !errors.Is(err, code) || !errors.As(err, &detail) || detail.Outcome() != outcome || detail.Operation() != storage.PutOperation || !errors.As(err, &retained) || retained != cause {
					t.Fatal("storage presentation changed identity, classification or publication outcome")
				}
				if typed {
					if id, ok := detail.Cleanup().Get(); !ok || id != cleanup {
						t.Fatal("cleanup reference lost")
					}
				}
				*detail = *storage.Failure(storage.Code("private mutation"), storage.Operation("private operation"), storage.Unknown, cause)
				if fmt.Sprintf("%v %+v %#v", err, err, err) != want+" "+want+" "+want || cause.calls.Load() != 0 || disk.Stats().Active != 0 {
					t.Fatal("storage diagnostic formatted private text or retained admission")
				}
			})
		}
	}
}
