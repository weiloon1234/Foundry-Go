package s3_test

import (
	"io"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/weiloon1234/Foundry-Go/storage"
	"github.com/weiloon1234/Foundry-Go/storage/s3"
)

type malformedUploadError struct {
	mode   string
	visits atomic.Int32
}

func (*malformedUploadError) Error() string { panic("private upload error formatted") }
func (e *malformedUploadError) Is(error) bool {
	switch e.mode {
	case "panic":
		panic("private classification panic")
	case "goexit":
		runtime.Goexit()
	}
	return false
}
func (e *malformedUploadError) Unwrap() error {
	// Finite escape prevents old implementations from hanging the test runner.
	if e.visits.Add(1) > 4096 {
		return nil
	}
	return e
}

type rejectedUploadSource struct{ err error }

func (s rejectedUploadSource) Read([]byte) (int, error) { return 0, s.err }

func TestMalformedSourceErrorsAbortMultipartAndReleaseCapacity(t *testing.T) {
	for _, mode := range []string{"cycle", "panic", "goexit"} {
		t.Run(mode, func(t *testing.T) {
			peer := &uploadPeer{}
			disk := uploadDisk(t, peer)
			malformed := &malformedUploadError{mode: mode}
			source := io.MultiReader(&zeroSource{remaining: s3.MinPartBytes + 1}, rejectedUploadSource{malformed})
			_, err := disk.Put(t.Context(), objectKey(t), source, storage.PutOptions{})
			detail, ok := err.(*storage.Error)
			if !ok || detail.Code() != storage.Unavailable || detail.Outcome() != storage.Unchanged {
				t.Fatal("failed staging lost unchanged publication outcome")
			}
			if mode == "cycle" && (malformed.visits.Load() == 0 || malformed.visits.Load() > 256) {
				t.Fatal("source error traversal was not bounded", malformed.visits.Load())
			}
			peer.mu.Lock()
			creates, parts, aborts, completes, active := peer.creates, peer.parts, peer.aborts, peer.completes, peer.active
			peer.mu.Unlock()
			if creates != 1 || parts != 1 || aborts != 1 || completes != 0 || active {
				t.Fatal("failed source escaped multipart cleanup", creates, parts, aborts, completes, active)
			}
			if disk.Stats().Active != 0 {
				t.Fatal("failed source retained disk capacity")
			}
			if _, err := disk.Put(t.Context(), objectKey(t), strings.NewReader("next"), storage.PutOptions{}); err != nil {
				t.Fatal("later upload could not use released adapter capacity", err)
			}
		})
	}
}
