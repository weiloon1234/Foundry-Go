package datatable

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
)

func artifactManager(t *testing.T) *Manager {
	t.Helper()
	config := DefaultConfig()
	config.MaxExports = 1
	config.MaxArtifacts = 1
	config.TempDir = t.TempDir()
	// These lifecycle tests never query the borrowed database.
	manager, err := New(Dependencies{Database: new(database.DB)}, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := manager.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	return manager
}

// openedArtifact publishes a completed file through the same retention and
// publication path Export uses, without a database.
func openedArtifact(t *testing.T, manager *Manager, ctx context.Context) *Artifact {
	t.Helper()
	release, err := manager.beginArtifact(ctx)
	if err != nil {
		t.Fatal(err)
	}
	file, err := os.CreateTemp(manager.config.TempDir, "foundry-report-*")
	if err != nil {
		release()
		t.Fatal(err)
	}
	artifact := &Artifact{file: file, path: file.Name(), release: release, manager: manager, name: "report.csv", media: exportMedia(CSV)}
	t.Cleanup(func() {
		if err := artifact.Close(); err != nil {
			t.Error(err)
		}
	})
	if _, err := file.WriteString("complete\n"); err != nil {
		t.Fatal(err)
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	if err := manager.publish(artifact); err != nil {
		t.Fatal(err)
	}
	return artifact
}

func shortWait(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	t.Cleanup(cancel)
	return ctx
}

func TestArtifactRetainsOnlyItsRetentionSlotUntilClose(t *testing.T) {
	manager := artifactManager(t)
	artifact := openedArtifact(t, manager, t.Context())
	info, err := os.Stat(artifact.path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatal("export file is not private", err)
	}
	if release, err := manager.beginArtifact(shortWait(t)); release != nil || !errors.Is(err, fault.Overloaded) {
		t.Fatal("completed artifact released its retention slot too early", err)
	}
	// Generation capacity is not held for delivery.
	lease, err := manager.exports.Begin(t.Context())
	if err != nil {
		t.Fatal("completed artifact retained generation capacity", err)
	}
	lease.Release()
	data, err := io.ReadAll(artifact)
	if err != nil || string(data) != "complete\n" {
		t.Fatal("artifact could not be streamed", err)
	}
	if offset, err := artifact.Seek(0, io.SeekStart); err != nil || offset != 0 {
		t.Fatal("HTTP range seek failed", err)
	}
	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() {
			if err := artifact.Close(); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if _, err := os.Stat(artifact.path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("closed artifact left a file", err)
	}
	if _, err := artifact.Read(make([]byte, 1)); !errors.Is(err, fault.Closed) {
		t.Fatal("closed artifact remained readable", err)
	}
	release, err := manager.beginArtifact(t.Context())
	if err != nil {
		t.Fatal("close did not release capacity", err)
	}
	release()
	release()
	if manager.retained.Active() != 0 {
		t.Fatal("repeated release changed accounting")
	}
}

func TestManagerShutdownClosesOpenArtifactsWithoutWaitingForOwners(t *testing.T) {
	manager := artifactManager(t)
	artifact := openedArtifact(t, manager, t.Context())
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	if err := manager.Close(canceled); err != nil {
		t.Fatal("shutdown waited for an artifact owner", err)
	}
	if _, err := artifact.Read(make([]byte, 1)); !errors.Is(err, fault.Closed) {
		t.Fatal("shutdown did not invalidate reads", err)
	}
	if _, err := os.Stat(artifact.path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("shutdown left an open artifact's file", err)
	}
	select {
	case <-manager.DoneExports():
	default:
		t.Fatal("completed artifacts held generation shutdown")
	}
	if err := artifact.Close(); err != nil {
		t.Fatal("owner close after shutdown failed", err)
	}
	if release, err := manager.beginArtifact(t.Context()); release != nil || !errors.Is(err, fault.Closed) {
		t.Fatal("closed manager admitted an export", err)
	}
}

func TestArtifactFailedRemovalRemainsOwnedAndBlocksNewExports(t *testing.T) {
	manager := artifactManager(t)
	artifact := openedArtifact(t, manager, t.Context())
	// Replace only this test's temporary path with an owned nonempty directory,
	// producing a deterministic removal failure without OS permission assumptions.
	saved := artifact.path + ".saved"
	if err := os.Rename(artifact.path, saved); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(artifact.path, 0o700); err != nil {
		t.Fatal(err)
	}
	blocker := filepath.Join(artifact.path, "owned-blocker")
	if err := os.WriteFile(blocker, []byte("test"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Register repair before any assertion can fail, so cleanup itself cannot
	// hang or retain an intentionally broken test artifact.
	t.Cleanup(func() { _ = os.Remove(blocker); _ = os.Remove(artifact.path) })
	if err := artifact.Close(); err == nil {
		t.Fatal("native removal failure was hidden")
	}
	if !artifact.closed || artifact.removed || len(manager.pendingCleanup) != 1 || len(manager.open) != 0 {
		t.Fatal("failed cleanup lost manager ownership")
	}
	if release, err := manager.beginArtifact(t.Context()); err == nil || release != nil {
		t.Fatal("new file admitted while cleanup remained broken")
	}
	if err := os.Remove(blocker); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(artifact.path); err != nil {
		t.Fatal(err)
	}
	release, err := manager.beginArtifact(t.Context())
	if err != nil {
		t.Fatal("next export did not retry recovered cleanup", err)
	}
	release()
	if len(manager.pendingCleanup) != 0 || !artifact.removed {
		t.Fatal("successful cleanup retained stale ownership")
	}
	if err := os.Remove(saved); err != nil {
		t.Fatal(err)
	}
}

func TestArtifactShutdownReportsPendingCleanupWithoutLosingIt(t *testing.T) {
	manager := artifactManager(t)
	artifact := openedArtifact(t, manager, t.Context())
	if err := artifact.file.Close(); err != nil {
		t.Fatal(err)
	}
	artifact.closed = true
	if err := os.Remove(artifact.path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(artifact.path, 0o700); err != nil {
		t.Fatal(err)
	}
	blocker := filepath.Join(artifact.path, "owned-blocker")
	if err := os.WriteFile(blocker, []byte("test"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(blocker); _ = os.Remove(artifact.path) })
	if err := artifact.Close(); err == nil {
		t.Fatal("expected cleanup failure")
	}
	if err := manager.Close(t.Context()); err == nil {
		t.Fatal("shutdown hid a remaining temporary file")
	}
	select {
	case <-manager.DoneExports():
	default:
		t.Fatal("closed file retained an unreachable active lease")
	}
	if len(manager.pendingCleanup) != 1 || manager.retained.Active() != 0 {
		t.Fatal("shutdown forgot failed removal or retained its slot")
	}
	if err := os.Remove(blocker); err != nil {
		t.Fatal(err)
	}
	if err := manager.Close(t.Context()); err != nil {
		t.Fatal("shutdown retry did not remove repaired path", err)
	}
}

func TestDeliveryContextRejectsSelfShutdownAndEndsWithManager(t *testing.T) {
	manager := artifactManager(t)
	artifact := openedArtifact(t, manager, t.Context())
	artifact.deadline = time.Now().Add(time.Minute)
	type key struct{}
	delivery, stop := manager.deliveryContext(context.WithValue(t.Context(), key{}, "job"), artifact)
	defer stop()
	if deadline, ok := delivery.Deadline(); !ok || !deadline.Equal(artifact.deadline) || delivery.Value(key{}) != "job" {
		t.Fatal("delivery lost the export deadline or caller values")
	}
	if err := manager.Close(delivery); !errors.Is(err, fault.Cycle) {
		t.Fatal("delivery closed its own manager", err)
	}
	if err := manager.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(delivery.Err(), context.Canceled) {
		t.Fatal("manager shutdown did not end delivery", delivery.Err())
	}
}

type shortWriter struct{}

func (shortWriter) Write(p []byte) (int, error) { return len(p) / 2, nil }

func TestExportWriterBoundsCancellationAndShortWrites(t *testing.T) {
	var output bytes.Buffer
	digest := sha256.New()
	writer := &exportWriter{ctx: t.Context(), file: &output, digest: digest, maximum: 3}
	if n, err := writer.Write([]byte("abc")); err != nil || n != 3 {
		t.Fatal(err)
	}
	if n, err := writer.Write([]byte("d")); err == nil || n != 0 || output.String() != "abc" {
		t.Fatal("oversized chunk was partially published", err)
	}
	want := sha256.Sum256([]byte("abc"))
	if !bytes.Equal(digest.Sum(nil), want[:]) {
		t.Fatal("digest did not match exact file content")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	writer.ctx = ctx
	if n, err := writer.Write(nil); n != 0 || !errors.Is(err, context.Canceled) {
		t.Fatal("canceled writer accepted work", err)
	}
	writer = &exportWriter{ctx: t.Context(), file: shortWriter{}, digest: sha256.New(), maximum: 10}
	if n, err := writer.Write([]byte("abcd")); n != 2 || !errors.Is(err, io.ErrShortWrite) || writer.written != 2 {
		t.Fatal("short write was hidden", err)
	}
}

func TestExportNamesAndMediaTypesRemainSafeAndFormatSpecific(t *testing.T) {
	for _, format := range []ExportFormat{CSV, XLSX} {
		for _, name := range []string{"", "report.csv", "report.xlsx", "../report\r\nX:injected", strings.Repeat("界", 200)} {
			result := exportName(name, "reports.members", format)
			if filepath.Base(result) != result || strings.ContainsAny(result, "\r\n\\") || !strings.HasSuffix(result, "."+string(format)) || len(result) > 255 {
				t.Fatal("unsafe or wrong-format download name")
			}
		}
	}
	if exportName("", "reports.members", CSV) != "reports.members.csv" {
		t.Fatal("semantic table suffix was mistaken for a file extension")
	}
	if exportMedia(CSV) != "text/csv; charset=utf-8" || exportMedia(XLSX) != "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet" {
		t.Fatal("incorrect download media type")
	}
	var absent *Artifact
	if absent.Name() != "" || absent.Size() != 0 || absent.Rows() != 0 || absent.SHA256() != "" || !absent.Modified().IsZero() || absent.EntityTag() != "" || absent.Close() != nil {
		t.Fatal("nil artifact access changed")
	}
}
