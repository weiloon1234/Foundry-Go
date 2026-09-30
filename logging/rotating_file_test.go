package logging

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
)

func rotationTime() time.Time { return time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC) }

func testRotatingFile(t *testing.T, policy RotationConfig) (*rotatingFile, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "app.jsonl")
	f, err := openRotatingFile(path, policy, rotationTime(), time.UTC, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := f.Close(); err != nil {
			t.Error(err)
		}
	})
	return f, path
}

func logArchives(t *testing.T, f *rotatingFile) []logArchive {
	t.Helper()
	dir, err := f.root.Open(".")
	if err != nil {
		t.Fatal(err)
	}
	defer dir.Close()
	archives, err := f.readArchives(dir)
	if err != nil {
		t.Fatal(err)
	}
	return archives
}

func writeLogAt(t *testing.T, f *rotatingFile, data string, now time.Time) {
	t.Helper()
	if n, err := f.writeAt([]byte(data), now); err != nil || n != len(data) {
		t.Fatalf("write returned %d, %v", n, err)
	}
}

func TestFileRotationSizeDailyAndWholeRecords(t *testing.T) {
	f, path := testRotatingFile(t, RotationConfig{MaxBytes: 8})
	now := rotationTime()
	writeLogAt(t, f, "1234", now)
	writeLogAt(t, f, "5678", now)
	if len(logArchives(t, f)) != 0 {
		t.Fatal("rotated before reaching size limit")
	}
	writeLogAt(t, f, "next", now)
	archives := logArchives(t, f)
	if len(archives) != 1 {
		t.Fatal("size did not rotate")
	}
	data, err := f.root.ReadFile(archives[0].name)
	if err != nil || string(data) != "12345678" {
		t.Fatal("archive lost or split data", err)
	}
	if n, err := f.writeAt([]byte("too-large"), now); n != 0 || !errors.Is(err, fault.Invalid) {
		t.Fatal("oversized record accepted", err)
	}
	// A local midnight is not a UTC midnight.
	writeLogAt(t, f, "!", now.In(time.FixedZone("next-day", 13*60*60)))
	if len(logArchives(t, f)) != 1 {
		t.Fatal("local timezone changed rollover")
	}
	writeLogAt(t, f, "day-two", now.UTC().Truncate(24*time.Hour).Add(24*time.Hour))
	if len(logArchives(t, f)) != 2 {
		t.Fatal("UTC day did not rotate")
	}
	data, err = os.ReadFile(path)
	if err != nil || string(data) != "day-two" {
		t.Fatal("active file lost record", err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("replacement permissions are not private", err)
	}
}

func TestFileRetentionCountAgeAndRestart(t *testing.T) {
	policy := RotationConfig{MaxBytes: 4, MaxFiles: 2, MaxAge: 48 * time.Hour}
	f, path := testRotatingFile(t, policy)
	now := rotationTime()
	for i := 0; i < 5; i++ {
		writeLogAt(t, f, fmt.Sprintf("%04d", i), now.Add(time.Duration(i)*time.Second))
	}
	f.awaitCleanup()
	archives := logArchives(t, f)
	if len(archives) != 2 {
		t.Fatal("archive count was not enforced")
	}
	var retained string
	for _, archive := range archives {
		data, err := f.root.ReadFile(archive.name)
		if err != nil {
			t.Fatal(err)
		}
		retained += string(data)
	}
	if !strings.Contains(retained, "0002") || !strings.Contains(retained, "0003") {
		t.Fatal("retention removed newer archives")
	}
	// Unrelated files, malformed archive names, directories and symlinks survive.
	if err := os.WriteFile(path+".foundry-not-an-archive.log", []byte("unrelated"), 0600); err != nil {
		t.Fatal(err)
	}
	link := "app.jsonl.foundry-20000101T000000.000000000Z-" + strings.Repeat("0", 32) + ".log"
	if err := os.Symlink(path, filepath.Join(filepath.Dir(path), link)); err != nil {
		t.Fatal(err)
	}
	directory := strings.Replace(link, strings.Repeat("0", 32), strings.Repeat("1", 32), 1)
	if err := os.Mkdir(filepath.Join(filepath.Dir(path), directory), 0700); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	fresh, err := openRotatingFile(path, policy, now.Add(72*time.Hour), time.UTC, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer fresh.Close()
	if len(logArchives(t, fresh)) != 0 {
		t.Fatal("startup did not prune expired archives")
	}
	for _, name := range []string{filepath.Base(path) + ".foundry-not-an-archive.log", link, directory} {
		if _, err := fresh.root.Lstat(name); err != nil {
			t.Fatal("retention touched unrelated entry", err)
		}
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "0004" {
		t.Fatal("restart truncated active file", err)
	}
}

// A request made while an older one is still queued must not be dropped: the
// worker would otherwise prune with the stale time. No worker runs here, so
// the pending slot shows exactly what the worker would receive.
func TestFileCleanupRequestKeepsTheLatestTime(t *testing.T) {
	f := &rotatingFile{cleanup: make(chan time.Time, 1)}
	now := rotationTime()
	f.requestCleanup(now)
	f.requestCleanup(now.Add(time.Hour))
	if queued := <-f.cleanup; !queued.Equal(now.Add(time.Hour)) {
		t.Fatal("a newer cleanup request was dropped", queued)
	}
	// A clock that stepped backwards keeps the later pending time.
	f.requestCleanup(now.Add(2 * time.Hour))
	f.requestCleanup(now)
	if queued := <-f.cleanup; !queued.Equal(now.Add(2 * time.Hour)) {
		t.Fatal("an earlier time replaced a later pending request", queued)
	}
	if next := f.nextPrune.Load(); next != now.Add(pruneInterval).UnixNano() {
		t.Fatal("the writer's next prune deadline changed", next)
	}
}

func TestFileRetentionRunsOnOrdinaryWrites(t *testing.T) {
	f, _ := testRotatingFile(t, RotationConfig{MaxBytes: 100, MaxAge: time.Hour})
	now := rotationTime()
	writeLogAt(t, f, strings.Repeat("a", 100), now)
	writeLogAt(t, f, "b", now)
	if len(logArchives(t, f)) != 1 {
		t.Fatal("missing initial archive")
	}
	writeLogAt(t, f, "c", now.Add(time.Hour))
	f.awaitCleanup()
	if len(logArchives(t, f)) != 0 {
		t.Fatal("ordinary write did not expire old archive")
	}
}

func TestFileRotationRestartRollsOldActiveDay(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.jsonl")
	now := rotationTime()
	if err := os.WriteFile(path, []byte("yesterday"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, now.Add(-24*time.Hour), now.Add(-24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	f, err := openRotatingFile(path, RotationConfig{}, now, time.UTC, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	writeLogAt(t, f, "today", now)
	if len(logArchives(t, f)) != 1 {
		t.Fatal("restart forgot prior file day")
	}
}

func TestRotationOwnershipFailureCleanupAndExplicitDisable(t *testing.T) {
	f, path := testRotatingFile(t, RotationConfig{})
	if _, err := openRotatingFile(path, RotationConfig{}, rotationTime(), time.UTC, nil); !errors.Is(err, fault.Conflict) {
		t.Fatal("second owner acquired rotating file", err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(outside, []byte("untouched"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, path); err != nil {
		t.Fatal(err)
	}
	if _, err := openRotatingFile(path, RotationConfig{}, rotationTime(), time.UTC, nil); !errors.Is(err, fault.Invalid) {
		t.Fatal("symlink accepted", err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	reopened, err := openRotatingFile(path, RotationConfig{}, rotationTime(), time.UTC, nil)
	if err != nil {
		t.Fatal("failed startup retained lock", err)
	}
	if err := reopened.Close(); err != nil {
		t.Fatal(err)
	}
	sink, err := PrepareSink(SinkConfig{Driver: File, Path: path, Rotation: RotationConfig{Disabled: true, MaxBytes: 1}})
	if err != nil {
		t.Fatal(err)
	}
	if err := sink.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	defer sink.Close()
	if _, err := sink.Write([]byte("larger-than-limit")); err != nil {
		t.Fatal("explicit opt-out still rotated", err)
	}
	data, _ := os.ReadFile(outside)
	if string(data) != "untouched" {
		t.Fatal("symlink target changed")
	}
}

func TestRotatingSinkConcurrentRecordsAndClose(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.jsonl")
	sink, err := PrepareSink(SinkConfig{Driver: File, Path: path, Rotation: RotationConfig{MaxBytes: 256, MaxFiles: 100}})
	if err != nil {
		t.Fatal(err)
	}
	if err := sink.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sink.Close() })
	var workers sync.WaitGroup
	for worker := 0; worker < 8; worker++ {
		workers.Go(func() {
			for i := 0; i < 32; i++ {
				if _, err := fmt.Fprintf(sink, "%02d:%02d\n", worker, i); err != nil {
					t.Error(err)
				}
			}
		})
	}
	workers.Wait()
	if err := sink.Close(); err != nil {
		t.Fatal(err)
	}
	files, err := filepath.Glob(path + "*")
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, path := range files {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, record := range bytes.Split(bytes.TrimSpace(data), []byte("\n")) {
			if len(record) == 0 {
				continue
			}
			if len(record) != 5 || seen[string(record)] {
				t.Fatal("record split or duplicated")
			}
			seen[string(record)] = true
		}
	}
	if len(seen) != 256 {
		t.Fatal("concurrent rotation lost records")
	}
	if _, err := sink.Write([]byte("closed")); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatal("closed sink accepted record", err)
	}
}

func TestRotationRefusesReplacedActiveFileAndRecovers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.jsonl")
	var notices bytes.Buffer
	events := newSinkEvents(File, &notices)
	f, err := openRotatingFile(path, RotationConfig{MaxBytes: 4}, rotationTime(), time.UTC, events)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	now := rotationTime()
	writeLogAt(t, f, "kept", now)
	if err := os.Rename(path, path+".saved"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("external replacement"), 0600); err != nil {
		t.Fatal(err)
	}
	// Refusing the substituted path is a rotation failure, not a lost record:
	// the record stays on the owned descriptor and rotation backs off.
	writeLogAt(t, f, "next", now)
	writeLogAt(t, f, "more", now.Add(30*time.Second))
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "external replacement" || len(logArchives(t, f)) != 0 {
		t.Fatal("failed rotation changed unrelated file", err)
	}
	if saved, err := os.ReadFile(path + ".saved"); err != nil || string(saved) != "keptnextmore" {
		t.Fatal("rotation failure lost records", string(saved), err)
	}
	if stats := events.snapshot(); stats.RotationFailures != 1 || strings.Count(notices.String(), "log rotation failed") != 1 || strings.Contains(notices.String(), path) {
		t.Fatal("rotation failure was not counted once with a safe notice", stats, notices.String())
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(path+".saved", path); err != nil {
		t.Fatal(err)
	}
	writeLogAt(t, f, "wait", now.Add(59*time.Second))
	if len(logArchives(t, f)) != 0 {
		t.Fatal("rotation retried before its backoff")
	}
	writeLogAt(t, f, "next", now.Add(time.Minute))
	if len(logArchives(t, f)) != 1 || events.snapshot().RotationFailures != 1 {
		t.Fatal("rotation failed to recover")
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "next" {
		t.Fatal("recovered rotation lost the active record", err)
	}
}

func TestPersistentPruneFailureNeverDropsRecords(t *testing.T) {
	dir := t.TempDir()
	var notices bytes.Buffer
	events := newSinkEvents(File, &notices)
	path := filepath.Join(dir, "app.jsonl")
	now := rotationTime()
	f, err := openRotatingFile(path, RotationConfig{}, now, time.UTC, events)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	// A write/search-only directory still permits appending, creating and
	// renaming log files, but every cleanup scan fails to list it.
	if err := os.Chmod(dir, 0300); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0700) })
	written := 0
	write := func(at time.Time) {
		writeLogAt(t, f, "rec", at)
		f.awaitCleanup()
		written++
	}
	failures := func() uint64 { return events.snapshot().PruneFailures }
	start := now.Add(pruneInterval) // the first hourly cleanup after startup
	write(start)
	if failures() != 1 {
		t.Fatal("cleanup failure was not counted", failures())
	}
	// Writes before the backoff neither rescan nor fail.
	for i := 1; i < 60; i++ {
		write(start.Add(time.Duration(i) * time.Second))
	}
	if failures() != 1 {
		t.Fatal("cleanup retried on every write", failures())
	}
	// Each due retry runs off the write path and doubles its delay.
	for _, offset := range []time.Duration{time.Minute, 3 * time.Minute, 7 * time.Minute} {
		write(start.Add(offset))
	}
	if failures() != 4 {
		t.Fatal("cleanup did not back off exponentially", failures())
	}
	write(start.Add(8 * time.Minute))
	if failures() != 4 {
		t.Fatal("cleanup retried before its backoff", failures())
	}
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || strings.Count(string(data), "rec") != written {
		t.Fatal("cleanup failure dropped records", written, err)
	}
	if strings.Count(notices.String(), "log retention cleanup failed") != 1 {
		t.Fatal("cleanup notice was not emitted exactly once", notices.String())
	}
}

func TestRetryDelayIsBounded(t *testing.T) {
	for failures, want := range map[int]time.Duration{1: time.Minute, 2: 2 * time.Minute, 3: 4 * time.Minute, 7: time.Hour, 100: time.Hour} {
		if got := retryDelay(failures); got != want {
			t.Fatal("unexpected retry delay", failures, got)
		}
	}
}

func TestRotationConfigDefaultsAndInvalidLimits(t *testing.T) {
	if (RotationConfig{}).resolved() != DefaultRotationConfig() {
		t.Fatal("zero configuration lost safe defaults")
	}
	for _, policy := range []RotationConfig{{MaxBytes: -1}, {MaxBytes: 1<<30 + 1}, {MaxFiles: -1}, {MaxFiles: 1001}, {MaxAge: time.Nanosecond}, {MaxAge: 366 * 24 * time.Hour}} {
		if err := policy.Validate(); err == nil {
			t.Fatal("invalid limits accepted", policy)
		}
	}
	for _, driver := range []SinkDriver{Stderr, Stdout, Stack} {
		settings := ChannelSettings{Sink: SinkConfig{Driver: driver, Rotation: DefaultRotationConfig()}, Stack: []ChannelName{"file"}}
		if driver != Stack {
			settings.Stack = nil
		}
		if err := settings.Validate(); err == nil {
			t.Fatal("non-file channel accepted rotation")
		}
	}
}
