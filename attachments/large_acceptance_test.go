package attachments

import (
	"context"
	"errors"
	"github.com/weiloon1234/Foundry-Go/fault"
	"io"
	"runtime"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/extensions"
	"github.com/weiloon1234/Foundry-Go/internal/extensiontest"
	"github.com/weiloon1234/Foundry-Go/internal/upload"
	"github.com/weiloon1234/Foundry-Go/internal/workscope"
	"github.com/weiloon1234/Foundry-Go/model"
)

func TestLargeSlotAcceptsUsesSameBoundedPreparation(t *testing.T) {
	calls, err := workscope.New(1, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	defer calls.Close(t.Context())
	m := &Manager{config: Config{TempDirectory: t.TempDir()}, calls: calls}
	policy := largePolicy()
	policy.MaxBytes = MaxBufferedBytes + 1
	policy.MaxStoredBytes = policy.MaxBytes
	slot := slotDefinition[extensiontest.Member, int64, One[extensiontest.Member]]{
		collection: Define(extensiontest.Members, "accept-large", policy), manager: m,
		binding: extensions.SlotBinding[extensiontest.Member, int64, One[extensiontest.Member]]{
			Field: "File",
			Reference: func(m extensiontest.Member) model.Reference[extensiontest.Member, int64] {
				return extensions.ORMIdentity(extensiontest.Members).Reference(m.ID)
			},
			Get: func(extensiontest.Member) One[extensiontest.Member] { return One[extensiontest.Member]{} },
			Set: func(m extensiontest.Member, _ One[extensiontest.Member]) extensiontest.Member { return m },
		},
	}
	for _, ceiling := range []int64{1024, MaxBufferedBytes + 1} {
		failedPolicy := policy
		failedPolicy.MaxBytes, failedPolicy.MaxStoredBytes = ceiling, ceiling
		slot.collection = Define(extensiontest.Members, "accept-large", failedPolicy)
		accepted, err := slot.accepts(t.Context(), failedAcceptanceFile{})
		if accepted || !errors.Is(err, fault.Invalid) {
			t.Fatal("read failure was normalized as policy rejection", err)
		}
	}
	slot.collection = Define(extensiontest.Members, "accept-large", policy)
	capture, err := upload.New(t.Context(), upload.Config{TempDirectory: t.TempDir(), MaxBytes: policy.MaxBytes + 1, MaxFileBytes: policy.MaxBytes + 1, MaxFiles: 1, MaxReaders: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer capture.Close()
	file, err := capture.Capture(t.Context(), io.LimitReader(repeatedByte('x'), policy.MaxBytes+1), "video.bin", "application/json")
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := slot.accepts(t.Context(), file)
	if err != nil || accepted {
		t.Fatal("Accepts ignored declared ceiling", err)
	}
	assertSpoolEmpty(t, m.config.TempDirectory)
	// Raise the policy to exactly the captured size; no database or storage needed.
	policy.MaxBytes++
	policy.MaxStoredBytes++
	slot.collection = Define(extensiontest.Members, "accept-large", policy)
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	accepted, err = slot.accepts(t.Context(), file)
	runtime.ReadMemStats(&after)
	if err != nil || !accepted {
		t.Fatal("Accepts rejected exact ceiling", err)
	}
	if after.TotalAlloc-before.TotalAlloc > 8<<20 {
		t.Fatal("large Accepts allocated content-sized memory")
	}
	assertSpoolEmpty(t, m.config.TempDirectory)
}

type failedAcceptanceFile struct{ sourceFile }

func (failedAcceptanceFile) Open(context.Context) (io.ReadSeekCloser, error) {
	return failedAcceptanceReader{}, nil
}

type failedAcceptanceReader struct{}

func (failedAcceptanceReader) Read([]byte) (int, error)       { return 0, fault.Invalid }
func (failedAcceptanceReader) Seek(int64, int) (int64, error) { return 0, nil }
func (failedAcceptanceReader) Close() error                   { return nil }
