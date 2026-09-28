package storage_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/weiloon1234/Foundry-Go/storage"
)

type cyclicStorageError struct{ visits atomic.Int32 }

func (*cyclicStorageError) Error() string { panic("private storage error formatted") }
func (e *cyclicStorageError) Unwrap() error {
	// Bound old implementations too, so regressions fail rather than hang.
	if e.visits.Add(1) > 512 {
		return nil
	}
	return e
}
func assertStorageCycleBound(t *testing.T, cycle *cyclicStorageError) {
	t.Helper()
	if cycle.visits.Load() == 0 || cycle.visits.Load() > 256 {
		t.Fatal("storage error inspection was not bounded", cycle.visits.Load())
	}
}
func assertStorageFailure(t *testing.T, err error, outcome storage.Outcome) {
	t.Helper()
	detail, ok := err.(*storage.Error)
	if !ok || detail.Code() != storage.Unavailable || detail.Outcome() != outcome {
		t.Fatal("storage failure lost conservative outcome")
	}
}

func TestCyclicStoragePutErrorReleasesCapacityAndKeepsUnknownOutcome(t *testing.T) {
	for _, canceled := range []bool{false, true} {
		t.Run(fmt.Sprint("canceled=", canceled), func(t *testing.T) {
			cycle := &cyclicStorageError{}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			var calls atomic.Int32
			disk := diskFor(t, &backend{put: func(_ context.Context, key storage.ObjectKey, source io.Reader, _ storage.PutOptions) (storage.ObjectInfo, error) {
				if calls.Add(1) == 1 {
					if canceled {
						cancel()
					}
					return storage.ObjectInfo{}, cycle
				}
				data, err := io.ReadAll(source)
				return metadata(key, int64(len(data))), err
			}}, 1)
			result, err := disk.Put(ctx, testKey(t), strings.NewReader("candidate"), storage.PutOptions{})
			assertStorageFailure(t, err, storage.Unknown)
			assertStorageCycleBound(t, cycle)
			if result != (storage.StoredObject{}) || disk.Stats().Active != 0 {
				t.Fatal("failed write retained capacity or returned a published object")
			}
			// Cancellation is joined before the private cause, so it is safe to inspect.
			if canceled && !errors.Is(err, context.Canceled) {
				t.Fatal("late cancellation was lost")
			}
			if _, err := disk.Put(t.Context(), testKey(t), strings.NewReader("next"), storage.PutOptions{}); err != nil {
				t.Fatal("later write did not acquire capacity")
			}
			if err := disk.Close(t.Context()); err != nil {
				t.Fatal(err)
			}
			select {
			case <-disk.Done():
			default:
				t.Fatal("disk retained shutdown ownership")
			}
		})
	}
}

type failingStorageReader struct{ failure error }

func (r failingStorageReader) Read([]byte) (int, error) { return 0, r.failure }
func TestCyclicStorageOpenAndReadErrorsReleaseTheirBodies(t *testing.T) {
	for _, duringRead := range []bool{false, true} {
		t.Run(fmt.Sprint("during-read=", duringRead), func(t *testing.T) {
			cycle := &cyclicStorageError{}
			raw := &body{Reader: strings.NewReader("abc")}
			if duringRead {
				raw.Reader = failingStorageReader{cycle}
			}
			disk := diskFor(t, &backend{open: func(_ context.Context, key storage.ObjectKey, _ storage.ReadOptions) (io.ReadCloser, storage.ReadInfo, error) {
				info := storage.ReadInfo{Object: metadata(key, 3), Length: 3}
				if duringRead {
					return raw, info, nil
				}
				return raw, info, cycle
			}}, 1)
			reader, _, err := disk.Open(t.Context(), testKey(t), storage.ReadOptions{})
			if duringRead {
				if err != nil || reader == nil {
					t.Fatal("valid open failed")
				}
				_, err = reader.Read(make([]byte, 3))
				assertStorageFailure(t, err, storage.NotApplicable)
				if disk.Stats().Active != 1 {
					t.Fatal("open reader lost ownership before close")
				}
				if err := reader.Close(); err != nil {
					t.Fatal(err)
				}
			} else {
				assertStorageFailure(t, err, storage.NotApplicable)
				if reader != nil {
					t.Fatal("failed open returned a reader")
				}
			}
			assertStorageCycleBound(t, cycle)
			if raw.closes.Load() != 1 || disk.Stats().Active != 0 {
				t.Fatal("failure leaked body or admission")
			}
			if err := disk.Close(t.Context()); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestWrappedStorageOutcomeAndCleanupSurviveLateCancellation(t *testing.T) {
	for _, outcome := range []storage.Outcome{storage.Unchanged, storage.Applied, storage.Unknown} {
		t.Run(fmt.Sprint(outcome), func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			cleanup := storage.NewCleanupID("private-upload")
			disk := diskFor(t, &backend{put: func(context.Context, storage.ObjectKey, io.Reader, storage.PutOptions) (storage.ObjectInfo, error) {
				cancel()
				return storage.ObjectInfo{}, fmt.Errorf("wrapped: %w", storage.Failure(storage.Unavailable, storage.PutOperation, outcome, nil).WithCleanup(cleanup))
			}}, 1)
			_, err := disk.Put(ctx, testKey(t), strings.NewReader("candidate"), storage.PutOptions{})
			assertStorageFailure(t, err, outcome)
			detail := err.(*storage.Error)
			got, ok := detail.Cleanup().Get()
			if !ok || got.Value() != cleanup.Value() || !errors.Is(err, context.Canceled) {
				t.Fatal("known outcome lost cleanup or cancellation")
			}
		})
	}
}
